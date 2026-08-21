package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/auto-code/auto-code/internal/auth"
)

const (
	voiceStreamPath      = "/api/ws/speech_to_text/voice_stream"
	keepaliveIntervalMS  = 8000
	keepaliveMsg         = `{"type":"KeepAlive"}`
	closeStreamMsg       = `{"type":"CloseStream"}`
	finalizeSafetyMS     = 5000
	finalizeNoDataMS     = 1500
	wsWriteTimeout       = 10 * time.Second
	wsReadTimeout        = 30 * time.Second
	wsMaxMessageSize     = 65536
	wsPingInterval       = 25 * time.Second
)

type FinalizeSource string

const (
	FinalizePostCloseEndpoint FinalizeSource = "post_closestream_endpoint"
	FinalizeNoDataTimeout     FinalizeSource = "no_data_timeout"
	FinalizeSafetyTimeout     FinalizeSource = "safety_timeout"
	FinalizeWSClose           FinalizeSource = "ws_close"
	FinalizeWSAlreadyClosed   FinalizeSource = "ws_already_closed"
)

type VoiceStreamCallbacks struct {
	OnTranscript func(text string, isFinal bool)
	OnError      func(error string, fatal bool)
	OnClose      func()
	OnReady      func(conn *VoiceStreamConnection)
}

type VoiceStreamConnection struct {
	mu          sync.Mutex
	send        func(audioChunk []byte)
	finalize    func() FinalizeSource
	close       func()
	isConnected func() bool
}

func (c *VoiceStreamConnection) Send(audioChunk []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.send != nil {
		c.send(audioChunk)
	}
}

func (c *VoiceStreamConnection) Finalize() FinalizeSource {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finalize != nil {
		return c.finalize()
	}
	return FinalizeWSAlreadyClosed
}

func (c *VoiceStreamConnection) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.close != nil {
		c.close()
	}
}

func (c *VoiceStreamConnection) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isConnected != nil {
		return c.isConnected()
	}
	return false
}

type voiceStreamMessage struct {
	Type        string `json:"type"`
	Data        string `json:"data,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
	Message     string `json:"message,omitempty"`
}

type VoiceStreamClient struct {
	oauthClient *auth.OAuthClient
	config      auth.OAuthConfig
	httpClient  *http.Client
}

func NewVoiceStreamClient(oauthClient *auth.OAuthClient, config auth.OAuthConfig) *VoiceStreamClient {
	return &VoiceStreamClient{
		oauthClient: oauthClient,
		config:      config,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *VoiceStreamClient) IsVoiceStreamAvailable() bool {
	tokens, err := c.oauthClient.GetStoredTokens()
	if err != nil || tokens == nil || tokens.AccessToken == "" {
		return false
	}
	return true
}

func (c *VoiceStreamClient) ConnectVoiceStream(ctx context.Context, callbacks VoiceStreamCallbacks, options *VoiceStreamOptions) (*VoiceStreamConnection, error) {
	tokens, err := c.oauthClient.GetStoredTokens()
	if err != nil || tokens == nil || tokens.AccessToken == "" {
		return nil, fmt.Errorf("no OAuth token available")
	}

	wsBaseURL := strings.Replace(c.config.APIBaseURL, "https://", "wss://", 1)
	wsBaseURL = strings.Replace(wsBaseURL, "http://", "ws://", 1)

	params := []string{
		"encoding=linear16",
		"sample_rate=16000",
		"channels=1",
		"endpointing_ms=300",
		"utterance_end_ms=1000",
	}

	language := "en"
	if options != nil && options.Language != "" {
		language = options.Language
	}
	params = append(params, "language="+language)

	if options != nil && len(options.Keyterms) > 0 {
		for _, term := range options.Keyterms {
			params = append(params, "keyterms="+term)
		}
	}

	url := fmt.Sprintf("%s%s?%s", wsBaseURL, voiceStreamPath, strings.Join(params, "&"))

	// 建立 WebSocket 连接
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+tokens.AccessToken)
	header.Set("User-Agent", "auto-code-voice-client/1.0")

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	wsConn, resp, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("websocket dial failed: %w", err)
	}

	conn := &VoiceStreamConnection{}

	var finalized bool
	var finalizing bool
	var lastTranscriptText string
	var resolveFinalize func(source FinalizeSource)
	var wsMu sync.Mutex
	var wsWriteMu sync.Mutex
	var wsDone chan struct{}

	wsDone = make(chan struct{})

	// 发送音频数据
	conn.send = func(audioChunk []byte) {
		wsMu.Lock()
		if finalized || wsConn == nil {
			wsMu.Unlock()
			return
		}
		wsMu.Unlock()

		wsWriteMu.Lock()
		defer wsWriteMu.Unlock()
		if finalized || wsConn == nil {
			return
		}
		_ = wsConn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
		_ = wsConn.WriteMessage(websocket.BinaryMessage, audioChunk)
	}

	// 结束流
	conn.finalize = func() FinalizeSource {
		if finalizing || finalized {
			return FinalizeWSAlreadyClosed
		}
		finalizing = true

		safetyTimer := time.NewTimer(finalizeSafetyMS * time.Millisecond)
		noDataTimer := time.NewTimer(finalizeNoDataMS * time.Millisecond)

		resolveCh := make(chan FinalizeSource, 1)

		resolveFinalize = func(source FinalizeSource) {
			safetyTimer.Stop()
			noDataTimer.Stop()
			select {
			case resolveCh <- source:
			default:
			}
		}

		// 发送关闭消息
		wsWriteMu.Lock()
		if wsConn != nil {
			_ = wsConn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			_ = wsConn.WriteMessage(websocket.TextMessage, []byte(closeStreamMsg))
		}
		wsWriteMu.Unlock()

		finalized = true

		go func() {
			select {
			case source := <-resolveCh:
				_ = source
			case <-safetyTimer.C:
				resolveFinalize(FinalizeSafetyTimeout)
			case <-noDataTimer.C:
				resolveFinalize(FinalizeNoDataTimeout)
			}
		}()

		return <-resolveCh
	}

	// 关闭连接
	conn.close = func() {
		wsMu.Lock()
		defer wsMu.Unlock()
		finalized = true
		if wsConn != nil {
			_ = wsConn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			_ = wsConn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "client closing"))
			_ = wsConn.Close()
		}
		wsConn = nil
	}

	// 检查连接状态
	conn.isConnected = func() bool {
		wsMu.Lock()
		defer wsMu.Unlock()
		return !finalized && wsConn != nil
	}

	// 读取 WebSocket 消息
	go func() {
		defer close(wsDone)
		defer func() {
			wsMu.Lock()
			wsConn = nil
			wsMu.Unlock()

			if callbacks.OnClose != nil {
				callbacks.OnClose()
			}
		}()

		_ = wsConn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		wsConn.SetPongHandler(func(string) error {
			_ = wsConn.SetReadDeadline(time.Now().Add(wsReadTimeout))
			return nil
		})

		// 心跳 goroutine
		go func() {
			ticker := time.NewTicker(wsPingInterval)
			defer ticker.Stop()
			for {
				select {
				case <-wsDone:
					return
				case <-ticker.C:
					wsWriteMu.Lock()
					if wsConn != nil {
						_ = wsConn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
						_ = wsConn.WriteMessage(websocket.PingMessage, nil)
					}
					wsWriteMu.Unlock()
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			wsConn.SetReadLimit(wsMaxMessageSize)
			_, rawMsg, err := wsConn.ReadMessage()
			if err != nil {
				if !finalized {
					if callbacks.OnError != nil {
						callbacks.OnError(err.Error(), true)
					}
				}
				return
			}

			msg, err := parseVoiceStreamMessage(rawMsg)
			if err != nil {
				continue
			}

			switch msg.Type {
			case "TranscriptText":
				if msg.Data != "" {
					if lastTranscriptText != "" && !strings.HasPrefix(msg.Data, lastTranscriptText) && !strings.HasPrefix(lastTranscriptText, msg.Data) {
						if callbacks.OnTranscript != nil {
							callbacks.OnTranscript(lastTranscriptText, true)
						}
					}
					lastTranscriptText = msg.Data
					if callbacks.OnTranscript != nil {
						callbacks.OnTranscript(msg.Data, false)
					}
				}
			case "TranscriptEndpoint":
				if lastTranscriptText != "" {
					if callbacks.OnTranscript != nil {
						callbacks.OnTranscript(lastTranscriptText, true)
					}
					lastTranscriptText = ""
				}
				if finalized && resolveFinalize != nil {
					resolveFinalize(FinalizePostCloseEndpoint)
				}
			case "TranscriptError":
				desc := msg.Description
				if desc == "" {
					desc = msg.ErrorCode
				}
				if desc == "" {
					desc = "unknown transcription error"
				}
				if !finalizing && callbacks.OnError != nil {
					callbacks.OnError(desc, false)
				}
			case "error":
				if !finalizing && callbacks.OnError != nil {
					callbacks.OnError(msg.Message, false)
				}
			}
		}
	}()

	if callbacks.OnReady != nil {
		callbacks.OnReady(conn)
	}

	return conn, nil
}

type VoiceStreamOptions struct {
	Language string
	Keyterms []string
}

func parseVoiceStreamMessage(data []byte) (*voiceStreamMessage, error) {
	var msg voiceStreamMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
