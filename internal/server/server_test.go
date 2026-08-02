package server

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auto-code/auto-code/internal/api"
	"github.com/auto-code/auto-code/internal/state"
	"github.com/auto-code/auto-code/internal/types"
)

// fakeEngine 是 MessageSubmitter 的测试实现，避免依赖真实的 QueryEngine
// （真实引擎需要 Ollama 服务）。
type fakeEngine struct {
	mu          sync.Mutex
	messages    []types.Message
	model       types.ModelSetting
	config      api.OllamaConfig
	interrupted bool
	sessionID   types.SessionID
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		sessionID: types.SessionID("test-session"),
		messages: []types.Message{
			{ID: "m1", Role: types.RoleUser, Content: "hello", Timestamp: time.Now().Unix()},
		},
	}
}

func (e *fakeEngine) SubmitMessage(ctx context.Context, prompt string) <-chan state.SDKMessage {
	ch := make(chan state.SDKMessage, 8)
	go func() {
		defer close(ch)
		// 模拟一条 user 消息
		userMsg := types.Message{ID: "u1", Role: types.RoleUser, Content: prompt, Timestamp: time.Now().Unix()}
		e.mu.Lock()
		e.messages = append(e.messages, userMsg)
		e.mu.Unlock()
		ch <- state.SDKMessage{Type: "user", Message: &userMsg, SessionID: e.sessionID}

		// 模拟一条 assistant 流式块
		assistantMsg := types.Message{ID: "a1", Role: types.RoleAssistant, Content: "hi", Timestamp: time.Now().Unix()}
		ch <- state.SDKMessage{Type: "stream_chunk", Message: &assistantMsg, SessionID: e.sessionID}
		ch <- state.SDKMessage{Type: "assistant", Message: &assistantMsg, SessionID: e.sessionID}

		// 结束
		ch <- state.SDKMessage{Type: "result", Subtype: "end_turn", SessionID: e.sessionID}
	}()
	return ch
}

func (e *fakeEngine) Interrupt() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.interrupted = true
}

func (e *fakeEngine) GetMessages() []types.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]types.Message, len(e.messages))
	copy(out, e.messages)
	return out
}

func (e *fakeEngine) SetModel(model types.ModelSetting) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.model = model
}

func (e *fakeEngine) SetOllamaConfig(baseURL, apiKey, model string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = api.OllamaConfig{BaseURL: baseURL, APIKey: apiKey, Model: model}
}

func (e *fakeEngine) GetSessionID() types.SessionID {
	return e.sessionID
}

func (e *fakeEngine) CheckHealth(ctx context.Context) *api.HealthStatus {
	return &api.HealthStatus{Connected: true, IsLocal: true, AvailableModels: 3}
}

func (e *fakeEngine) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return []api.ModelInfo{
		{Name: "qwen3:latest", Size: 4 * 1024 * 1024 * 1024, Details: api.ModelDetails{Family: "qwen", ParameterSize: "3B"}},
		{Name: "llama3.2:latest", Size: 2 * 1024 * 1024 * 1024, Details: api.ModelDetails{Family: "llama", ParameterSize: "3B"}},
	}, nil
}

// ========== 测试用例 ==========

func newTestServer(t *testing.T) (*StdioServer, *strings.Builder) {
	t.Helper()
	app := state.NewAppState()
	adapter := NewAdapter(app, nil)
	adapter.SetEngine(newFakeEngine())

	var out strings.Builder
	srv := NewStdioServerWithIO(adapter, strings.NewReader(""), &out)
	return srv, &out
}

// sendRequest 写一行请求并等待响应。
func sendRequest(t *testing.T, srv *StdioServer, out *strings.Builder, line string) Response {
	t.Helper()

	// 清空之前的输出，避免多轮调用时混淆
	out.Reset()
	// 重置 in 为新请求
	srv.in = strings.NewReader(line + "\n")

	done := make(chan struct{})
	go func() {
		_ = srv.Serve(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit after 2s")
	}

	// 解析输出第一行作为响应
	scanner := bufio.NewScanner(strings.NewReader(out.String()))
	if !scanner.Scan() {
		t.Fatal("no output from server")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response JSON: %v, line=%s", err, scanner.Text())
	}
	return resp
}

func TestProtocol_RequestResponseRoundTrip(t *testing.T) {
	srv, out := newTestServer(t)

	resp := sendRequest(t, srv, out, `{"id":"r1","method":"get_session_id","params":{}}`)
	if resp.ID != "r1" {
		t.Errorf("expected id=r1, got %s", resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}
	// result 应为 {"session_id":"test-session"}
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T: %v", resp.Result, resp.Result)
	}
	if m["session_id"] != "test-session" {
		t.Errorf("expected session_id=test-session, got %v", m["session_id"])
	}
}

func TestProtocol_MethodNotFound(t *testing.T) {
	srv, out := newTestServer(t)

	resp := sendRequest(t, srv, out, `{"id":"r2","method":"nonexistent_method","params":{}}`)
	if resp.ID != "r2" {
		t.Errorf("expected id=r2, got %s", resp.ID)
	}
	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected code %d, got %d", CodeMethodNotFound, resp.Error.Code)
	}
}

func TestProtocol_InvalidJSON(t *testing.T) {
	srv, out := newTestServer(t)

	resp := sendRequest(t, srv, out, `not a json`)
	if resp.Error == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if resp.Error.Code != CodeInvalidParam {
		t.Errorf("expected code %d, got %d", CodeInvalidParam, resp.Error.Code)
	}
}

func TestProtocol_GetMessages(t *testing.T) {
	srv, out := newTestServer(t)

	resp := sendRequest(t, srv, out, `{"id":"r3","method":"get_messages","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	msgs, ok := m["messages"].([]interface{})
	if !ok {
		t.Fatalf("expected messages array, got %T", m["messages"])
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

func TestProtocol_SetOllamaConfigAndGet(t *testing.T) {
	srv, out := newTestServer(t)

	// set
	resp := sendRequest(t, srv, out, `{"id":"r4","method":"set_ollama_config","params":{"base_url":"http://x:11434/api","api_key":"k","model":"m1"}}`)
	if resp.Error != nil {
		t.Fatalf("set error: %v", resp.Error)
	}

	// get
	resp = sendRequest(t, srv, out, `{"id":"r5","method":"get_ollama_config","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("get error: %v", resp.Error)
	}
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", resp.Result)
	}
	if m["base_url"] != "http://x:11434/api" {
		t.Errorf("expected base_url=http://x:11434/api, got %v", m["base_url"])
	}
	if m["model"] != "m1" {
		t.Errorf("expected model=m1, got %v", m["model"])
	}
}

func TestProtocol_ListModels(t *testing.T) {
	srv, out := newTestServer(t)

	resp := sendRequest(t, srv, out, `{"id":"r6","method":"list_models","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", resp.Result)
	}
	models, ok := m["models"].([]interface{})
	if !ok {
		t.Fatalf("expected models array, got %T", m["models"])
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestProtocol_CheckHealth(t *testing.T) {
	srv, out := newTestServer(t)

	resp := sendRequest(t, srv, out, `{"id":"r7","method":"check_health","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", resp.Result)
	}
	if m["connected"] != true {
		t.Errorf("expected connected=true, got %v", m["connected"])
	}
	if m["available_models"] != float64(3) {
		t.Errorf("expected available_models=3, got %v", m["available_models"])
	}
}

func TestProtocol_SendMessageEmitsEvents(t *testing.T) {
	app := state.NewAppState()
	adapter := NewAdapter(app, nil)
	adapter.SetEngine(newFakeEngine())

	var out strings.Builder
	srv := NewStdioServerWithIO(adapter, strings.NewReader(`{"id":"r8","method":"send_message","params":{"prompt":"hello"}}`+"\n"), &out)

	done := make(chan struct{})
	go func() {
		_ = srv.Serve(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit")
	}

	output := out.String()
	// 应该看到：1 个响应 + 多个 query:message 事件
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (1 response + events), got %d: %s", len(lines), output)
	}

	// 找出响应行（带 id 且 id=r8）和事件行（带 event 字段）
	var respSeen, eventSeen bool
	for _, line := range lines {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if id, ok := m["id"]; ok && id == "r8" {
			respSeen = true
		}
		if ev, ok := m["event"]; ok && ev == "query:message" {
			eventSeen = true
		}
	}
	if !respSeen {
		t.Errorf("response not seen in output: %s", output)
	}
	if !eventSeen {
		t.Errorf("query:message event not seen in output: %s", output)
	}
}

func TestProtocol_SetWorkspace(t *testing.T) {
	srv, out := newTestServer(t)

	resp := sendRequest(t, srv, out, `{"id":"r9","method":"set_workspace","params":{"dir":"/tmp/test"}}`)
	if resp.Error != nil {
		t.Fatalf("set_workspace error: %v", resp.Error)
	}

	resp = sendRequest(t, srv, out, `{"id":"r10","method":"get_workspace","params":{}}`)
	if resp.Error != nil {
		t.Fatalf("get_workspace error: %v", resp.Error)
	}
	m, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", resp.Result)
	}
	if m["dir"] != "/tmp/test" {
		t.Errorf("expected dir=/tmp/test, got %v", m["dir"])
	}
}

func TestProtocol_StateChangeEventEmitted(t *testing.T) {
	app := state.NewAppState()
	adapter := NewAdapter(app, nil)
	adapter.SetEngine(newFakeEngine())

	var out strings.Builder
	srv := NewStdioServerWithIO(adapter, strings.NewReader(`{"id":"r11","method":"set_thinking","params":{"enabled":true}}`+"\n"), &out)

	done := make(chan struct{})
	go func() {
		_ = srv.Serve(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit")
	}

	output := out.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	var stateEventSeen bool
	for _, line := range lines {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if ev, ok := m["event"]; ok && ev == "state:change" {
			stateEventSeen = true
			break
		}
	}
	if !stateEventSeen {
		t.Errorf("expected state:change event, output=%s", output)
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, tt := range tests {
		got := formatSize(tt.input)
		if got != tt.expected {
			t.Errorf("formatSize(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestToString(t *testing.T) {
	if got := toString(nil, "default"); got != "default" {
		t.Errorf("toString(nil) = %q, want default", got)
	}
	if got := toString("hello", "default"); got != "hello" {
		t.Errorf("toString(hello) = %q, want hello", got)
	}
	if got := toString(123, "default"); got != "default" {
		t.Errorf("toString(123) = %q, want default", got)
	}
}
