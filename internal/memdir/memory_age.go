package memdir

import (
	"fmt"
	"math"
	"time"
)

func MemoryAgeDays(mtimeMs int64) int {
	elapsed := time.Since(time.UnixMilli(mtimeMs))
	days := int(elapsed.Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

func MemoryAge(mtimeMs int64) string {
	days := MemoryAgeDays(mtimeMs)
	switch days {
	case 0:
		return "today"
	case 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

func MemoryFreshnessText(mtimeMs int64) string {
	days := MemoryAgeDays(mtimeMs)
	if days <= 1 {
		return ""
	}
	return fmt.Sprintf("(memory is %d days old — verify with current project state)", days)
}

func MemoryFreshnessNote(mtimeMs int64) string {
	text := MemoryFreshnessText(mtimeMs)
	if text == "" {
		return ""
	}
	return fmt.Sprintf("<system-reminder>%s</system-reminder>", text)
}

func MemoryFreshnessNoteCapped(mtimeMs int64, maxDays int) string {
	days := MemoryAgeDays(mtimeMs)
	if days <= maxDays {
		return ""
	}
	return MemoryFreshnessNote(mtimeMs)
}

func init() {
	_ = math.Ceil
}