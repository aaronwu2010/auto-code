package sessionlogger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/pkg/logger"
)

// SessionLogger 记录每次模型调用的完整请求和响应到文件。
// 文件保存在 ~/.auto/sessions/session-{id}.txt，id 从 1 开始递增。
type SessionLogger struct {
	mu       sync.Mutex
	file     *os.File
	filePath string
	enabled  bool
	turnNum  int
}

var (
	instance     *SessionLogger
	instanceOnce sync.Once
)

// GetInstance 获取单例
func GetInstance() *SessionLogger {
	instanceOnce.Do(func() {
		instance = &SessionLogger{}
	})
	return instance
}

// IsEnabled 当前是否启用
func (s *SessionLogger) IsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled && s.file != nil
}

// Enable 打开日志记录并创建新文件
func (s *SessionLogger) Enable() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.enabled && s.file != nil {
		return nil // 已经开启
	}

	dir, err := sessionsDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建 sessions 目录失败: %w", err)
	}

	id := nextSessionID(dir)
	path := filepath.Join(dir, fmt.Sprintf("session-%d.txt", id))

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("创建 session 日志文件失败: %w", err)
	}

	s.file = f
	s.filePath = path
	s.enabled = true
	s.turnNum = 0

	logger.NewModule("SessionLogger").Info("会话日志已开启: %s", path)

	// 写入文件头
	header := fmt.Sprintf("========== Session Log ==========\nStarted: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	f.WriteString(header)

	return nil
}

// Disable 关闭日志记录并关闭文件
func (s *SessionLogger) Disable() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file != nil {
		s.file.WriteString(fmt.Sprintf("\n========== Session End ==========\nEnded: %s\n", time.Now().Format("2006-01-02 15:04:05")))
		s.file.Close()
		s.file = nil
		logger.NewModule("SessionLogger").Info("会话日志已关闭: %s", s.filePath)
	}
	s.enabled = false
	s.filePath = ""
	s.turnNum = 0
}

// Close 关闭当前文件（用于引擎 Shutdown 时调用）
func (s *SessionLogger) Close() {
	s.Disable()
}

// LogRequest 记录发送给模型的请求
// systemPrompt: 系统提示（完整）
// messages: 用户+助手+工具消息列表
// tools: 工具定义列表
// model: 模型名
func (s *SessionLogger) LogRequest(systemPrompt string, messages interface{}, tools interface{}, model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled || s.file == nil {
		return
	}

	s.turnNum++

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n---------- Turn %d Request ----------\n", s.turnNum))
	sb.WriteString(fmt.Sprintf("Time: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Model: %s\n", model))
	sb.WriteString("\n--- System Prompt ---\n")
	sb.WriteString(systemPrompt)
	sb.WriteString("\n\n--- Messages ---\n")
	writeJSONBlock(&sb, messages)
	sb.WriteString("\n--- Tools ---\n")
	writeJSONBlock(&sb, tools)
	sb.WriteString("\n")

	s.file.WriteString(sb.String())
}

// LogResponse 记录模型的完整响应
// content: 模型输出的 content
// thinking: 模型的 thinking 内容
// toolCalls: 模型返回的 tool_calls
// stopReason: 停止原因
// usage: token 使用量
func (s *SessionLogger) LogResponse(content string, thinking string, toolCalls interface{}, stopReason string, usage interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled || s.file == nil {
		return
	}

	var sb strings.Builder
	sb.WriteString("\n--- Response ---\n")
	sb.WriteString(fmt.Sprintf("Stop Reason: %s\n", stopReason))

	// 用标志位判断是否有任何实质性输出
	hasContent := false

	if thinking != "" {
		sb.WriteString("\n[Thinking]\n")
		sb.WriteString(thinking)
		sb.WriteString("\n")
		hasContent = true
	}

	if content != "" {
		sb.WriteString("\n[Content]\n")
		sb.WriteString(content)
		sb.WriteString("\n")
		hasContent = true
	}

	// 对 slice 类型：检查长度而非 nil（空 slice != nil）
	if toolCalls != nil && !isEmptySlice(toolCalls) {
		sb.WriteString("\n[Tool Calls]\n")
		writeJSONBlock(&sb, toolCalls)
		hasContent = true
	}

	if usage != nil {
		sb.WriteString("\n[Usage]\n")
		writeJSONBlock(&sb, usage)
		hasContent = true
	}

	if !hasContent {
		sb.WriteString("\n(empty response — model returned no content, thinking, or tool_calls)\n")
	}

	sb.WriteString("\n")

	s.file.WriteString(sb.String())
}

func writeJSONBlock(sb *strings.Builder, v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		sb.WriteString(fmt.Sprintf("<marshal error: %v>", err))
		return
	}
	sb.WriteString(string(data))
}

// isEmptySlice 判断 v 是否为空 slice（nil 或 len==0）
func isEmptySlice(v interface{}) bool {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice {
		return rv.Len() == 0
	}
	return false
}

// sessionsDir 返回 sessions 目录路径
func sessionsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".auto", "sessions"), nil
}

// nextSessionID 扫描目录中已有的 session-{id}.txt，返回最大 id+1
func nextSessionID(dir string) int {
	re := regexp.MustCompile(`^session-(\d+)\.txt$`)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1
	}

	var ids []int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := re.FindStringSubmatch(e.Name())
		if m != nil {
			id, err := strconv.Atoi(m[1])
			if err == nil {
				ids = append(ids, id)
			}
		}
	}

	if len(ids) == 0 {
		return 1
	}

	sort.Ints(ids)
	return ids[len(ids)-1] + 1
}
