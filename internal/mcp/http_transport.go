package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type HTTPTransport struct {
	baseURL  string
	client   *http.Client
	mu       sync.Mutex
	nextID   atomic.Int64
	onNotif  func(*JSONRPCNotification)
	headers  map[string]string
}

func NewHTTPTransport(baseURL string) (*HTTPTransport, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	return &HTTPTransport{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
		headers: make(map[string]string),
	}, nil
}

func (t *HTTPTransport) Send(ctx context.Context, msg *JSONRPCRequest) (*JSONRPCResponse, error) {
	if msg.ID == nil {
		msg.ID = t.nextID.Add(1)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &rpcResp, nil
}

func (t *HTTPTransport) SendNotification(_ context.Context, notif *JSONRPCNotification) error {
	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

func (t *HTTPTransport) Close() error {
	return nil
}

func (t *HTTPTransport) OnNotification(handler func(*JSONRPCNotification)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onNotif = handler
}

func (t *HTTPTransport) SetHeader(key, value string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.headers[key] = value
}