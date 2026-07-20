package mcp

import (
	"context"
	"fmt"
	"sync"
)

type MCPServer struct {
	mu         sync.RWMutex
	manager    *ConnectionManager
	config     ServerConfig
}

func NewMCPServer() (*MCPServer, error) {
	return &MCPServer{
		manager: NewConnectionManager(),
	}, nil
}

func NewMCPServerWithConfig(config ServerConfig) *MCPServer {
	return &MCPServer{
		manager: NewConnectionManager(),
		config:  config,
	}
}

func (s *MCPServer) Serve(_ context.Context) error {
	return fmt.Errorf("use ConnectToServer or StartServer instead")
}

func (s *MCPServer) ConnectToServer(ctx context.Context, config ServerConfig) (*MCPServerConnection, error) {
	return s.manager.Connect(ctx, config)
}

func (s *MCPServer) DisconnectServer(name string) error {
	return s.manager.Disconnect(name)
}

func (s *MCPServer) ReconnectServer(ctx context.Context, name string) error {
	return s.manager.Reconnect(ctx, name)
}

func (s *MCPServer) GetConnection(name string) *MCPServerConnection {
	return s.manager.Get(name)
}

func (s *MCPServer) GetAllConnections() []*MCPServerConnection {
	return s.manager.All()
}

func (s *MCPServer) CallTool(ctx context.Context, serverName, toolName string, arguments map[string]any) (*ToolCallResult, error) {
	conn := s.manager.Get(serverName)
	if conn == nil {
		return nil, fmt.Errorf("MCP server %q not found", serverName)
	}

	if conn.GetState() != StateConnected {
		return nil, fmt.Errorf("MCP server %q is not connected (state: %s)", serverName, conn.GetState())
	}

	return conn.CallTool(ctx, toolName, arguments)
}

func (s *MCPServer) ReadResource(ctx context.Context, serverName, uri string) (*ResourceReadResult, error) {
	conn := s.manager.Get(serverName)
	if conn == nil {
		return nil, fmt.Errorf("MCP server %q not found", serverName)
	}

	if conn.GetState() != StateConnected {
		return nil, fmt.Errorf("MCP server %q is not connected", serverName)
	}

	return conn.ReadResource(ctx, uri)
}

func (s *MCPServer) ListAllTools() map[string][]ToolDefinition {
	conns := s.manager.All()
	result := make(map[string][]ToolDefinition)
	for _, conn := range conns {
		if conn.GetState() == StateConnected {
			result[conn.GetName()] = conn.GetTools()
		}
	}
	return result
}

func (s *MCPServer) ListAllResources() map[string][]ResourceDefinition {
	conns := s.manager.All()
	result := make(map[string][]ResourceDefinition)
	for _, conn := range conns {
		if conn.GetState() == StateConnected {
			result[conn.GetName()] = conn.GetResources()
		}
	}
	return result
}

func (s *MCPServer) GetManager() *ConnectionManager {
	return s.manager
}
