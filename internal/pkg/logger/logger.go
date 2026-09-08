// Package logger provides a structured logging wrapper around Go's standard
// library "log/slog". It replaces the scattered log.Printf calls throughout
// the codebase with a leveled, structured, module-aware logging API.
//
// Design goals:
//   - Zero external dependencies (log/slog is stdlib since Go 1.21)
//   - Drop-in replacement: log.Printf("[Query] %s", msg) → logger.Query.Info(msg)
//   - Preserve output format during migration (text handler, same [Tag] prefix)
//   - Allow runtime level changes for debugging without restart
//
// Quick start:
//
//	logger.SetLevel(logger.LevelDebug) // enable debug logs
//	logger.Query.Info("message")      // structured: level=INFO module=Query msg=message
//	logger.API.Warn("retry failed")    // level=WARN  module=API  msg=retry failed
package logger

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
)

// formatRegex matches printf-style format verbs like %s, %v, %+v, %q, %d, %x, etc.
var formatRegex = regexp.MustCompile(`%[+\-#0-9]*[a-zA-Z]`)

// hasPrintfVerb checks whether msg contains a printf-style format verb (not counting %% escape).
func hasPrintfVerb(msg string) bool {
	// Strip %% (escaped percent) first
	stripped := strings.ReplaceAll(msg, "%%", "")
	return formatRegex.MatchString(stripped)
}

// formatIfNeeded returns (formattedMsg, remainingArgs). If msg contains printf verbs
// and args provides enough values, it formats via fmt.Sprintf and returns empty args.
// Otherwise it keeps slog's original key-value semantics.
func formatIfNeeded(msg string, args []any) (string, []any) {
	if len(args) == 0 {
		return msg, nil
	}
	if hasPrintfVerb(msg) {
		return fmt.Sprintf(msg, args...), nil
	}
	return msg, args
}

// Level mirrors slog's level constants for convenience.
type Level = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// Global slog logger + level controller
var (
	mu       sync.RWMutex
	level    = slog.LevelDebug
	root     *slog.Logger
	handlers map[string]*ModuleLogger
)

func init() {
	// Default: text handler (preserves old log.Printf feel, adds level)
	// Output example:
	//   2026/09/07 11:26:00 INFO [Query] auto-compact 触发: estimated 25000 >= threshold 22768
	root = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Time format: keep slog default (RFC3339)
			// Level format: keep default (INFO/WARN/ERROR/DEBUG)
			return a
		},
	}))
	slog.SetDefault(root)
	handlers = make(map[string]*ModuleLogger)
}

// SetLevel changes the minimum level for all module loggers.
// Useful for debugging without recompiling.
//
//	logger.SetLevel(logger.LevelDebug) // show everything
//	logger.SetLevel(logger.LevelWarn)  // quiet
func SetLevel(l Level) {
	mu.Lock()
	defer mu.Unlock()
	level = l
	// Rebuild root + all module loggers with new level
	root = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
	slog.SetDefault(root)
	handlers = make(map[string]*ModuleLogger)
}

// GetLevel returns the current minimum level.
func GetLevel() Level {
	mu.RLock()
	defer mu.RUnlock()
	return level
}

// SetJSON switches to JSON output format (good for log aggregators).
func SetJSON() {
	mu.Lock()
	defer mu.Unlock()
	root = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(root)
	handlers = make(map[string]*ModuleLogger)
}

// ModuleLogger wraps a slog.Logger with a fixed [module] prefix.
// Usage:
//
//	var log = logger.NewModule("Query")
//	log.Info("auto-compact triggered", "tokens", 25000, "threshold", 22768)
//	// Output: INFO [Query] auto-compact triggered tokens=25000 threshold=22768
type ModuleLogger struct {
	module string
	log    *slog.Logger
}

// NewModule creates or retrieves a ModuleLogger for the given module name.
// Module names are cached; calling NewModule("Query") multiple times returns
// the same instance.
func NewModule(name string) *ModuleLogger {
	// Normalize: strip brackets, trim spaces
	name = strings.TrimSpace(strings.Trim(name, "[]"))

	mu.RLock()
	if h, ok := handlers[name]; ok {
		mu.RUnlock()
		return h
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()
	if h, ok := handlers[name]; ok {
		return h
	}
	h := &ModuleLogger{
		module: name,
		log:    root.WithGroup("[" + name + "]"),
	}
	handlers[name] = h
	return h
}

// Debug logs at DEBUG level. Supports both printf-style (msg="failed: %v", err)
// and slog structured (msg="failed", "error", err) call conventions.
func (m *ModuleLogger) Debug(msg string, args ...any) {
	msg, args = formatIfNeeded(msg, args)
	m.log.Debug("["+m.module+"] "+msg, args...)
}

// Info logs at INFO level. Supports both printf-style and slog structured conventions.
func (m *ModuleLogger) Info(msg string, args ...any) {
	msg, args = formatIfNeeded(msg, args)
	m.log.Info("["+m.module+"] "+msg, args...)
}

// Warn logs at WARN level. Supports both printf-style and slog structured conventions.
func (m *ModuleLogger) Warn(msg string, args ...any) {
	msg, args = formatIfNeeded(msg, args)
	m.log.Warn("["+m.module+"] "+msg, args...)
}

// Error logs at ERROR level. Supports both printf-style and slog structured conventions.
func (m *ModuleLogger) Error(msg string, args ...any) {
	msg, args = formatIfNeeded(msg, args)
	m.log.Error("["+m.module+"] "+msg, args...)
}
