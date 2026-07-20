package mcp

import (
	"context"
	"sync"
)

type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[string]*MCPServerConnection
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*MCPServerConnection),
	}
}

func (m *ConnectionManager) Connect(ctx context.Context, config ServerConfig) (*MCPServerConnection, error) {
	m.mu.Lock()
	if existing, ok := m.connections[config.Name]; ok {
		m.mu.Unlock()
		if existing.GetState() == StateConnected {
			return existing, nil
		}
		if err := existing.Reconnect(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}

	conn := NewMCPServerConnection(config)
	m.connections[config.Name] = conn
	m.mu.Unlock()

	if err := conn.Connect(ctx); err != nil {
		return nil, err
	}

	return conn, nil
}

func (m *ConnectionManager) Disconnect(name string) error {
	m.mu.Lock()
	conn, ok := m.connections[name]
	m.mu.Unlock()

	if !ok {
		return nil
	}

	return conn.Disconnect()
}

func (m *ConnectionManager) Reconnect(ctx context.Context, name string) error {
	m.mu.RLock()
	conn, ok := m.connections[name]
	m.mu.RUnlock()

	if !ok {
		return nil
	}

	return conn.Reconnect(ctx)
}

func (m *ConnectionManager) Get(name string) *MCPServerConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connections[name]
}

func (m *ConnectionManager) All() []*MCPServerConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*MCPServerConnection, 0, len(m.connections))
	for _, conn := range m.connections {
		result = append(result, conn)
	}
	return result
}

func (m *ConnectionManager) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, ok := m.connections[name]; ok {
		_ = conn.Disconnect()
		delete(m.connections, name)
	}
}

func (m *ConnectionManager) ToggleEnabled(name string, enabled bool) error {
	m.mu.RLock()
	conn, ok := m.connections[name]
	m.mu.RUnlock()

	if !ok {
		return nil
	}

	conn.SetEnabled(enabled)
	return nil
}