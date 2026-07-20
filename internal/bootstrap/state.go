package state

import (
	"sync/atomic"
	"time"

	"github.com/auto-code/auto-code/internal/types"
)

type BootstrapState struct {
	OriginalCwd              string
	ProjectRoot              string
	TotalCostUSD             float64
	TotalAPIDuration         time.Duration
	TotalToolDuration        time.Duration
	StartTime                time.Time
	LastInteractionTime      atomic.Int64
	SessionID                types.SessionID
	ParentSessionID          types.SessionID
	MainLoopModelOverride    types.ModelSetting
	InitialMainLoopModel     types.ModelSetting
	IsInteractive            bool
	ClientType               string
	SessionPersistenceDisabled bool
}

var globalBootstrap *BootstrapState

func InitBootstrap(cwd string) *BootstrapState {
	globalBootstrap = &BootstrapState{
		OriginalCwd:         cwd,
		ProjectRoot:         cwd,
		StartTime:           time.Now(),
		SessionID:           generateSessionID(),
		ClientType:          "cli",
		IsInteractive:       true,
	}
	globalBootstrap.LastInteractionTime.Store(time.Now().UnixMilli())
	return globalBootstrap
}

func GetBootstrap() *BootstrapState {
	return globalBootstrap
}

func (bs *BootstrapState) GetSessionID() types.SessionID {
	return bs.SessionID
}

func (bs *BootstrapState) GetOriginalCwd() string {
	return bs.OriginalCwd
}

func (bs *BootstrapState) GetProjectRoot() string {
	return bs.ProjectRoot
}

func (bs *BootstrapState) GetTotalCostUSD() float64 {
	return bs.TotalCostUSD
}

func (bs *BootstrapState) AddToTotalCost(cost float64) {
	bs.TotalCostUSD += cost
}

func (bs *BootstrapState) GetMainLoopModelOverride() types.ModelSetting {
	return bs.MainLoopModelOverride
}

func (bs *BootstrapState) SetMainLoopModelOverride(model types.ModelSetting) {
	bs.MainLoopModelOverride = model
}

func (bs *BootstrapState) UpdateLastInteractionTime() {
	bs.LastInteractionTime.Store(time.Now().UnixMilli())
}

func (bs *BootstrapState) GetLastInteractionTime() time.Time {
	return time.UnixMilli(bs.LastInteractionTime.Load())
}

func generateSessionID() types.SessionID {
	return types.SessionID("session-" + time.Now().Format("20060102150405"))
}