package remote

import (
	"context"
	"sync"
	"time"
)

type RemoteSessionState int

const (
	RemoteSessionDisconnected RemoteSessionState = iota
	RemoteSessionConnecting
	RemoteSessionConnected
	RemoteSessionReconnecting
	RemoteSessionClosed
)

type RemoteSessionConfig struct {
	ServerURL    string
	SessionID    string
	AuthToken    string
	ReconnectMax int
}

type RemoteSession struct {
	mu          sync.RWMutex
	config      RemoteSessionConfig
	state       RemoteSessionState
	connectedAt time.Time
	lastError   string
}

func NewRemoteSession(config RemoteSessionConfig) *RemoteSession {
	return &RemoteSession{
		config: config,
		state:  RemoteSessionDisconnected,
	}
}

func (s *RemoteSession) Connect(ctx context.Context) error {
	s.mu.Lock()
	s.state = RemoteSessionConnecting
	s.mu.Unlock()

	s.mu.Lock()
	s.state = RemoteSessionConnected
	s.connectedAt = time.Now()
	s.mu.Unlock()

	return nil
}

func (s *RemoteSession) Disconnect() {
	s.mu.Lock()
	s.state = RemoteSessionDisconnected
	s.mu.Unlock()
}

func (s *RemoteSession) GetState() RemoteSessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *RemoteSession) IsConnected() bool {
	return s.GetState() == RemoteSessionConnected
}

func (s *RemoteSession) GetLastError() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastError
}

type SSHSessionConfig struct {
	Host       string
	Port       int
	User       string
	PrivateKey []byte
}

type SSHSession struct {
	mu     sync.RWMutex
	config SSHSessionConfig
	state  RemoteSessionState
}

func NewSSHSession(config SSHSessionConfig) *SSHSession {
	return &SSHSession{
		config: config,
		state:  RemoteSessionDisconnected,
	}
}

func (s *SSHSession) Connect(ctx context.Context) error {
	s.mu.Lock()
	s.state = RemoteSessionConnecting
	s.mu.Unlock()

	s.mu.Lock()
	s.state = RemoteSessionConnected
	s.mu.Unlock()

	return nil
}

func (s *SSHSession) Disconnect() {
	s.mu.Lock()
	s.state = RemoteSessionDisconnected
	s.mu.Unlock()
}

func (s *SSHSession) GetState() RemoteSessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *SSHSession) IsConnected() bool {
	return s.GetState() == RemoteSessionConnected
}

type RemotePermissionBridge struct {
	mu          sync.RWMutex
	session     *RemoteSession
	permissions map[string]bool
}

func NewRemotePermissionBridge(session *RemoteSession) *RemotePermissionBridge {
	return &RemotePermissionBridge{
		session:     session,
		permissions: make(map[string]bool),
	}
}

func (b *RemotePermissionBridge) IsAllowed(toolName string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.session.IsConnected() {
		return false
	}
	allowed, ok := b.permissions[toolName]
	if !ok {
		return true
	}
	return allowed
}

func (b *RemotePermissionBridge) SetPermission(toolName string, allowed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.permissions[toolName] = allowed
}

func (b *RemotePermissionBridge) ClearPermissions() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.permissions = make(map[string]bool)
}