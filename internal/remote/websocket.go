package remote

// STUB IMPLEMENTATION: This file contains placeholder stubs for the WebSocket-based
// remote session protocol. Connect() does not establish a real WebSocket connection,
// Send() is a no-op, and listen() only waits for context cancellation.
// TODO: Implement real WebSocket transport using gorilla/websocket or nhooyr/websocket.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type RemotePermissionResponse struct {
	Behavior string `json:"behavior"`
	Message  string `json:"message,omitempty"`
}

type RemoteSessionConfig struct {
	ServerURL string `json:"serverUrl"`
	AuthToken string `json:"authToken"`
	SessionID string `json:"sessionId"`
}

type RemoteSessionCallbacks struct {
	OnConnected    func(sessionID string)
	OnDisconnected func(sessionID string, err error)
	OnMessage      func(msgType string, data json.RawMessage)
	OnError        func(err error)
}

func CreateRemoteSessionConfig(serverURL, authToken string) RemoteSessionConfig {
	return RemoteSessionConfig{
		ServerURL: serverURL,
		AuthToken: authToken,
		SessionID: fmtRemoteSessionID(),
	}
}

type SessionsWebSocket struct {
	mu        sync.RWMutex
	url       string
	connected bool
	callbacks SessionsWebSocketCallbacks
	cancel    context.CancelFunc
}

type SessionsWebSocketCallbacks struct {
	OnOpen    func()
	OnClose   func(err error)
	OnMessage func(data []byte)
	OnError   func(err error)
}

func NewSessionsWebSocket(url string, callbacks SessionsWebSocketCallbacks) *SessionsWebSocket {
	return &SessionsWebSocket{
		url:       url,
		callbacks: callbacks,
	}
}

func (ws *SessionsWebSocket) Connect(ctx context.Context) error {
	ws.mu.Lock()
	ws.connected = true
	ws.mu.Unlock()

	wsCtx, cancel := context.WithCancel(ctx)
	ws.cancel = cancel

	if ws.callbacks.OnOpen != nil {
		ws.callbacks.OnOpen()
	}

	go ws.listen(wsCtx)

	return nil
}

func (ws *SessionsWebSocket) listen(ctx context.Context) {
	<-ctx.Done()
	ws.mu.Lock()
	ws.connected = false
	ws.mu.Unlock()

	if ws.callbacks.OnClose != nil {
		ws.callbacks.OnClose(nil)
	}
}

func (ws *SessionsWebSocket) Close() {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.cancel != nil {
		ws.cancel()
	}
	ws.connected = false
}

func (ws *SessionsWebSocket) IsConnected() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.connected
}

func (ws *SessionsWebSocket) Send(data []byte) error {
	ws.mu.RLock()
	connected := ws.connected
	ws.mu.RUnlock()

	if !connected {
		return ErrNotConnected
	}
	return nil
}

type SDKMessage struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content,omitempty"`
	Role    string          `json:"role,omitempty"`
}

type ConvertedMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func ConvertSDKMessage(msg SDKMessage) ConvertedMessage {
	return ConvertedMessage{
		Role:    msg.Role,
		Content: string(msg.Content),
	}
}

func IsSessionEndMessage(msg SDKMessage) bool {
	return msg.Type == "session_end"
}

func IsSuccessResult(msg SDKMessage) bool {
	return msg.Type == "result" || msg.Type == "success"
}

func GetResultText(msg SDKMessage) string {
	return string(msg.Content)
}

func CreateSyntheticAssistantMessage(content string) SDKMessage {
	return SDKMessage{
		Type:    "assistant",
		Role:    "assistant",
		Content: json.RawMessage(`"` + content + `"`),
	}
}

func CreateToolStub(toolName, toolID string) SDKMessage {
	return SDKMessage{
		Type:    "tool_use",
		Role:    "assistant",
		Content: json.RawMessage(`{"name":"` + toolName + `","id":"` + toolID + `"}`),
	}
}

var remoteSessionIDCounter int64

func fmtRemoteSessionID() string {
	remoteSessionIDCounter++
	return fmtRemoteID(remoteSessionIDCounter)
}

func fmtRemoteID(n int64) string {
	return fmt.Sprintf("remote-%d", n)
}

type Error string

func (e Error) Error() string { return string(e) }

const ErrNotConnected Error = "websocket not connected"
