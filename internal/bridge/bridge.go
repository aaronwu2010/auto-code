package bridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/types"
)

type WorkData struct {
	WorkID      string            `json:"work_id"`
	WorkSecret  string            `json:"work_secret"`
	SessionURL  string            `json:"session_url"`
	EnvironmentID string          `json:"environment_id"`
	Metadata    map[string]string `json:"metadata"`
}

type SpawnMode string

const (
	SpawnModeLocal    SpawnMode = "local"
	SpawnModeRemote   SpawnMode = "remote"
	SpawnModeWorktree SpawnMode = "worktree"
)

type SessionHandle struct {
	SessionID   types.SessionID `json:"session_id"`
	Connected   bool            `json:"connected"`
	ConnectedAt time.Time       `json:"connected_at"`
	LastActive  time.Time       `json:"last_active"`
}

type BridgeMain struct {
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	workerID       string
	sessions       map[string]*SessionHandle
	jwtToken       string
	heartbeatInterval time.Duration
	sessionTimeout time.Duration
	onWorkReceived func(*WorkData) error
}

func NewBridgeMain() *BridgeMain {
	return &BridgeMain{
		sessions:          make(map[string]*SessionHandle),
		heartbeatInterval: 30 * time.Second,
		sessionTimeout:    24 * time.Hour,
	}
}

func (b *BridgeMain) Start(ctx context.Context) error {
	b.ctx, b.cancel = context.WithCancel(ctx)
	go b.heartbeatLoop()
	go b.sessionTimeoutLoop()
	return nil
}

func (b *BridgeMain) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
}

func (b *BridgeMain) RegisterWorker(workerID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.workerID = workerID
	return nil
}

func (b *BridgeMain) GetWork(ctx context.Context) (*WorkData, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("no work available")
	}
}

func (b *BridgeMain) StartSession(sessionID types.SessionID) (*SessionHandle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	handle := &SessionHandle{
		SessionID:   sessionID,
		Connected:   true,
		ConnectedAt: time.Now(),
		LastActive:  time.Now(),
	}
	b.sessions[string(sessionID)] = handle
	return handle, nil
}

func (b *BridgeMain) EndSession(sessionID types.SessionID) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, string(sessionID))
	return nil
}

func (b *BridgeMain) GetSessions() []*SessionHandle {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]*SessionHandle, 0, len(b.sessions))
	for _, h := range b.sessions {
		result = append(result, h)
	}
	return result
}

func (b *BridgeMain) SetOnWorkReceived(fn func(*WorkData) error) {
	b.onWorkReceived = fn
}

func (b *BridgeMain) heartbeatLoop() {
	ticker := time.NewTicker(b.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.mu.RLock()
			for _, h := range b.sessions {
				h.LastActive = time.Now()
			}
			b.mu.RUnlock()
		}
	}
}

func (b *BridgeMain) sessionTimeoutLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.mu.Lock()
			now := time.Now()
			for id, h := range b.sessions {
				if now.Sub(h.LastActive) > b.sessionTimeout {
					delete(b.sessions, id)
				}
			}
			b.mu.Unlock()
		}
	}
}