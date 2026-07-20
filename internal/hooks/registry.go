package hooks

import (
	"fmt"
	"sync"
)

type HookRegistry struct {
	mu             sync.RWMutex
	settings       HooksSettings
	sessionHooks   map[string]*SessionStore
	configSnapshot *HooksConfigSnapshot
}

type SessionStore struct {
	Hooks map[HookEvent][]SessionHookMatcher
}

type SessionHookMatcher struct {
	Matcher    string
	SkillRoot  string
	Hooks      []HookCommand
	OnSuccess  func(hook HookCommand, result *AggregatedHookResult)
}

type HooksConfigSnapshot struct {
	AllowManagedHooksOnly         bool
	DisableAllHooksIncludingManaged bool
	Settings                      HooksSettings
}

func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		settings:     make(HooksSettings),
		sessionHooks: make(map[string]*SessionStore),
	}
}

func (r *HookRegistry) RegisterSettings(settings HooksSettings) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings = settings
}

func (r *HookRegistry) GetSettings() HooksSettings {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(HooksSettings, len(r.settings))
	for k, v := range r.settings {
		result[k] = v
	}
	return result
}

func (r *HookRegistry) GetMatchersForEvent(event HookEvent) []HookMatcher {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.configSnapshot != nil {
		if r.configSnapshot.DisableAllHooksIncludingManaged {
			return nil
		}
		if r.configSnapshot.AllowManagedHooksOnly {
			return nil
		}
	}

	return r.settings[event]
}

func (r *HookRegistry) GetSessionMatchersForEvent(event HookEvent) []HookMatcher {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []HookMatcher
	for _, store := range r.sessionHooks {
		if matchers, ok := store.Hooks[event]; ok {
			for _, sm := range matchers {
				var hooks []HookCommand
				for _, h := range sm.Hooks {
					hooks = append(hooks, h)
				}
				if len(hooks) > 0 {
					result = append(result, HookMatcher{
						Matcher: sm.Matcher,
						Hooks:   hooks,
					})
				}
			}
		}
	}
	return result
}

func (r *HookRegistry) AddSessionHook(sessionID string, event HookEvent, matcher string, hook HookCommand) {
	r.mu.Lock()
	defer r.mu.Unlock()

	store, ok := r.sessionHooks[sessionID]
	if !ok {
		store = &SessionStore{Hooks: make(map[HookEvent][]SessionHookMatcher)}
		r.sessionHooks[sessionID] = store
	}

	eventMatchers := store.Hooks[event]
	for i, em := range eventMatchers {
		if em.Matcher == matcher {
			eventMatchers[i].Hooks = append(em.Hooks, hook)
			store.Hooks[event] = eventMatchers
			return
		}
	}

	store.Hooks[event] = append(eventMatchers, SessionHookMatcher{
		Matcher: matcher,
		Hooks:   []HookCommand{hook},
	})
}

func (r *HookRegistry) AddFunctionHook(sessionID string, event HookEvent, matcher string, callback FunctionHookCallback, errorMessage string, timeout int) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	hookID := fmtHookID()

	hook := &FunctionHook{
		Type:         HookTypeFunction,
		ID:           hookID,
		Timeout:      timeout,
		Callback:     callback,
		ErrorMessage: errorMessage,
	}

	store, ok := r.sessionHooks[sessionID]
	if !ok {
		store = &SessionStore{Hooks: make(map[HookEvent][]SessionHookMatcher)}
		r.sessionHooks[sessionID] = store
	}

	eventMatchers := store.Hooks[event]
	for i, em := range eventMatchers {
		if em.Matcher == matcher {
			eventMatchers[i].Hooks = append(em.Hooks, hook)
			store.Hooks[event] = eventMatchers
			return hookID
		}
	}

	store.Hooks[event] = append(eventMatchers, SessionHookMatcher{
		Matcher: matcher,
		Hooks:   []HookCommand{hook},
	})

	return hookID
}

func (r *HookRegistry) RemoveFunctionHook(sessionID string, event HookEvent, hookID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	store, ok := r.sessionHooks[sessionID]
	if !ok {
		return
	}

	eventMatchers := store.Hooks[event]
	var updated []SessionHookMatcher
	for _, em := range eventMatchers {
		var remaining []HookCommand
		for _, h := range em.Hooks {
			if fnHook, ok := h.(*FunctionHook); ok && fnHook.ID == hookID {
				continue
			}
			remaining = append(remaining, h)
		}
		if len(remaining) > 0 {
			updated = append(updated, SessionHookMatcher{
				Matcher: em.Matcher,
				Hooks:   remaining,
			})
		}
	}

	if len(updated) > 0 {
		store.Hooks[event] = updated
	} else {
		delete(store.Hooks, event)
	}
}

func (r *HookRegistry) RemoveSessionHook(sessionID string, event HookEvent, hook HookCommand) {
	r.mu.Lock()
	defer r.mu.Unlock()

	store, ok := r.sessionHooks[sessionID]
	if !ok {
		return
	}

	eventMatchers := store.Hooks[event]
	var updated []SessionHookMatcher
	for _, em := range eventMatchers {
		var remaining []HookCommand
		for _, h := range em.Hooks {
			if !isHookEqual(h, hook) {
				remaining = append(remaining, h)
			}
		}
		if len(remaining) > 0 {
			updated = append(updated, SessionHookMatcher{
				Matcher: em.Matcher,
				Hooks:   remaining,
			})
		}
	}

	if len(updated) > 0 {
		store.Hooks[event] = updated
	} else {
		delete(store.Hooks, event)
	}
}

func (r *HookRegistry) ClearSessionHooks(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessionHooks, sessionID)
}

func (r *HookRegistry) GetSessionHooks(sessionID string, event HookEvent) []HookMatcher {
	r.mu.RLock()
	defer r.mu.RUnlock()

	store, ok := r.sessionHooks[sessionID]
	if !ok {
		return nil
	}

	matchers := store.Hooks[event]
	if matchers == nil {
		return nil
	}

	var result []HookMatcher
	for _, sm := range matchers {
		var hooks []HookCommand
		for _, h := range sm.Hooks {
			hooks = append(hooks, h)
		}
		if len(hooks) > 0 {
			result = append(result, HookMatcher{
				Matcher: sm.Matcher,
				Hooks:   hooks,
			})
		}
	}
	return result
}

func (r *HookRegistry) CaptureSnapshot() *HooksConfigSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.configSnapshot != nil {
		return r.configSnapshot
	}

	snapshot := &HooksConfigSnapshot{
		Settings: r.settings,
	}
	return snapshot
}

func (r *HookRegistry) UpdateSnapshot(snapshot *HooksConfigSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configSnapshot = snapshot
}

func (r *HookRegistry) ResetSnapshot() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configSnapshot = nil
}

func isHookEqual(a, b HookCommand) bool {
	aType := a.GetType()
	bType := b.GetType()
	if aType != bType {
		return false
	}

	switch a := a.(type) {
	case *BashCommandHook:
		if b, ok := b.(*BashCommandHook); ok {
			return a.Command == b.Command && a.Shell == b.Shell
		}
	case *PromptHook:
		if b, ok := b.(*PromptHook); ok {
			return a.Prompt == b.Prompt && a.Model == b.Model
		}
	case *HTTPHook:
		if b, ok := b.(*HTTPHook); ok {
			return a.URL == b.URL
		}
	case *AgentHook:
		if b, ok := b.(*AgentHook); ok {
			return a.Prompt == b.Prompt && a.Model == b.Model
		}
	case *FunctionHook:
		if b, ok := b.(*FunctionHook); ok {
			return a.ID == b.ID
		}
	}
	return false
}

var hookIDCounter int64

func fmtHookID() string {
	hookIDCounter++
	return fmt.Sprintf("function-hook-%d", hookIDCounter)
}