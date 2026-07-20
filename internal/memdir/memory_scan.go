package memdir

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	MaxMemoryFiles     = 200
	FrontmatterMaxLines = 30
)

type MemoryHeader struct {
	Filename    string      `json:"filename"`
	FilePath    string      `json:"file_path"`
	MtimeMs     int64       `json:"mtime_ms"`
	Description string      `json:"description,omitempty"`
	Type        MemoryType  `json:"type,omitempty"`
}

func ScanMemoryFiles(ctx context.Context, memoryDir string) ([]MemoryHeader, error) {
	var headers []MemoryHeader

	err := filepath.WalkDir(memoryDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		if d.Name() == "MEMORY.md" {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(memoryDir, path)
		if err != nil {
			relPath = d.Name()
		}

		desc, memType := parseFrontmatter(path)

		headers = append(headers, MemoryHeader{
			Filename:    relPath,
			FilePath:    path,
			MtimeMs:     info.ModTime().UnixMilli(),
			Description: desc,
			Type:        memType,
		})
		return nil
	})

	if err != nil && err != context.Canceled {
		return nil, nil
	}

	sort.Slice(headers, func(i, j int) bool {
		return headers[i].MtimeMs > headers[j].MtimeMs
	})

	if len(headers) > MaxMemoryFiles {
		headers = headers[:MaxMemoryFiles]
	}

	return headers, nil
}

func FormatMemoryManifest(memories []MemoryHeader) string {
	var sb strings.Builder
	for _, m := range memories {
		sb.WriteString(fmt.Sprintf("- %s", m.Filename))
		if m.Description != "" {
			sb.WriteString(fmt.Sprintf(": %s", m.Description))
		}
		if m.Type != "" {
			sb.WriteString(fmt.Sprintf(" [%s]", m.Type))
		}
		age := MemoryAge(m.MtimeMs)
		sb.WriteString(fmt.Sprintf(" (%s)", age))
		sb.WriteString("\n")
	}
	return sb.String()
}

func parseFrontmatter(path string) (string, MemoryType) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}

	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return "", ""
	}

	endIdx := strings.Index(content[3:], "---")
	if endIdx < 0 {
		return "", ""
	}

	frontmatter := content[3 : endIdx+3]
	var desc string
	var memType MemoryType

	lines := strings.Split(frontmatter, "\n")
	lineCount := 0
	for _, line := range lines {
		lineCount++
		if lineCount > FrontmatterMaxLines {
			break
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			desc = strings.Trim(desc, "\"'")
		}
		if strings.HasPrefix(line, "type:") {
			typeVal := strings.TrimSpace(strings.TrimPrefix(line, "type:"))
			typeVal = strings.Trim(typeVal, "\"'")
			memType = ParseMemoryType(typeVal)
		}
	}

	return desc, memType
}