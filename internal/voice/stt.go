package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/auth"
)

const (
	voiceStreamPath      = "/api/ws/speech_to_text/voice_stream"
	keepaliveIntervalMS  = 8000
	keepaliveMsg         = `{"type":"KeepAlive"}`
	closeStreamMsg       = `{"type":"CloseStream"}`
	finalizeSafetyMS     = 5000
	finalizeNoDataMS     = 1500
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

	_ = url

	conn := &VoiceStreamConnection{}

	var finalized bool
	var finalizing bool
	var lastTranscriptText string
	var resolveFinalize func(source FinalizeSource)

	conn.send = func(audioChunk []byte) {
		if finalized {
			return
		}
	}

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

		finalized = true

		go func() {
			select {
			case source := <-resolveCh:
				_ = source
			case <-safetyTimer.C:
			case <-noDataTimer.C:
			}
		}()

		return <-resolveCh
	}

	conn.close = func() {
		finalized = true
	}

	conn.isConnected = func() bool {
		return !finalized
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				if callbacks.OnClose != nil {
					callbacks.OnClose()
				}
				return
			default:
			}

			msg, err := readMockMessage(ctx)
			if err != nil {
				if callbacks.OnError != nil {
					callbacks.OnError(err.Error(), false)
				}
				if callbacks.OnClose != nil {
					callbacks.OnClose()
				}
				return
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

func readMockMessage(ctx context.Context) (*voiceStreamMessage, error) {
	<-ctx.Done()
	return nil, ctx.Err()
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

func (c *VoiceStreamClient) SendKeepalive(w io.Writer) error {
	_, err := w.Write([]byte(keepaliveMsg))
	return err
}

func (c *VoiceStreamClient) SendCloseStream(w io.Writer) error {
	_, err := w.Write([]byte(closeStreamMsg))
	return err
}