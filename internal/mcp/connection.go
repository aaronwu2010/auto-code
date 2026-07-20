package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type ConnectionState string

const (
	StatePending   ConnectionState = "pending"
	StateConnected ConnectionState = "connected"
	StateFailed    ConnectionState = "failed"
	StateDisabled  ConnectionState = "disabled"
	NeedsAuth      ConnectionState = "needs_auth"
)

type ServerConfig struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"` // stdio, sse, http
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Enabled bool              `json:"enabled"`
}

type MCPServerConnection struct {
	mu           sync.RWMutex
	config       ServerConfig
	state        ConnectionState
	transport    Transport
	tools        []ToolDefinition
	resources    []ResourceDefinition
	serverInfo   ServerInfo
	capabilities ServerCapabilities
	lastError    string
	connectedAt  time.Time
}

func NewMCPServerConnection(config ServerConfig) *MCPServerConnection {
	state := StatePending
	if !config.Enabled {
		state = StateDisabled
	}
	return &MCPServerConnection{
		config: config,
		state:  state,
	}
}

func (c *MCPServerConnection) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == StateConnected {
		return nil
	}

	var transport Transport
	var err error

	switch c.config.Type {
	case "stdio":
		transport, err = NewStdioTransport(c.config.Command, c.config.Args, c.config.Env)
	case "sse", "http":
		transport, err = NewHTTPTransport(c.config.URL)
	default:
		return fmt.Errorf("unsupported transport type: %s", c.config.Type)
	}

	if err != nil {
		c.state = StateFailed
		c.lastError = err.Error()
		return fmt.Errorf("create transport: %w", err)
	}

	c.transport = transport

	initResult, err := c.initialize(ctx)
	if err != nil {
		c.state = StateFailed
		c.lastError = err.Error()
		_ = transport.Close()
		return fmt.Errorf("initialize: %w", err)
	}

	c.serverInfo = initResult.ServerInfo
	c.capabilities = initResult.Capabilities

	if c.capabilities.Tools != nil {
		tools, err := c.listTools(ctx)
		if err != nil {
			c.state = StateFailed
			c.lastError = err.Error()
			_ = transport.Close()
			return fmt.Errorf("list tools: %w", err)
		}
		c.tools = tools
	}

	if c.capabilities.Resources != nil {
		resources, err := c.listResources(ctx)
		if err == nil {
			c.resources = resources
		}
	}

	c.state = StateConnected
	c.connectedAt = time.Now()
	c.lastError = ""

	return nil
}

func (c *MCPServerConnection) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.transport != nil {
		err := c.transport.Close()
		c.transport = nil
		c.state = StatePending
		c.tools = nil
		c.resources = nil
		return err
	}
	return nil
}

func (c *MCPServerConnection) Reconnect(ctx context.Context) error {
	_ = c.Disconnect()
	return c.Connect(ctx)
}

func (c *MCPServerConnection) CallTool(ctx context.Context, toolName string, arguments map[string]any) (*ToolCallResult, error) {
	c.mu.RLock()
	transport := c.transport
	c.mu.RUnlock()

	if transport == nil {
		return nil, fmt.Errorf("not connected")
	}

	params := ToolCallParams{
		Name:      toolName,
		Arguments: arguments,
	}

	resp, err := transport.Send(ctx, NewRequest(0, "tools/call", params))
	if err != nil {
		return nil, fmt.Errorf("send tool call: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	resultData, err := reencode(resp.Result)
	if err != nil {
		return nil, err
	}

	var result ToolCallResult
	if err := jsonUnmarshal(resultData, &result); err != nil {
		return &ToolCallResult{
			Content: []ToolContent{{Type: "text", Text: string(resultData)}},
		}, nil
	}

	return &result, nil
}

func (c *MCPServerConnection) ReadResource(ctx context.Context, uri string) (*ResourceReadResult, error) {
	c.mu.RLock()
	transport := c.transport
	c.mu.RUnlock()

	if transport == nil {
		return nil, fmt.Errorf("not connected")
	}

	params := ResourceReadParams{URI: uri}
	resp, err := transport.Send(ctx, NewRequest(0, "resources/read", params))
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	resultData, err := reencode(resp.Result)
	if err != nil {
		return nil, err
	}

	var result ResourceReadResult
	if err := jsonUnmarshal(resultData, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *MCPServerConnection) GetState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *MCPServerConnection) GetTools() []ToolDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tools
}

func (c *MCPServerConnection) GetResources() []ResourceDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resources
}

func (c *MCPServerConnection) GetServerInfo() ServerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverInfo
}

func (c *MCPServerConnection) GetLastError() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastError
}

func (c *MCPServerConnection) GetName() string {
	return c.config.Name
}

func (c *MCPServerConnection) IsEnabled() bool {
	return c.config.Enabled
}

func (c *MCPServerConnection) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Enabled = enabled
	if !enabled {
		c.state = StateDisabled
	}
}

func (c *MCPServerConnection) initialize(ctx context.Context) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities: ClientCapabilities{
			Roots:       &RootsCapability{ListChanged: true},
			Elicitation: &ElicitationCapability{},
		},
		ClientInfo: ClientInfo{
			Name:    "auto-code",
			Version: "1.0.0",
		},
	}

	resp, err := c.transport.Send(ctx, NewRequest(0, "initialize", params))
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	resultData, err := reencode(resp.Result)
	if err != nil {
		return nil, err
	}

	var result InitializeResult
	if err := jsonUnmarshal(resultData, &result); err != nil {
		return nil, err
	}

	_ = c.transport.SendNotification(ctx, NewNotification("notifications/initialized", nil))

	return &result, nil
}

func (c *MCPServerConnection) listTools(ctx context.Context) ([]ToolDefinition, error) {
	resp, err := c.transport.Send(ctx, NewRequest(0, "tools/list", nil))
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	resultData, err := reencode(resp.Result)
	if err != nil {
		return nil, err
	}

	var result ToolsListResult
	if err := jsonUnmarshal(resultData, &result); err != nil {
		return nil, err
	}

	return result.Tools, nil
}

func (c *MCPServerConnection) listResources(ctx context.Context) ([]ResourceDefinition, error) {
	resp, err := c.transport.Send(ctx, NewRequest(0, "resources/list", nil))
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	resultData, err := reencode(resp.Result)
	if err != nil {
		return nil, err
	}

	var result ResourcesListResult
	if err := jsonUnmarshal(resultData, &result); err != nil {
		return nil, err
	}

	return result.Resources, nil
}