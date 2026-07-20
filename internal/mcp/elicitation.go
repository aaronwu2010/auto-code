package mcp

import (
	"context"
	"sync"
)

type ElicitationHandler struct {
	mu       sync.Mutex
	handlers map[string]func(*ElicitationRequest) *ElicitationResponse
}

type ElicitationRequest struct {
	ServerName string         `json:"server_name"`
	RequestID  any            `json:"request_id"`
	Params     ElicitParams   `json:"params"`
}

type ElicitParams struct {
	Message string         `json:"message"`
	Fields  []ElicitField  `json:"requestedSchema,omitempty"`
}

type ElicitField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type ElicitationResponse struct {
	Action string            `json:"action"`
	Content map[string]any   `json:"content,omitempty"`
}

func NewElicitationHandler() *ElicitationHandler {
	return &ElicitationHandler{
		handlers: make(map[string]func(*ElicitationRequest) *ElicitationResponse),
	}
}

func (h *ElicitationHandler) RegisterHandler(serverName string, handler func(*ElicitationRequest) *ElicitationResponse) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[serverName] = handler
}

func (h *ElicitationHandler) HandleElicitation(_ context.Context, req *ElicitationRequest) *ElicitationResponse {
	h.mu.Lock()
	handler, ok := h.handlers[req.ServerName]
	h.mu.Unlock()

	if ok {
		return handler(req)
	}

	return &ElicitationResponse{
		Action: "cancel",
	}
}