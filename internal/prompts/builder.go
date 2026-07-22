package prompts

import (
	"context"
	"strings"
)

// SystemPromptConfig 系统提示词配置
type SystemPromptConfig struct {
	LanguagePreference string
	OutputStyle        OutputStyle
	CustomInstructions string
}

// BuildSystemPrompt 构建完整的系统提示词
func BuildSystemPrompt(ctx context.Context, config SystemPromptConfig) string {
	builder := NewSystemPromptBuilder()

	// 添加基础介绍
	builder.AddSection(GetSimpleIntroSection())

	// 添加工具使用指导（关键：告诉 AI 使用工具而不是只返回文本）
	builder.AddSection(GetToolUsageSection())

	// 添加系统段落
	builder.AddSection(GetSystemSection())

	// 添加任务执行段落
	builder.AddSection(GetDoingTasksSection())

	// 添加行动指南段落
	builder.AddSection(GetActionsSection())

	// 添加语言设置（如果有）
	if config.LanguagePreference != "" {
		builder.AddSection(GetLanguageSection(config.LanguagePreference))
	}

	// 添加输出风格（如果有）
	if config.OutputStyle != "" && config.OutputStyle != OutputStyleDefault {
		styleSection := GetOutputStyleSection(config.OutputStyle)
		if styleSection != "" {
			builder.AddSection(styleSection)
		}
	}

	// 添加自定义指令（如果有）
	if config.CustomInstructions != "" {
		builder.AddSection(config.CustomInstructions)
	}

	return builder.Build()
}

// BuildMinimalSystemPrompt 构建最小化的系统提示词
func BuildMinimalSystemPrompt() string {
	builder := NewSystemPromptBuilder()

	builder.AddSection(GetSimpleIntroSection())
	builder.AddSection(GetToolUsageSection())
	builder.AddSection(GetSystemSection())
	builder.AddSection(GetDoingTasksSection())

	return builder.Build()
}

// BuildSystemPromptForTool 为特定工具构建系统提示词
func BuildSystemPromptForTool(toolName string, basePrompt string) string {
	if basePrompt == "" {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("# Tool: " + toolName + "\n\n")
	sb.WriteString(basePrompt)
	sb.WriteString("\n\n")
	sb.WriteString(GetCyberRiskInstruction())

	return sb.String()
}

// GetDefaultSystemPrompt 获取默认系统提示词
func GetDefaultSystemPrompt() string {
	return BuildSystemPrompt(context.Background(), SystemPromptConfig{
		OutputStyle: OutputStyleDefault,
	})
}

// GetExplanatorySystemPrompt 获取解释性系统提示词
func GetExplanatorySystemPrompt() string {
	return BuildSystemPrompt(context.Background(), SystemPromptConfig{
		OutputStyle: OutputStyleExplanatory,
	})
}

// GetLearningSystemPrompt 获取学习型系统提示词
func GetLearningSystemPrompt() string {
	return BuildSystemPrompt(context.Background(), SystemPromptConfig{
		OutputStyle: OutputStyleLearning,
	})
}
