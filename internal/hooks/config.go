package hooks

import (
	"sort"
)

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
	Event    HookEvent         `json:"event"`
	Matchers []MatcherWithMeta `json:"matchers"`
}

type MatcherWithMeta struct {
	Matcher string          `json:"matcher"`
	Hooks   []HookCommand   `json:"hooks"`
	Meta    MatcherMetadata `json:"meta"`
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

// SortMatchersByPriority 按优先级降序排序（高优先级先执行）
// 当前实现基于 matcher 字符串的字母序作为启发式排序（实际项目中应根据 HookSource 或
// 显式 Priority 字段排序）
func SortMatchersByPriority(matchers []HookMatcher) []HookMatcher {
	sorted := make([]HookMatcher, len(matchers))
	copy(sorted, matchers)
	sort.Slice(sorted, func(i, j int) bool {
		// 字母序倒序：字母靠前的 matcher 排在后面（优先级低），
		// 字母靠后的排在前面（优先级高）
		return sorted[i].Matcher > sorted[j].Matcher
	})
	return sorted
}
