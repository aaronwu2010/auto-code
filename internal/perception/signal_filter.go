package perception

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// BaseSignalFilter 基础信号过滤器
// 提供基于规则的输入过滤功能
type BaseSignalFilter struct {
	rules []*FilterRule
	mu    sync.RWMutex

	// 统计信息
	totalFiltered  int64
	totalProcessed int64
}

// NewBaseSignalFilter 创建基础信号过滤器
func NewBaseSignalFilter() *BaseSignalFilter {
	return &BaseSignalFilter{
		rules: make([]*FilterRule, 0),
	}
}

// Filter 过滤输入
func (f *BaseSignalFilter) Filter(ctx context.Context, input *InputData) (*OutputData, bool, error) {
	f.mu.Lock()
	f.totalProcessed++
	f.mu.Unlock()

	// 按优先级排序规则
	sortedRules := f.getSortedRules()

	// 应用过滤规则
	for _, rule := range sortedRules {
		if !rule.Enabled {
			continue
		}

		matched, err := f.matchRule(input, rule)
		if err != nil {
			return nil, false, fmt.Errorf("rule matching failed: %w", err)
		}

		if matched {
			output := f.applyAction(input, rule)

			// 如果动作是拒绝，标记为已过滤
			if rule.Action.Type == FilterActionDeny {
				f.mu.Lock()
				f.totalFiltered++
				f.mu.Unlock()

				return output, true, nil
			}

			// 如果动作是修改，更新输入
			if rule.Action.Type == FilterActionModify {
				return output, false, nil
			}
		}
	}

	// 所有规则都通过了，返回未过滤的结果
	return &OutputData{
		ProcessedContent: input.Content,
		Filtered:         false,
		Confidence:       1.0,
	}, false, nil
}

// AddRule 添加过滤规则
func (f *BaseSignalFilter) AddRule(rule *FilterRule) error {
	if rule == nil {
		return fmt.Errorf("rule is nil")
	}

	if rule.ID == "" {
		return fmt.Errorf("rule ID is required")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// 检查规则是否已存在
	for _, r := range f.rules {
		if r.ID == rule.ID {
			return fmt.Errorf("rule with ID %s already exists", rule.ID)
		}
	}

	f.rules = append(f.rules, rule)
	return nil
}

// RemoveRule 移除过滤规则
func (f *BaseSignalFilter) RemoveRule(ruleID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, r := range f.rules {
		if r.ID == ruleID {
			f.rules = append(f.rules[:i], f.rules[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("rule with ID %s not found", ruleID)
}

// GetRules 获取所有过滤规则
func (f *BaseSignalFilter) GetRules() ([]*FilterRule, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]*FilterRule, len(f.rules))
	copy(result, f.rules)
	return result, nil
}

// ClearRules 清空所有过滤规则
func (f *BaseSignalFilter) ClearRules() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.rules = make([]*FilterRule, 0)
	return nil
}

// matchRule 匹配规则
func (f *BaseSignalFilter) matchRule(input *InputData, rule *FilterRule) (bool, error) {
	cond := rule.Condition

	// 检查输入类型
	if len(cond.InputTypes) > 0 {
		typeMatched := false
		for _, t := range cond.InputTypes {
			if input.Type == t {
				typeMatched = true
				break
			}
		}
		if !typeMatched {
			return false, nil
		}
	}

	// 检查来源
	if len(cond.Sources) > 0 {
		sourceMatched := false
		for _, s := range cond.Sources {
			if input.Source == s {
				sourceMatched = true
				break
			}
		}
		if !sourceMatched {
			return false, nil
		}
	}

	// 检查优先级范围
	if cond.MinPriority > 0 && input.Priority < cond.MinPriority {
		return false, nil
	}
	if cond.MaxPriority > 0 && input.Priority > cond.MaxPriority {
		return false, nil
	}

	// 检查包含关键词
	if len(cond.Contains) > 0 {
		contentMatched := false
		for _, keyword := range cond.Contains {
			if strings.Contains(input.Content, keyword) {
				contentMatched = true
				break
			}
		}
		if !contentMatched {
			return false, nil
		}
	}

	// 检查排除关键词
	if len(cond.Excludes) > 0 {
		for _, keyword := range cond.Excludes {
			if strings.Contains(input.Content, keyword) {
				return false, nil
			}
		}
	}

	// 检查正则表达式模式
	if cond.Pattern != "" {
		matched, err := regexp.MatchString(cond.Pattern, input.Content)
		if err != nil {
			return false, fmt.Errorf("regex match failed: %w", err)
		}
		if !matched {
			return false, nil
		}
	}

	return true, nil
}

// applyAction 应用动作
func (f *BaseSignalFilter) applyAction(input *InputData, rule *FilterRule) *OutputData {
	output := &OutputData{
		ProcessedContent: input.Content,
		Filtered:         rule.Action.Type == FilterActionDeny,
		FilterReason:     rule.Action.Message,
		Confidence:       1.0,
	}

	// 如果是修改动作，应用替换规则
	if rule.Action.Type == FilterActionModify && len(rule.Action.Replacements) > 0 {
		content := input.Content
		for old, new := range rule.Action.Replacements {
			content = strings.ReplaceAll(content, old, new)
		}
		output.ProcessedContent = content
	}

	return output
}

// getSortedRules 获取排序后的规则
func (f *BaseSignalFilter) getSortedRules() []*FilterRule {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// 复制规则列表
	sorted := make([]*FilterRule, len(f.rules))
	copy(sorted, f.rules)

	// 按优先级从高到低排序（高优先级先执行）
	// 使用冒泡排序
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			// 注意：Priority 值越大优先级越高，所以大的应排在前面
			if sorted[j].Priority < sorted[j+1].Priority {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	return sorted
}

// GetStats 获取统计信息
func (f *BaseSignalFilter) GetStats() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return map[string]interface{}{
		"total_filtered":  f.totalFiltered,
		"total_processed": f.totalProcessed,
		"filter_rate":     float64(f.totalFiltered) / float64(f.totalProcessed),
		"rule_count":      len(f.rules),
	}
}

// Reset 重置统计信息
func (f *BaseSignalFilter) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.totalFiltered = 0
	f.totalProcessed = 0
}

// DefaultFilterRules 返回默认过滤规则
func DefaultFilterRules() []*FilterRule {
	return []*FilterRule{
		{
			ID:          "deny_empty_content",
			Name:        "拒绝空内容",
			Description: "拒绝内容为空的输入",
			Enabled:     true,
			Priority:    100,
			Condition: FilterCondition{
				Contains: []string{},
			},
			Action: FilterAction{
				Type:    FilterActionDeny,
				Message: "Input content is empty",
			},
		},
		{
			ID:          "warn_long_content",
			Name:        "警告长内容",
			Description: "警告超过10000字符的内容",
			Enabled:     true,
			Priority:    50,
			Condition: FilterCondition{
				Pattern: ".{10000,}",
			},
			Action: FilterAction{
				Type:    FilterActionWarn,
				Message: "Input content is very long (over 10000 characters)",
			},
		},
	}
}
