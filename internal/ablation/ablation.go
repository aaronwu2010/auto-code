package ablation

import "os"

const (
	EnvAblationBaseline = "AUTO_CODE_ABLATION_BASELINE"

	EnvDisableThinking     = "AUTO_CODE_DISABLE_THINKING"
	EnvDisableCompact      = "AUTO_CODE_DISABLE_COMPACT"
	EnvDisableAutoCompact  = "AUTO_CODE_DISABLE_AUTO_COMPACT"
	EnvDisableAutoMemory   = "AUTO_CODE_DISABLE_AUTO_MEMORY"
	EnvDisableBackground   = "AUTO_CODE_DISABLE_BACKGROUND_TASKS"
	EnvDisableHistorySnip  = "AUTO_CODE_DISABLE_HISTORY_SNIP"
	EnvDisableContextCollapse = "AUTO_CODE_DISABLE_CONTEXT_COLLAPSE"
)

type AblationFlags struct {
	DisableThinking       bool
	DisableCompact        bool
	DisableAutoCompact    bool
	DisableAutoMemory     bool
	DisableBackground     bool
	DisableHistorySnip    bool
	DisableContextCollapse bool
}

var cachedFlags *AblationFlags

func GetAblationFlags() AblationFlags {
	if cachedFlags != nil {
		return *cachedFlags
	}

	flags := AblationFlags{}

	if os.Getenv(EnvAblationBaseline) != "" {
		flags.DisableThinking = true
		flags.DisableCompact = true
		flags.DisableAutoCompact = true
		flags.DisableAutoMemory = true
		flags.DisableBackground = true
		flags.DisableHistorySnip = true
		flags.DisableContextCollapse = true
	}

	if os.Getenv(EnvDisableThinking) != "" {
		flags.DisableThinking = true
	}
	if os.Getenv(EnvDisableCompact) != "" {
		flags.DisableCompact = true
	}
	if os.Getenv(EnvDisableAutoCompact) != "" {
		flags.DisableAutoCompact = true
	}
	if os.Getenv(EnvDisableAutoMemory) != "" {
		flags.DisableAutoMemory = true
	}
	if os.Getenv(EnvDisableBackground) != "" {
		flags.DisableBackground = true
	}
	if os.Getenv(EnvDisableHistorySnip) != "" {
		flags.DisableHistorySnip = true
	}
	if os.Getenv(EnvDisableContextCollapse) != "" {
		flags.DisableContextCollapse = true
	}

	cachedFlags = &flags
	return flags
}

func IsAblationBaseline() bool {
	return os.Getenv(EnvAblationBaseline) != ""
}

func IsThinkingDisabled() bool {
	return GetAblationFlags().DisableThinking
}

func IsCompactDisabled() bool {
	return GetAblationFlags().DisableCompact
}

func IsAutoCompactDisabled() bool {
	return GetAblationFlags().DisableAutoCompact
}

func IsAutoMemoryDisabled() bool {
	return GetAblationFlags().DisableAutoMemory
}

func IsBackgroundTasksDisabled() bool {
	return GetAblationFlags().DisableBackground
}

func IsHistorySnipDisabled() bool {
	return GetAblationFlags().DisableHistorySnip
}

func IsContextCollapseDisabled() bool {
	return GetAblationFlags().DisableContextCollapse
}

func ResetAblationCache() {
	cachedFlags = nil
}
