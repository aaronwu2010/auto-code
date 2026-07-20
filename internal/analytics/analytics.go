package analytics

import (
	"context"
	"sync"
	"time"
)

type AnalyticsEvent struct {
	Name      string                 `json:"name"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	SessionID string                 `json:"sessionId,omitempty"`
	UserID    string                 `json:"userId,omitempty"`
}

type AnalyticsSink interface {
	LogEvent(event AnalyticsEvent)
	Flush()
	Close()
}

type ConsoleSink struct{}

func NewConsoleSink() *ConsoleSink { return &ConsoleSink{} }

func (s *ConsoleSink) LogEvent(event AnalyticsEvent) {}
func (s *ConsoleSink) Flush()                        {}
func (s *ConsoleSink) Close()                        {}

type EventSamplingConfig struct {
	SampleRate float64 `json:"sampleRate"`
	MaxEvents  int     `json:"maxEvents"`
}

type AnalyticsService struct {
	mu          sync.RWMutex
	sinks       []AnalyticsSink
	disabled    bool
	eventCount  int64
	sampling    EventSamplingConfig
	sessionID   string
	userID      string
}

func NewAnalyticsService(sessionID, userID string) *AnalyticsService {
	return &AnalyticsService{
		sinks:     []AnalyticsSink{NewConsoleSink()},
		sampling:  EventSamplingConfig{SampleRate: 1.0, MaxEvents: 10000},
		sessionID: sessionID,
		userID:    userID,
	}
}

func (s *AnalyticsService) LogEvent(name string, properties map[string]interface{}) {
	if s.disabled {
		return
	}

	s.mu.RLock()
	sinks := make([]AnalyticsSink, len(s.sinks))
	copy(sinks, s.sinks)
	s.mu.RUnlock()

	event := AnalyticsEvent{
		Name:       name,
		Properties: properties,
		Timestamp:  time.Now(),
		SessionID:  s.sessionID,
		UserID:     s.userID,
	}

	for _, sink := range sinks {
		sink.LogEvent(event)
	}
}

func (s *AnalyticsService) LogEventAsync(name string, properties map[string]interface{}) {
	go s.LogEvent(name, properties)
}

func (s *AnalyticsService) AddSink(sink AnalyticsSink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sinks = append(s.sinks, sink)
}

func (s *AnalyticsService) Flush() {
	s.mu.RLock()
	sinks := make([]AnalyticsSink, len(s.sinks))
	copy(sinks, s.sinks)
	s.mu.RUnlock()

	for _, sink := range sinks {
		sink.Flush()
	}
}

func (s *AnalyticsService) Close() {
	s.mu.RLock()
	sinks := make([]AnalyticsSink, len(s.sinks))
	copy(sinks, s.sinks)
	s.mu.RUnlock()

	for _, sink := range sinks {
		sink.Close()
	}
}

func (s *AnalyticsService) IsDisabled() bool {
	return s.disabled
}

func (s *AnalyticsService) SetDisabled(disabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disabled = disabled
}

func (s *AnalyticsService) SetSamplingConfig(config EventSamplingConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampling = config
}

func (s *AnalyticsService) InitializeGates(ctx context.Context) {}

func SanitizeToolNameForAnalytics(toolName string) string {
	if len(toolName) > 64 {
		return toolName[:64]
	}
	return toolName
}