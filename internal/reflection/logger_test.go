package reflection

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestGetLogger(t *testing.T) {
	logger1 := GetLogger()
	logger2 := GetLogger()

	if logger1 != logger2 {
		t.Error("GetLogger should return the same instance")
	}
}

func TestNewLogger(t *testing.T) {
	logger := NewLogger(LogLevelDebug, true)

	if logger == nil {
		t.Fatal("Logger should not be nil")
	}

	if logger.level != LogLevelDebug {
		t.Errorf("Logger level = %v, want %v", logger.level, LogLevelDebug)
	}

	if !logger.enabled {
		t.Error("Logger should be enabled")
	}
}

func TestLogger_Levels(t *testing.T) {
	// 创建一个buffer来捕获日志输出
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelDebug,
		enabled:   true,
		totalLogs: make(map[LogLevel]int64),
	}

	tests := []struct {
		name     string
		logFunc  func()
		expected string
	}{
		{
			name: "Debug",
			logFunc: func() {
				logger.Debug("test debug message")
			},
			expected: "[DEBUG]",
		},
		{
			name: "Info",
			logFunc: func() {
				logger.Info("test info message")
			},
			expected: "[INFO]",
		},
		{
			name: "Warn",
			logFunc: func() {
				logger.Warn("test warn message")
			},
			expected: "[WARN]",
		},
		{
			name: "Error",
			logFunc: func() {
				logger.Error("test error message")
			},
			expected: "[ERROR]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc()
			output := buf.String()
			if !strings.Contains(output, tt.expected) {
				t.Errorf("Expected output to contain %s, got %s", tt.expected, output)
			}
		})
	}
}

func TestLogger_LevelFiltering(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelInfo,
		enabled:   true,
		totalLogs: make(map[LogLevel]int64),
	}

	// Debug级别应该被过滤
	buf.Reset()
	logger.Debug("debug message")
	if buf.Len() > 0 {
		t.Error("Debug message should be filtered when level is Info")
	}

	// Info级别应该被记录
	buf.Reset()
	logger.Info("info message")
	if buf.Len() == 0 {
		t.Error("Info message should be logged")
	}
}

func TestLogger_Disabled(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelDebug,
		enabled:   false,
		totalLogs: make(map[LogLevel]int64),
	}

	logger.Info("test message")
	if buf.Len() > 0 {
		t.Error("No logs should be output when logger is disabled")
	}
}

func TestLogger_SetLevel(t *testing.T) {
	logger := NewLogger(LogLevelInfo, true)

	logger.SetLevel(LogLevelError)
	if logger.level != LogLevelError {
		t.Errorf("Logger level = %v, want %v", logger.level, LogLevelError)
	}
}

func TestLogger_SetEnabled(t *testing.T) {
	logger := NewLogger(LogLevelInfo, true)

	logger.SetEnabled(false)
	if logger.enabled {
		t.Error("Logger should be disabled")
	}
}

func TestLogger_GetStats(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelDebug,
		enabled:   true,
		totalLogs: make(map[LogLevel]int64),
	}

	logger.Info("message 1")
	logger.Info("message 2")
	logger.Error("error message")

	stats := logger.GetStats()

	if stats["total_logs"].(map[LogLevel]int64)[LogLevelInfo] != 2 {
		t.Error("Info log count should be 2")
	}

	if stats["total_logs"].(map[LogLevel]int64)[LogLevelError] != 1 {
		t.Error("Error log count should be 1")
	}
}

func TestLogger_LogEvaluationStart(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelDebug,
		enabled:   true,
		totalLogs: make(map[LogLevel]int64),
	}

	logger.LogEvaluationStart("task-1", "Test goal")

	output := buf.String()
	if !strings.Contains(output, "Evaluation Started") {
		t.Error("Should contain 'Evaluation Started'")
	}

	if !strings.Contains(output, "task-1") {
		t.Error("Should contain task ID")
	}
}

func TestLogger_LogEvaluationEnd(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelDebug,
		enabled:   true,
		totalLogs: make(map[LogLevel]int64),
	}

	logger.LogEvaluationEnd("task-1", EvaluationStatusSuccess, 0.95, 0)

	output := buf.String()
	if !strings.Contains(output, "Evaluation Completed") {
		t.Error("Should contain 'Evaluation Completed'")
	}

	if !strings.Contains(output, "success") {
		t.Error("Should contain status")
	}

	if !strings.Contains(output, "0.95") {
		t.Error("Should contain score")
	}
}

func TestLogger_LogErrorAnalysisStart(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelDebug,
		enabled:   true,
		totalLogs: make(map[LogLevel]int64),
	}

	logger.LogErrorAnalysisStart("err-1", "Test error")

	output := buf.String()
	if !strings.Contains(output, "Error Analysis Started") {
		t.Error("Should contain 'Error Analysis Started'")
	}
}

func TestLogger_LogErrorAnalysisEnd(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelDebug,
		enabled:   true,
		totalLogs: make(map[LogLevel]int64),
	}

	logger.LogErrorAnalysisEnd("err-1", ErrorCategoryTimeout, ErrorSeverityHigh, 0.85)

	output := buf.String()
	if !strings.Contains(output, "Error Analysis Completed") {
		t.Error("Should contain 'Error Analysis Completed'")
	}
}

func TestLogger_LogExperienceSave(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelDebug,
		enabled:   true,
		totalLogs: make(map[LogLevel]int64),
	}

	logger.LogExperienceSave("exp-1", ExperienceTypeSuccess, 0.9)

	output := buf.String()
	if !strings.Contains(output, "Experience Saved") {
		t.Error("Should contain 'Experience Saved'")
	}
}

func TestLogger_LogExperienceSearch(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelDebug,
		enabled:   true,
		totalLogs: make(map[LogLevel]int64),
	}

	query := &ExperienceQuery{
		Type:  ExperienceTypeSuccess,
		Limit: 5,
	}

	logger.LogExperienceSearch(query, 3, 0)

	output := buf.String()
	if !strings.Contains(output, "Experience Search") {
		t.Error("Should contain 'Experience Search'")
	}

	if !strings.Contains(output, "Results Found: 3") {
		t.Error("Should contain result count")
	}
}

func TestLogger_Reset(t *testing.T) {
	logger := NewLogger(LogLevelInfo, true)

	logger.Info("message 1")
	logger.Info("message 2")

	stats := logger.GetStats()
	if stats["total_logs"].(map[LogLevel]int64)[LogLevelInfo] != 2 {
		t.Error("Info log count should be 2")
	}

	logger.Reset()

	stats = logger.GetStats()
	if stats["total_logs"].(map[LogLevel]int64)[LogLevelInfo] != 0 {
		t.Error("Info log count should be 0 after reset")
	}
}

func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{LogLevelDebug, "DEBUG"},
		{LogLevelInfo, "INFO"},
		{LogLevelWarn, "WARN"},
		{LogLevelError, "ERROR"},
		{LogLevel(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("LogLevel.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func BenchmarkLogger_Info(b *testing.B) {
	buf := &bytes.Buffer{}
	logger := &Logger{
		logger:    log.New(buf, "[Test] ", 0),
		level:     LogLevelInfo,
		enabled:   true,
		totalLogs: make(map[LogLevel]int64),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message %d", i)
	}
}
