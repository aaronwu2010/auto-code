package perception

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PerceptionManagerImpl 感知层管理器实现
type PerceptionManagerImpl struct {
	config           *PerceptionConfig
	processors       map[string]InputProcessor
	filters          map[string]SignalFilter
	injectors        map[string]ContextInjector
	multimodalHandlers map[string]MultimodalHandler

	defaultProcessor string
	defaultFilter    string
	defaultInjector  string

	mu       sync.RWMutex

	// 统计信息
	metrics *Metrics
}

// NewPerceptionManager 创建感知层管理器
func NewPerceptionManager(config *PerceptionConfig) *PerceptionManagerImpl {
	if config == nil {
		config = DefaultPerceptionConfig()
	}

	return &PerceptionManagerImpl{
		config:             config,
		processors:         make(map[string]InputProcessor),
		filters:            make(map[string]SignalFilter),
		injectors:          make(map[string]ContextInjector),
		multimodalHandlers: make(map[string]MultimodalHandler),
		metrics:            &Metrics{},
	}
}

// Process 处理输入
func (m *PerceptionManagerImpl) Process(ctx context.Context, input *InputData) (*OutputData, error) {
	start := time.Now()
	defer func() {
		m.mu.Lock()
		m.metrics.TotalInputs++
		m.metrics.AverageLatency = (m.metrics.AverageLatency + time.Since(start)) / 2
		if time.Since(start) > m.metrics.MaxLatency {
			m.metrics.MaxLatency = time.Since(start)
		}
		m.mu.Unlock()
	}()

	// 1. 应用过滤器（如果启用）
	if m.config.EnableFiltering {
		output, filtered, err := m.applyFilters(ctx, input)
		if err != nil {
			m.mu.Lock()
			m.metrics.FailedInputs++
			m.mu.Unlock()
			return nil, err
		}
		if filtered {
			m.mu.Lock()
			m.metrics.FilteredInputs++
			m.mu.Unlock()
			return output, nil
		}
	}

	// 2. 注入上下文（如果启用）
	if m.config.EnableInjection {
		context, err := m.buildContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to build context: %w", err)
		}

		input, err = m.applyInjectors(ctx, input, context)
		if err != nil {
			return nil, fmt.Errorf("failed to inject context: %w", err)
		}
	}

	// 3. 选择处理器并处理
	output, err := m.processInput(ctx, input)
	if err != nil {
		m.mu.Lock()
		m.metrics.FailedInputs++
		m.mu.Unlock()
		return nil, err
	}

	m.mu.Lock()
	m.metrics.ProcessedInputs++
	m.mu.Unlock()

	return output, nil
}

// RegisterProcessor 注册输入处理器
func (m *PerceptionManagerImpl) RegisterProcessor(processor InputProcessor) error {
	if processor == nil {
		return fmt.Errorf("processor is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.processors[processor.Name()] = processor

	// 设置为默认处理器（如果还没有）
	if m.defaultProcessor == "" {
		m.defaultProcessor = processor.Name()
	}

	return nil
}

// UnregisterProcessor 注销输入处理器
func (m *PerceptionManagerImpl) UnregisterProcessor(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.processors[name]; !exists {
		return fmt.Errorf("processor %s not found", name)
	}

	delete(m.processors, name)

	// 如果删除的是默认处理器，选择另一个
	if m.defaultProcessor == name {
		m.defaultProcessor = ""
		for n := range m.processors {
			m.defaultProcessor = n
			break
		}
	}

	return nil
}

// RegisterFilter 注册过滤器
func (m *PerceptionManagerImpl) RegisterFilter(filter SignalFilter) error {
	if filter == nil {
		return fmt.Errorf("filter is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	name := "default_filter"
	m.filters[name] = filter

	if m.defaultFilter == "" {
		m.defaultFilter = name
	}

	return nil
}

// UnregisterFilter 注销过滤器
func (m *PerceptionManagerImpl) UnregisterFilter(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filters[name]; !exists {
		return fmt.Errorf("filter %s not found", name)
	}

	delete(m.filters, name)

	if m.defaultFilter == name {
		m.defaultFilter = ""
		for n := range m.filters {
			m.defaultFilter = n
			break
		}
	}

	return nil
}

// RegisterInjector 注册上下文注入器
func (m *PerceptionManagerImpl) RegisterInjector(injector ContextInjector) error {
	if injector == nil {
		return fmt.Errorf("injector is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.injectors[injector.Name()] = injector

	if m.defaultInjector == "" {
		m.defaultInjector = injector.Name()
	}

	return nil
}

// UnregisterInjector 注销上下文注入器
func (m *PerceptionManagerImpl) UnregisterInjector(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.injectors[name]; !exists {
		return fmt.Errorf("injector %s not found", name)
	}

	delete(m.injectors, name)

	if m.defaultInjector == name {
		m.defaultInjector = ""
		for n := range m.injectors {
			m.defaultInjector = n
			break
		}
	}

	return nil
}

// RegisterMultimodalHandler 注册多模态处理器
func (m *PerceptionManagerImpl) RegisterMultimodalHandler(handler MultimodalHandler) error {
	if handler == nil {
		return fmt.Errorf("handler is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.multimodalHandlers[handler.Name()] = handler
	return nil
}

// UnregisterMultimodalHandler 注销多模态处理器
func (m *PerceptionManagerImpl) UnregisterMultimodalHandler(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.multimodalHandlers[name]; !exists {
		return fmt.Errorf("multimodal handler %s not found", name)
	}

	delete(m.multimodalHandlers, name)
	return nil
}

// GetMetrics 获取性能指标
func (m *PerceptionManagerImpl) GetMetrics(ctx context.Context) (*Metrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := *m.metrics
	return &metrics, nil
}

// Reset 重置所有组件
func (m *PerceptionManagerImpl) Reset(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 重置统计信息
	m.metrics = &Metrics{}

	// 重置所有处理器
	for _, p := range m.processors {
		if resettable, ok := p.(interface{ Reset() }); ok {
			resettable.Reset()
		}
	}

	// 重置所有过滤器
	for _, f := range m.filters {
		if resettable, ok := f.(interface{ Reset() }); ok {
			resettable.Reset()
		}
	}

	return nil
}

// Shutdown 关闭管理器
func (m *PerceptionManagerImpl) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 清空所有组件
	m.processors = make(map[string]InputProcessor)
	m.filters = make(map[string]SignalFilter)
	m.injectors = make(map[string]ContextInjector)
	m.multimodalHandlers = make(map[string]MultimodalHandler)

	// 重置统计信息
	m.metrics = &Metrics{}

	return nil
}

// Helper methods

func (m *PerceptionManagerImpl) applyFilters(ctx context.Context, input *InputData) (*OutputData, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, filter := range m.filters {
		output, filtered, err := filter.Filter(ctx, input)
		if err != nil {
			return nil, false, err
		}
		if filtered {
			return output, true, nil
		}
	}

	return nil, false, nil
}

func (m *PerceptionManagerImpl) buildContext(ctx context.Context) (*Context, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.injectors) == 0 {
		return NewContext(), nil
	}

	// 使用默认注入器构建上下文
	if m.defaultInjector != "" {
		if injector, exists := m.injectors[m.defaultInjector]; exists {
			return injector.BuildContext(ctx)
		}
	}

	// 使用第一个可用的注入器
	for _, injector := range m.injectors {
		return injector.BuildContext(ctx)
	}

	return NewContext(), nil
}

func (m *PerceptionManagerImpl) applyInjectors(ctx context.Context, input *InputData, context *Context) (*InputData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, injector := range m.injectors {
		injected, err := injector.Inject(ctx, input, context)
		if err != nil {
			return nil, err
		}
		input = injected
	}

	return input, nil
}

func (m *PerceptionManagerImpl) processInput(ctx context.Context, input *InputData) (*OutputData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 选择合适的处理器
	processor := m.selectProcessor(input)
	if processor == nil {
		return nil, fmt.Errorf("no suitable processor found for input type %s", input.Type)
	}

	return processor.Process(ctx, input)
}

func (m *PerceptionManagerImpl) selectProcessor(input *InputData) InputProcessor {
	// 首先尝试找到能处理该输入类型的处理器
	for _, processor := range m.processors {
		if processor.CanProcess(input) {
			return processor
		}
	}

	// 如果没有找到，使用默认处理器
	if m.defaultProcessor != "" {
		if processor, exists := m.processors[m.defaultProcessor]; exists {
			return processor
		}
	}

	return nil
}