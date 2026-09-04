package prompts

import (
	"fmt"
	"strings"
)

// ChecklistEngine 场景/风险 checklist 引擎
//
// 核心思想：不污染 System Prompt（保持稳定短），所有 checklist 走 IsMeta user message 注入
// 分层触发：
//   - L1 任务类型层 → 已有 DynamicPromptEngine.BuildTaskInstruction
//   - L2 语言层     → 已有 buildLangInstruction
//   - L3 场景层     → 这里实现：DetectScenes → SceneChecklistText
//   - L4 风险层     → 这里实现：DetectRisks  → RiskChecklistText
type ChecklistEngine struct {
	enabled bool
}

func NewChecklistEngine(enabled bool) *ChecklistEngine {
	return &ChecklistEngine{enabled: enabled}
}

// BuildSceneChecklists 根据场景列表构建所有场景 checklist
func (ce *ChecklistEngine) BuildSceneChecklists(scenes []SceneType) string {
	if ce == nil || !ce.enabled || len(scenes) == 0 {
		return ""
	}

	var sb strings.Builder
	seen := make(map[SceneType]bool)

	for _, scene := range scenes {
		if seen[scene] {
			continue
		}
		seen[scene] = true

		text := SceneChecklistText(scene)
		if text != "" {
			sb.WriteString(text)
			sb.WriteString("\n\n")
		}
	}

	return strings.TrimSpace(sb.String())
}

// BuildRiskChecklists 根据风险列表构建所有风险 checklist
func (ce *ChecklistEngine) BuildRiskChecklists(risks []RiskType) string {
	if ce == nil || !ce.enabled || len(risks) == 0 {
		return ""
	}

	var sb strings.Builder
	seen := make(map[RiskType]bool)

	for _, risk := range risks {
		if seen[risk] {
			continue
		}
		seen[risk] = true

		text := RiskChecklistText(risk)
		if text != "" {
			sb.WriteString(text)
			sb.WriteString("\n\n")
		}
	}

	return strings.TrimSpace(sb.String())
}

// BuildAll 一次性构建所有 checklist（L1-L5）
// 返回完整的 IsMeta message 内容
//
// L1 任务类型层 + L2 语言层: 来自 DynamicPromptEngine.BuildTaskInstruction
// L3 场景层 + L4 风险层: 来自 ChecklistEngine 自身
// L5 全栈交付层: StackDeliveryChecklist（feature/build 触发）
// L6 参考迁移层: ReferenceMigrationGuide（检测到参考关键字时触发）
func (ce *ChecklistEngine) BuildAll(taskType TaskType, lang ProjectLang, scenes []SceneType, risks []RiskType, prompt string) string {
	if ce == nil || !ce.enabled {
		return ""
	}

	var sb strings.Builder

	// L1 + L2: 任务类型 + 语言特化（已有 DynamicPromptEngine 实现）
	if de := NewDynamicPromptEngine(true); de != nil {
		if taskInstruction := de.BuildTaskInstruction(taskType, lang); taskInstruction != "" {
			sb.WriteString(taskInstruction)
			sb.WriteString("\n\n")
		}
	}

	// L3: 场景 checklist
	if sceneText := ce.BuildSceneChecklists(scenes); sceneText != "" {
		sb.WriteString("---\n\n")
		sb.WriteString(fmt.Sprintf("[Detected Scenes] %v\n\n", scenes))
		sb.WriteString(sceneText)
		sb.WriteString("\n\n")
	}

	// L4: 风险 checklist
	if riskText := ce.BuildRiskChecklists(risks); riskText != "" {
		sb.WriteString("---\n\n")
		sb.WriteString(fmt.Sprintf("[Detected Risks] %v\n\n", risks))
		sb.WriteString(riskText)
		sb.WriteString("\n\n")
	}

	// L5: 全栈交付清单（feature/build 任务触发）
	if deliveryText := StackDeliveryChecklist(taskType, ProjectUnknown, lang); deliveryText != "" {
		sb.WriteString("---\n\n")
		sb.WriteString(deliveryText)
		sb.WriteString("\n\n")
	}

	// L6: 参考项目迁移指南（检测到参考关键字时触发）
	if DetectReferencePrompt(prompt) {
		sb.WriteString("---\n\n")
		sb.WriteString(ReferenceMigrationGuide())
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String())
}
