package hooks

type HookMatcher struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []HookCommand `json:"hooks"`
}

type HooksSettings map[HookEvent][]HookMatcher

type HookSource string

const (
	HookSourceUser    HookSource = "user"
	HookSourceProject HookSource = "project"
	HookSourceLocal   HookSource = "local"
	HookSourcePolicy  HookSource = "policy"
	HookSourcePlugin  HookSource = "plugin"
	HookSourceSession HookSource = "session"
)

type MatcherMetadata struct {
	Source     HookSource `json:"source"`
	PluginName string     `json:"pluginName,omitempty"`
	SkillRoot  string     `json:"skillRoot,omitempty"`
}

type HookEventMetadata struct {
	Event     HookEvent          `json:"event"`
	Matchers  []MatcherWithMeta  `json:"matchers"`
}

type MatcherWithMeta struct {
	Matcher string           `json:"matcher"`
	Hooks   []HookCommand    `json:"hooks"`
	Meta    MatcherMetadata  `json:"meta"`
}

func GroupHooksByEventAndMatcher(settings HooksSettings, source HookSource, pluginName string) []HookEventMetadata {
	var result []HookEventMetadata
	for _, event := range AllHookEvents {
		matchers, ok := settings[event]
		if !ok || len(matchers) == 0 {
			continue
		}
		var matchersWithMeta []MatcherWithMeta
		for _, m := range matchers {
			matchersWithMeta = append(matchersWithMeta, MatcherWithMeta{
				Matcher: m.Matcher,
				Hooks:   m.Hooks,
				Meta: MatcherMetadata{
					Source:     source,
					PluginName: pluginName,
				},
			})
		}
		result = append(result, HookEventMetadata{
			Event:    event,
			Matchers: matchersWithMeta,
		})
	}
	return result
}

func GetHooksForEvent(settings HooksSettings, event HookEvent) []HookMatcher {
	return settings[event]
}

func SortMatchersByPriority(matchers []HookMatcher) []HookMatcher {
	sorted := make([]HookMatcher, len(matchers))
	copy(sorted, matchers)
	return sorted
}