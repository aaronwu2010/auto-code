package remote

// STUB IMPLEMENTATION: This file contains placeholder stubs for the remote session
// manager. Connect() creates a fake session without connecting to a real server,
// ProxyPermission() always returns Allow without checking server-side policy.
// TODO: Implement real remote session management with server-side permission proxy.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/types"
)

type RemoteSessionManager struct {
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	sessions  map[string]*RemoteSession
	url       string
	status    ConnectionStatus
	httpClient *http.Client
}

type ConnectionStatus string

const (
	StatusConnecting   ConnectionStatus = "connecting"
	StatusConnected    ConnectionStatus = "connected"
	StatusReconnecting ConnectionStatus = "reconnecting"
	StatusDisconnected ConnectionStatus = "disconnected"
)

type RemoteSession struct {
	SessionID   types.SessionID  `json:"session_id"`
	URL         string           `json:"url"`
	Status      ConnectionStatus `json:"status"`
	ConnectedAt time.Time        `json:"connected_at"`
}

func NewRemoteSessionManager(url string) *RemoteSessionManager {
	return &RemoteSessionManager{
		sessions:  make(map[string]*RemoteSession),
		url:       url,
		status:    StatusConnecting,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (m *RemoteSessionManager) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.setStatus(StatusConnecting)

	go m.reconnectLoop()

	return nil
}

func (m *RemoteSessionManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.setStatus(StatusDisconnected)
}

func (m *RemoteSessionManager) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := &RemoteSession{
		SessionID:   types.SessionID(fmt.Sprintf("remote-%d", time.Now().Unix())),
		URL:         m.url,
		Status:      StatusConnected,
		ConnectedAt: time.Now(),
	}
	m.sessions[string(session.SessionID)] = session
	m.status = StatusConnected

	return nil
}

func (m *RemoteSessionManager) Disconnect(sessionID types.SessionID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[string(sessionID)]; ok {
		session.Status = StatusDisconnected
		delete(m.sessions, string(sessionID))
	}

	return nil
}

func (m *RemoteSessionManager) GetStatus() ConnectionStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *RemoteSessionManager) GetSessions() []*RemoteSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*RemoteSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

func (m *RemoteSessionManager) ProxyPermission(ctx context.Context, sessionID types.SessionID, toolName string, input any) (types.PermissionResult, error) {
	m.mu.RLock()
	session, ok := m.sessions[string(sessionID)]
	m.mu.RUnlock()

	if !ok {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}

	if session.Status != StatusConnected {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}

	// 构造权限检查请求
	reqBody := map[string]any{
		"session_id": string(sessionID),
		"tool_name":  toolName,
		"input":      input,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url+"/api/permission/proxy", bytes.NewReader(bodyBytes))
	if err != nil {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		// 服务端不可用时，降级为允许（本地安全策略可覆盖）
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}

	var result types.PermissionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}

	return result, nil
}

func (m *RemoteSessionManager) setStatus(status ConnectionStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = status
}

func (m *RemoteSessionManager) reconnectLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if m.GetStatus() == StatusDisconnected || m.GetStatus() == StatusReconnecting {
				m.setStatus(StatusReconnecting)
				req, err := http.NewRequestWithContext(m.ctx, http.MethodGet, m.url+"/health", nil)
				if err != nil {
					continue
				}
				if resp, err := m.httpClient.Do(req); err == nil {
					resp.Body.Close()
					m.setStatus(StatusConnected)
				}
			}
		}
	}
}
