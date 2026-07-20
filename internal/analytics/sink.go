package analytics

import "sync"

type SinkName string

const (
	SinkDatadog    SinkName = "datadog"
	SinkFirstParty SinkName = "firstparty"
)

type SinkKillSwitch struct {
	mu      sync.RWMutex
	killed  map[SinkName]bool
}

func NewSinkKillSwitch() *SinkKillSwitch {
	return &SinkKillSwitch{
		killed: make(map[SinkName]bool),
	}
}

func (k *SinkKillSwitch) IsKilled(name SinkName) bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.killed[name]
}

func (k *SinkKillSwitch) Kill(name SinkName) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.killed[name] = true
}

func (k *SinkKillSwitch) Revive(name SinkName) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.killed, name)
}

func (k *SinkKillSwitch) Reset() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.killed = make(map[SinkName]bool)
}

func IsAnalyticsDisabled() bool {
	return false
}

func IsFeedbackSurveyDisabled() bool {
	return false
}