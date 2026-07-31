package perception

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// BaseInputProcessor 基础输入处理器
// 提供通用的文本输入处理功能
type BaseInputProcessor struct {
	name          string
	supportedTypes []InputType
	config        *PerceptionConfig
	mu            sync.RWMutex

	// 统计信息
	totalProcessed int64
	totalErrors    int64
	totalLatency   time.Duration
}

// NewBaseInputProcessor 创建基础输入处理器
func NewBaseInputProcessor(config *PerceptionConfig) *BaseInputProcessor {
	if config == nil {
		config = DefaultPerceptionConfig()
	}

	return &BaseInputProcessor{
		name: "base_processor",
		supportedTypes: []InputType{
			InputTypeText,
			InputTypeStructured,
			InputTypeEnvironment,
			InputTypeEvent,
		},
		config: config,
	}
}

// Process 处理输入
func (p *BaseInputProcessor) Process(ctx context.Context, input *InputData) (*OutputData, error) {
	start := time.Now()
	defer func() {
		p.mu.Lock()
		p.totalProcessed++
		p.totalLatency += time.Since(start)
		p.mu.Unlock()
	}()

	// 验证输入
	if err := p.validateInput(input); err != nil {
		p.mu.Lock()
		p.totalErrors++
		p.mu.Unlock()
		return nil, fmt.Errorf("input validation failed: %w", err)
	}

	// 处理输入内容
	processedContent, features, err := p.processContent(input)
	if err != nil {
		p.mu.Lock()
		p.totalErrors++
		p.mu.Unlock()
		return nil, fmt.Errorf("content processing failed: %w", err)
	}

	// 构建输出
	output := &OutputData{
		ProcessedContent: processedContent,
		Features:         features,
		ProcessingTime:   time.Since(start),
		Confidence:       p.calculateConfidence(input, processedContent),
		Filtered:         false,
	}

	return output, nil
}

// CanProcess 判断是否能处理该输入
func (p *BaseInputProcessor) CanProcess(input *InputData) bool {
	if input == nil {
		return false
	}

	for _, t := range p.supportedTypes {
		if input.Type == t {
			return true
		}
	}

	return false
}

// Name 返回处理器名称
func (p *BaseInputProcessor) Name() string {
	return p.name
}

// SupportedTypes 返回支持的输入类型
func (p *BaseInputProcessor) SupportedTypes() []InputType {
	return p.supportedTypes
}

// validateInput 验证输入数据
func (p *BaseInputProcessor) validateInput(input *InputData) error {
	if input == nil {
		return fmt.Errorf("input is nil")
	}

	if input.ID == "" {
		return fmt.Errorf("input ID is required")
	}

	if input.Type == "" {
		return fmt.Errorf("input type is required")
	}

	if input.Content == "" && len(input.RawData) == 0 {
		return fmt.Errorf("input content or raw data is required")
	}

	return nil
}

// processContent 处理内容
func (p *BaseInputProcessor) processContent(input *InputData) (string, map[string]interface{}, error) {
	features := make(map[string]interface{})

	// 文本处理
	if input.Type == InputTypeText {
		content := p.preprocessText(input.Content)
		features["word_count"] = len(strings.Fields(content))
		features["char_count"] = len(content)
		features["line_count"] = len(strings.Split(content, "\n"))
		return content, features, nil
	}

	// 结构化数据处理
	if input.Type == InputTypeStructured {
		content := p.processStructured(input)
		features["data_type"] = "structured"
		return content, features, nil
	}

	// 环境反馈处理
	if input.Type == InputTypeEnvironment {
		content := p.processEnvironment(input)
		features["data_type"] = "environment"
		return content, features, nil
	}

	// 事件触发处理
	if input.Type == InputTypeEvent {
		content := p.processEvent(input)
		features["data_type"] = "event"
		return content, features, nil
	}

	return input.Content, features, nil
}

// preprocessText 预处理文本
func (p *BaseInputProcessor) preprocessText(content string) string {
	// 去除多余空白
	content = strings.TrimSpace(content)

	// 标准化换行符
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	// 去除连续空行
	lines := strings.Split(content, "\n")
	var result []string
	prevEmpty := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if !prevEmpty {
				result = append(result, "")
				prevEmpty = true
			}
		} else {
			result = append(result, line)
			prevEmpty = false
		}
	}

	return strings.Join(result, "\n")
}

// processStructured 处理结构化数据
func (p *BaseInputProcessor) processStructured(input *InputData) string {
	// 简单的JSON格式化
	if input.Metadata != nil {
		return fmt.Sprintf("Structured data: %v", input.Metadata)
	}
	return input.Content
}

// processEnvironment 处理环境反馈
func (p *BaseInputProcessor) processEnvironment(input *InputData) string {
	return fmt.Sprintf("Environment feedback: %s", input.Content)
}

// processEvent 处理事件触发
func (p *BaseInputProcessor) processEvent(input *InputData) string {
	return fmt.Sprintf("Event: %s", input.Content)
}

// calculateConfidence 计算置信度
func (p *BaseInputProcessor) calculateConfidence(input *InputData, processed string) float64 {
	// 基础置信度计算
	confidence := 0.8

	// 如果有元数据，增加置信度
	if input.Metadata != nil && len(input.Metadata) > 0 {
		confidence += 0.1
	}

	// 如果内容较长，增加置信度
	if len(processed) > 50 {
		confidence += 0.05
	}

	// 限制在0-1之间
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// GetStats 获取统计信息
func (p *BaseInputProcessor) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var avgLatency time.Duration
	if p.totalProcessed > 0 {
		avgLatency = p.totalLatency / time.Duration(p.totalProcessed)
	}

	return map[string]interface{}{
		"total_processed": p.totalProcessed,
		"total_errors":    p.totalErrors,
		"average_latency": avgLatency,
	}
}

// Reset 重置统计信息
func (p *BaseInputProcessor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalProcessed = 0
	p.totalErrors = 0
	p.totalLatency = 0
}