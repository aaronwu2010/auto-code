package reflection

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// LogLevel 日志级别
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// String 返回日志级别的字符串表示
func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger 反思机制日志记录器
type Logger struct {
	logger  *log.Logger
	level   LogLevel
	enabled bool
	mu      sync.Mutex

	// 统计信息
	totalLogs map[LogLevel]int64
	startTime time.Time
}

var (
	defaultLogger *Logger
	once          sync.Once
)

// GetLogger 获取默认日志记录器
func GetLogger() *Logger {
	once.Do(func() {
		defaultLogger = &Logger{
			logger:    log.New(os.Stdout, "[Reflection] ", log.LstdFlags|log.Lshortfile),
			level:     LogLevelInfo,
			enabled:   true,
			totalLogs: make(map[LogLevel]int64),
			startTime: time.Now(),
		}
	})
	return defaultLogger
}

// NewLogger 创建新的日志记录器
func NewLogger(level LogLevel, enabled bool) *Logger {
	return &Logger{
		logger:    log.New(os.Stdout, "[Reflection] ", log.LstdFlags|log.Lshortfile),
		level:     level,
		enabled:   enabled,
		totalLogs: make(map[LogLevel]int64),
		startTime: time.Now(),
	}
}

// SetLevel 设置日志级别
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetEnabled 设置是否启用
func (l *Logger) SetEnabled(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = enabled
}

// log 记录日志
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if !l.enabled || level < l.level {
		return
	}

	l.mu.Lock()
	l.totalLogs[level]++
	l.mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	prefix := fmt.Sprintf("[%s] ", level.String())
	l.logger.Println(prefix + msg)
}

// Debug 调试日志
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LogLevelDebug, format, args...)
}

// Info 信息日志
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LogLevelInfo, format, args...)
}

// Warn 警告日志
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LogLevelWarn, format, args...)
}

// Error 错误日志
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LogLevelError, format, args...)
}

// LogEvaluationStart 记录评估开始
func (l *Logger) LogEvaluationStart(taskID, goal string) {
	l.Info(" ========== Evaluation Started ==========")
	l.Info(" Task ID: %s", taskID)
	l.Info(" Goal: %s", goal)
	l.Debug(" Start Time: %s", time.Now().Format(time.RFC3339))
}

// LogEvaluationEnd 记录评估结束
func (l *Logger) LogEvaluationEnd(taskID string, status EvaluationStatus, score float64, duration time.Duration) {
	l.Info(" ========== Evaluation Completed ==========")
	l.Info(" Task ID: %s", taskID)
	l.Info(" Status: %s", status)
	l.Info(" Score: %.2f", score)
	l.Info(" Duration: %v", duration)
	l.Debug(" End Time: %s", time.Now().Format(time.RFC3339))
}

// LogEvaluationCriteria 记录评估标准
func (l *Logger) LogEvaluationCriteria(criteria map[string]float64) {
	l.Debug(" Evaluation Criteria:")
	for name, score := range criteria {
		l.Debug("   - %s: %.2f", name, score)
	}
}

// LogEvaluationStrengths 记录评估优点
func (l *Logger) LogEvaluationStrengths(strengths []string) {
	if len(strengths) > 0 {
		l.Debug(" Strengths:")
		for _, s := range strengths {
			l.Debug("   + %s", s)
		}
	}
}

// LogEvaluationWeaknesses 记录评估缺点
func (l *Logger) LogEvaluationWeaknesses(weaknesses []string) {
	if len(weaknesses) > 0 {
		l.Debug(" Weaknesses:")
		for _, w := range weaknesses {
			l.Debug("   - %s", w)
		}
	}
}

// LogErrorAnalysisStart 记录错误分析开始
func (l *Logger) LogErrorAnalysisStart(errorID, message string) {
	l.Info(" ========== Error Analysis Started ==========")
	l.Info(" Error ID: %s", errorID)
	l.Info(" Message: %s", message)
	l.Debug(" Start Time: %s", time.Now().Format(time.RFC3339))
}

// LogErrorAnalysisEnd 记录错误分析结束
func (l *Logger) LogErrorAnalysisEnd(errorID string, category ErrorCategory, severity ErrorSeverity, confidence float64) {
	l.Info(" ========== Error Analysis Completed ==========")
	l.Info(" Error ID: %s", errorID)
	l.Info(" Category: %s", category)
	l.Info(" Severity: %s", severity)
	l.Info(" Confidence: %.2f", confidence)
}

// LogErrorRootCause 记录错误根因
func (l *Logger) LogErrorRootCause(rootCause string, causalChain []string) {
	l.Debug(" Root Cause: %s", rootCause)
	if len(causalChain) > 0 {
		l.Debug(" Causal Chain:")
		for i, cause := range causalChain {
			l.Debug("   %d. %s", i+1, cause)
		}
	}
}

// LogErrorSolutions 记录错误解决方案
func (l *Logger) LogErrorSolutions(immediate []string, longTerm []string, prevention []string) {
	if len(immediate) > 0 {
		l.Debug(" Immediate Actions:")
		for _, action := range immediate {
			l.Debug("   - %s", action)
		}
	}

	if len(longTerm) > 0 {
		l.Debug(" Long-term Solutions:")
		for _, solution := range longTerm {
			l.Debug("   - %s", solution)
		}
	}

	if len(prevention) > 0 {
		l.Debug(" Prevention Measures:")
		for _, measure := range prevention {
			l.Debug("   - %s", measure)
		}
	}
}

// LogExperienceSave 记录经验保存
func (l *Logger) LogExperienceSave(expID string, expType ExperienceType, effectiveness float64) {
	l.Info(" ========== Experience Saved ==========")
	l.Info(" Experience ID: %s", expID)
	l.Info(" Type: %s", expType)
	l.Info(" Effectiveness: %.2f", effectiveness)
}

// LogExperienceLoad 记录经验加载
func (l *Logger) LogExperienceLoad(expID string, fromCache bool) {
	l.Debug(" Experience Loaded: %s", expID)
	if fromCache {
		l.Debug(" (from cache)")
	} else {
		l.Debug(" (from file)")
	}
}

// LogExperienceSearch 记录经验搜索
func (l *Logger) LogExperienceSearch(query *ExperienceQuery, resultCount int, duration time.Duration) {
	l.Info(" ========== Experience Search ==========")
	l.Debug(" Query Type: %s", query.Type)
	l.Debug(" Keywords: %v", query.Keywords)
	l.Debug(" Min Effectiveness: %.2f", query.MinEffectiveness)
	l.Info(" Results Found: %d", resultCount)
	l.Info(" Search Duration: %v", duration)
}

// LogExperienceDelete 记录经验删除
func (l *Logger) LogExperienceDelete(expID string) {
	l.Info(" Experience Deleted: %s", expID)
}

// LogExperienceExport 记录经验导出
func (l *Logger) LogExperienceExport(count int, outputPath string) {
	l.Info(" ========== Experience Export ==========")
	l.Info(" Exported Count: %d", count)
	l.Info(" Output Path: %s", outputPath)
}

// LogExperienceImport 记录经验导入
func (l *Logger) LogExperienceImport(count int, inputPath string, success int, failed int) {
	l.Info(" ========== Experience Import ==========")
	l.Info(" Total Count: %d", count)
	l.Info(" Input Path: %s", inputPath)
	l.Info(" Success: %d, Failed: %d", success, failed)
}

// LogStoreStats 记录存储统计
func (l *Logger) LogStoreStats(stats map[string]interface{}) {
	l.Info(" ========== Store Statistics ==========")
	for key, value := range stats {
		l.Info(" %s: %v", key, value)
	}
}

// LogOperationStart 记录操作开始
func (l *Logger) LogOperationStart(operation string) {
	l.Debug(" >>> Operation Started: %s", operation)
}

// LogOperationEnd 记录操作结束
func (l *Logger) LogOperationEnd(operation string, duration time.Duration, err error) {
	if err != nil {
		l.Error(" <<< Operation Failed: %s (duration: %v, error: %v)", operation, duration, err)
	} else {
		l.Debug(" <<< Operation Completed: %s (duration: %v)", operation, duration)
	}
}

// GetStats 获取日志统计
func (l *Logger) GetStats() map[string]interface{} {
	l.mu.Lock()
	defer l.mu.Unlock()

	return map[string]interface{}{
		"total_logs": l.totalLogs,
		"level":      l.level.String(),
		"enabled":    l.enabled,
		"uptime":     time.Since(l.startTime),
	}
}

// Reset 重置统计
func (l *Logger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.totalLogs = make(map[LogLevel]int64)
	l.startTime = time.Now()
}
