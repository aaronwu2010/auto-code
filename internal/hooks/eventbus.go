package hooks

import (
	"context"
	"fmt"
	"sync"
)

type HookEventHandler func(event HookExecutionEvent)

type HookExecutionEvent struct {
	Type       HookEvent `json:"type"`
	HookName   string    `json:"hookName"`
	Command    string    `json:"command,omitempty"`
	Status     string    `json:"status"`
	DurationMs int64     `json:"durationMs,omitempty"`
}

type HookEventBus struct {
	mu       sync.RWMutex
	handlers []HookEventHandler
	enabled  bool
}

func NewHookEventBus() *HookEventBus {
	return &HookEventBus{enabled: true}
}

func (b *HookEventBus) RegisterHandler(handler HookEventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, handler)
}

func (b *HookEventBus) EmitStarted(event HookEvent, hookName, command string) {
	if !b.enabled {
		return
	}
	b.emit(HookExecutionEvent{
		Type:     event,
		HookName: hookName,
		Command:  command,
		Status:   "started",
	})
}

func (b *HookEventBus) EmitResponse(event HookEvent, hookName, command string, durationMs int64) {
	if !b.enabled {
		return
	}
	b.emit(HookExecutionEvent{
		Type:       event,
		HookName:   hookName,
		Command:    command,
		Status:     "completed",
		DurationMs: durationMs,
	})
}

func (b *HookEventBus) EmitError(event HookEvent, hookName, command string, durationMs int64) {
	if !b.enabled {
		return
	}
	b.emit(HookExecutionEvent{
		Type:       event,
		HookName:   hookName,
		Command:    command,
		Status:     "error",
		DurationMs: durationMs,
	})
}

func (b *HookEventBus) SetEnabled(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = enabled
}

func (b *HookEventBus) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = nil
	b.enabled = true
}

func (b *HookEventBus) emit(event HookExecutionEvent) {
	b.mu.RLock()
	handlers := make([]HookEventHandler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}

type PostSamplingHook func(ctx context.Context, input HookInput) (HookJSONOutput, error)

type PostSamplingHookRegistry struct {
	mu    sync.RWMutex
	hooks []PostSamplingHook
}

func NewPostSamplingHookRegistry() *PostSamplingHookRegistry {
	return &PostSamplingHookRegistry{}
}

func (r *PostSamplingHookRegistry) Register(hook PostSamplingHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, hook)
}

func (r *PostSamplingHookRegistry) Execute(ctx context.Context, input HookInput) []HookJSONOutput {
	r.mu.RLock()
	hooks := make([]PostSamplingHook, len(r.hooks))
	copy(hooks, r.hooks)
	r.mu.RUnlock()

	var results []HookJSONOutput
	for _, hook := range hooks {
		output, err := hook(ctx, input)
		if err != nil {
			continue
		}
		results = append(results, output)
	}
	return results
}

func (r *PostSamplingHookRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = nil
}

type AsyncHookEntry struct {
	ID        string
	HookEvent HookEvent
	HookName  string
	Command   string
	CreatedAt int64
	Response  chan HookJSONOutput
	Delivered bool
}

type AsyncHookRegistry struct {
	mu     sync.RWMutex
	hooks  map[string]*AsyncHookEntry
	nextID int64
}

func NewAsyncHookRegistry() *AsyncHookRegistry {
	return &AsyncHookRegistry{
		hooks: make(map[string]*AsyncHookEntry),
	}
}

func (r *AsyncHookRegistry) Register(hookEvent HookEvent, hookName, command string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	id := fmtAsyncHookID(r.nextID)

	entry := &AsyncHookEntry{
		ID:        id,
		HookEvent: hookEvent,
		HookName:  hookName,
		Command:   command,
		Response:  make(chan HookJSONOutput, 1),
	}

	r.hooks[id] = entry
	return id
}

func (r *AsyncHookRegistry) CheckForResponses() map[string]HookJSONOutput {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make(map[string]HookJSONOutput)
	for id, entry := range r.hooks {
		select {
		case resp := <-entry.Response:
			results[id] = resp
			entry.Delivered = true
		default:
		}
	}
	return results
}

func (r *AsyncHookRegistry) RemoveDelivered() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, entry := range r.hooks {
		if entry.Delivered {
			delete(r.hooks, id)
		}
	}
}

func (r *AsyncHookRegistry) Finalize() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, entry := range r.hooks {
		if !entry.Delivered {
			close(entry.Response)
			delete(r.hooks, id)
		}
	}
}

func (r *AsyncHookRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, entry := range r.hooks {
		if !entry.Delivered {
			close(entry.Response)
		}
	}
	r.hooks = make(map[string]*AsyncHookEntry)
}

var asyncHookIDCounter int64

func fmtAsyncHookID(n int64) string {
	return fmt.Sprintf("async-hook-%d", n)
}