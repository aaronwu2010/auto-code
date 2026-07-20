package mcp

import (
	"sync"
)

type ChannelAllowlist struct {
	mu      sync.RWMutex
	entries map[string]bool
}

func NewChannelAllowlist() *ChannelAllowlist {
	return &ChannelAllowlist{
		entries: make(map[string]bool),
	}
}

func (a *ChannelAllowlist) IsAllowlisted(marketplace, plugin string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	key := marketplace + "/" + plugin
	if allowed, ok := a.entries[key]; ok {
		return allowed
	}

	key = marketplace + "/*"
	if allowed, ok := a.entries[key]; ok {
		return allowed
	}

	return false
}

func (a *ChannelAllowlist) Add(marketplace, plugin string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries[marketplace+"/"+plugin] = true
}

func (a *ChannelAllowlist) Remove(marketplace, plugin string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.entries, marketplace+"/"+plugin)
}

type ChannelPermission struct {
	mu      sync.Mutex
	pending map[string]chan *ChannelPermissionResponse
}

type ChannelPermissionRequest struct {
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
	Input      string `json:"input_preview"`
	RequestID  string `json:"request_id"`
}

type ChannelPermissionResponse struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

func NewChannelPermission() *ChannelPermission {
	return &ChannelPermission{
		pending: make(map[string]chan *ChannelPermissionResponse),
	}
}

func (p *ChannelPermission) RequestPermission(req *ChannelPermissionRequest) *ChannelPermissionResponse {
	ch := make(chan *ChannelPermissionResponse, 1)

	p.mu.Lock()
	p.pending[req.RequestID] = ch
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.pending, req.RequestID)
		p.mu.Unlock()
	}()

	select {
	case resp := <-ch:
		return resp
	default:
		return &ChannelPermissionResponse{Approved: false, Reason: "no handler registered"}
	}
}

func (p *ChannelPermission) Respond(requestID string, resp *ChannelPermissionResponse) {
	p.mu.Lock()
	ch, ok := p.pending[requestID]
	p.mu.Unlock()

	if ok {
		ch <- resp
	}
}

type ChannelNotification struct {
	mu      sync.RWMutex
	handler func(serverName string, content string)
}

func NewChannelNotification() *ChannelNotification {
	return &ChannelNotification{}
}

func (n *ChannelNotification) OnNotification(handler func(serverName string, content string)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handler = handler
}

func (n *ChannelNotification) Notify(serverName, content string) {
	n.mu.RLock()
	handler := n.handler
	n.mu.RUnlock()

	if handler != nil {
		handler(serverName, content)
	}
}