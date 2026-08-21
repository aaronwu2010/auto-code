package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	readWait       = 60 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 65536
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
	conn      *websocket.Conn
	writeMu   sync.Mutex
	sendCh    chan []byte
	doneCh    chan struct{}
	wg        sync.WaitGroup
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
		sendCh:    make(chan []byte, 256),
		doneCh:    make(chan struct{}),
	}
}

func (ws *SessionsWebSocket) Connect(ctx context.Context) error {
	header := make(http.Header)
	header.Set("User-Agent", "auto-code-remote-client/1.0")

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.DialContext(ctx, ws.url, header)
	if err != nil {
		if ws.callbacks.OnError != nil {
			ws.callbacks.OnError(err)
		}
		return fmt.Errorf("websocket dial failed: %w (http status: %v)", err, resp)
	}

	ws.mu.Lock()
	ws.conn = conn
	ws.connected = true
	wsCtx, cancel := context.WithCancel(ctx)
	ws.cancel = cancel
	ws.mu.Unlock()

	if ws.callbacks.OnOpen != nil {
		ws.callbacks.OnOpen()
	}

	ws.wg.Add(2)
	go ws.readPump(wsCtx)
	go ws.writePump(wsCtx)

	return nil
}

func (ws *SessionsWebSocket) readPump(ctx context.Context) {
	defer ws.wg.Done()
	defer func() {
		ws.mu.Lock()
		ws.connected = false
		ws.mu.Unlock()

		if ws.callbacks.OnClose != nil {
			ws.callbacks.OnClose(nil)
		}
	}()

	ws.mu.RLock()
	conn := ws.conn
	ws.mu.RUnlock()
	if conn == nil {
		return
	}

	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				if ws.callbacks.OnError != nil {
					ws.callbacks.OnError(err)
				}
			}
			return
		}

		if ws.callbacks.OnMessage != nil {
			ws.callbacks.OnMessage(message)
		}
	}
}

func (ws *SessionsWebSocket) writePump(ctx context.Context) {
	defer ws.wg.Done()

	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ws.doneCh:
			return
		case message, ok := <-ws.sendCh:
			ws.writeMu.Lock()
			ws.mu.RLock()
			conn := ws.conn
			ws.mu.RUnlock()
			if conn == nil {
				ws.writeMu.Unlock()
				return
			}

			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
				ws.writeMu.Unlock()
				return
			}

			err := conn.WriteMessage(websocket.TextMessage, message)
			ws.writeMu.Unlock()
			if err != nil {
				if ws.callbacks.OnError != nil {
					ws.callbacks.OnError(err)
				}
				return
			}
		case <-ticker.C:
			ws.writeMu.Lock()
			ws.mu.RLock()
			conn := ws.conn
			ws.mu.RUnlock()
			if conn == nil {
				ws.writeMu.Unlock()
				return
			}

			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				ws.writeMu.Unlock()
				if ws.callbacks.OnError != nil {
					ws.callbacks.OnError(err)
				}
				return
			}
			ws.writeMu.Unlock()
		}
	}
}

func (ws *SessionsWebSocket) Close() {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	ws.connected = false
	if ws.cancel != nil {
		ws.cancel()
		ws.cancel = nil
	}

	if ws.conn != nil {
		_ = ws.conn.Close()
		ws.conn = nil
	}

	close(ws.doneCh)
	ws.wg.Wait()
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

	select {
	case ws.sendCh <- data:
	default:
		return fmt.Errorf("send buffer full (capacity: 256)")
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
	n := atomic.AddInt64(&remoteSessionIDCounter, 1)
	return fmtRemoteID(n)
}

func fmtRemoteID(n int64) string {
	return fmt.Sprintf("remote-%d", n)
}

type Error string

func (e Error) Error() string { return string(e) }

const ErrNotConnected Error = "websocket not connected"
