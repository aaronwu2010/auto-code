package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/auto-code/auto-code/internal/state"
)

// stdio_server.go 实现 NDJSON-RPC over stdio 的服务端。
//
// 通信约定：
//   - 每行一个 JSON 对象，行尾 \n。
//   - 前端 -> Go: {"id":"req-1","method":"send_message","params":{...}}
//   - Go -> 前端（响应）: {"id":"req-1","result":{...}}
//   - Go -> 前端（事件）: {"event":"query:message","data":{...}}
//
// 服务端在收到 EOF 或 ctx.Done() 时退出。

// StdioServer 是 stdio NDJSON-RPC 服务端。
type StdioServer struct {
	adapter *Adapter
	in      io.Reader
	out     io.Writer
	outMu   sync.Mutex     // 串行化 stdout 写入，避免行交错
	wg      sync.WaitGroup // 跟踪所有在飞的 handleLine goroutine

	// pending 用于在测试中观察请求处理完成。
	// 不暴露为公共 API。
}

// NewStdioServer 用默认 stdin/stdout 构造服务端。
func NewStdioServer(adapter *Adapter) *StdioServer {
	s := &StdioServer{
		adapter: adapter,
		in:      os.Stdin,
		out:     os.Stdout,
	}
	// 把 emit 回调指向本服务端的 writeEvent
	adapter.SetEmit(s.writeEvent)
	// 把 AppState 的状态变更桥接到事件流
	adapter.registerStateListener()
	return s
}

// NewStdioServerWithIO 用自定义 IO 构造服务端（用于测试）。
func NewStdioServerWithIO(adapter *Adapter, in io.Reader, out io.Writer) *StdioServer {
	s := &StdioServer{
		adapter: adapter,
		in:      in,
		out:     out,
	}
	adapter.SetEmit(s.writeEvent)
	adapter.registerStateListener()
	return s
}

// Serve 阻塞读取 stdin 并分发请求，直到 EOF 或 ctx 取消。
func (s *StdioServer) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.in)
	// 单行最大 4MB，足以容纳大型消息历史
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	for {
		select {
		case <-ctx.Done():
			// 取消时仍等待在飞请求处理完，避免半截输出
			s.wg.Wait()
			s.adapter.WaitStreams()
			return ctx.Err()
		default:
		}

		if !scanner.Scan() {
			break
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// 每行一个请求，独立 goroutine 处理避免阻塞读取循环。
		// 但需复制 line，因为 scanner 下次迭代会复用底层数组。
		lineCopy := make([]byte, len(line))
		copy(lineCopy, line)

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleLine(ctx, lineCopy)
		}()
	}

	// scanner 结束（EOF 或错误）
	if err := scanner.Err(); err != nil {
		s.wg.Wait()
		s.adapter.WaitStreams()
		return fmt.Errorf("stdin scan: %w", err)
	}

	// EOF：等待所有在飞的请求处理完成，再等待后台流式推送，
	// 确保所有响应和事件都已写入 stdout。
	s.wg.Wait()
	s.adapter.WaitStreams()
	return nil
}

// handleLine 解析并分发一行请求。
func (s *StdioServer) handleLine(ctx context.Context, line []byte) {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		// 无法解析的行直接返回错误响应（id 留空）。
		s.writeResponse(Response{
			Error: NewError(CodeInvalidParam, "invalid JSON: "+err.Error()),
		})
		return
	}

	if req.Method == "" {
		s.writeResponse(Response{
			ID:    req.ID,
			Error: NewError(CodeInvalidParam, "method is required"),
		})
		return
	}

	result, err := s.dispatch(ctx, &req)
	if err != nil {
		s.writeResponse(Response{
			ID:    req.ID,
			Error: err,
		})
		return
	}

	s.writeResponse(Response{
		ID:     req.ID,
		Result: result,
	})
}

// dispatch 根据方法名路由到 Adapter 对应处理函数。
// 返回值 result 为成功结果，err 为 *RPCError。
func (s *StdioServer) dispatch(ctx context.Context, req *Request) (interface{}, *RPCError) {
	// 解析 params 的辅助函数
	parseParams := func(dst interface{}) *RPCError {
		if len(req.Params) == 0 {
			return nil
		}
		if err := json.Unmarshal(req.Params, dst); err != nil {
			return NewError(CodeInvalidParam, "invalid params: "+err.Error())
		}
		return nil
	}

	switch req.Method {
	case "send_message":
		var p SendMessageRequest
		if e := parseParams(&p); e != nil {
			return nil, e
		}
		return s.adapter.SendMessage(ctx, p), nil

	case "interrupt":
		s.adapter.Interrupt()
		return map[string]bool{"ok": true}, nil

	case "get_messages":
		return s.adapter.GetMessages(), nil

	case "get_app_state":
		return s.adapter.GetAppState(), nil

	case "set_model":
		var p SetModelRequest
		if e := parseParams(&p); e != nil {
			return nil, e
		}
		s.adapter.SetModel(p)
		return map[string]bool{"ok": true}, nil

	case "set_thinking":
		var p SetThinkingRequest
		if e := parseParams(&p); e != nil {
			return nil, e
		}
		s.adapter.SetThinking(p)
		return map[string]bool{"ok": true}, nil

	case "set_fast_mode":
		var p SetFastModeRequest
		if e := parseParams(&p); e != nil {
			return nil, e
		}
		s.adapter.SetFastMode(p)
		return map[string]bool{"ok": true}, nil

	case "set_permission_mode":
		var p SetPermissionModeRequest
		if e := parseParams(&p); e != nil {
			return nil, e
		}
		s.adapter.SetPermissionMode(p)
		return map[string]bool{"ok": true}, nil

	case "set_ollama_config":
		var p OllamaConfigRequest
		if e := parseParams(&p); e != nil {
			return nil, e
		}
		if err := s.adapter.SetOllamaConfig(p); err != nil {
			return nil, NewError(CodeInternal, err.Error())
		}
		return map[string]bool{"ok": true}, nil

	case "get_ollama_config":
		return s.adapter.GetOllamaConfig(), nil

	case "list_models":
		return s.adapter.ListAvailableModels(ctx), nil

	case "get_context_usage":
		usage, err := s.adapter.GetContextUsage(ctx)
		if err != nil {
			return nil, NewError(CodeInternal, err.Error())
		}
		return usage, nil

	case "check_health":
		return s.adapter.CheckOllamaHealth(ctx), nil

	case "get_session_id":
		return map[string]string{"session_id": s.adapter.GetSessionID()}, nil

	case "get_available_tools":
		return s.adapter.GetAvailableTools(ctx), nil

	case "refresh_context":
		if err := s.adapter.RefreshContext(ctx); err != nil {
			return nil, NewError(CodeInternal, err.Error())
		}
		return map[string]bool{"ok": true}, nil

	case "set_workspace":
		var p struct {
			Dir string `json:"dir"`
		}
		if e := parseParams(&p); e != nil {
			return nil, e
		}
		if err := s.adapter.SetWorkspace(p.Dir); err != nil {
			return nil, NewError(CodeInvalidParam, err.Error())
		}
		return map[string]bool{"ok": true}, nil

	case "get_workspace":
		return map[string]string{"dir": s.adapter.GetWorkspace()}, nil

	default:
		return nil, NewError(CodeMethodNotFound, "method not found: "+req.Method)
	}
}

// writeResponse 串行化写出一行响应。
func (s *StdioServer) writeResponse(resp Response) {
	s.writeJSON(resp)
}

// writeEvent 串行化写出一行事件。
func (s *StdioServer) writeEvent(name string, data interface{}) {
	// 与 state.WailsBindings 行为保持一致：StateChangeEvent 直接序列化对象，
	// 其他事件直接序列化对象。NDJSON 层会统一处理。
	s.writeJSON(Event{Event: name, Data: data})
}

// writeJSON 是底层写一行 JSON 的实现，保证行完整性。
func (s *StdioServer) writeJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		// 序列化失败不应导致整个进程崩溃，写一条错误事件即可。
		errEvent := Event{
			Event: "internal_error",
			Data:  map[string]string{"error": "marshal failed: " + err.Error()},
		}
		data, _ = json.Marshal(errEvent)
	}

	s.outMu.Lock()
	defer s.outMu.Unlock()
	_, _ = s.out.Write(data)
	_, _ = s.out.Write([]byte("\n"))
}

// EmitForTest 暴露 emit 回调给测试用例，便于直接注入事件。
// 不属于稳定 API。
func (s *StdioServer) EmitForTest(name string, data interface{}) {
	s.writeEvent(name, data)
}

// _ 用于确保 state 包被使用（registerStateListener 通过 Adapter 间接使用）。
var _ = state.StateChangeEvent{}
