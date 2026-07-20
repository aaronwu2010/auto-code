package voice

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const maxKeyterms = 50

var globalKeyterms = []string{
	"MCP",
	"symlink",
	"grep",
	"regex",
	"localhost",
	"codebase",
	"TypeScript",
	"JSON",
	"OAuth",
	"webhook",
	"gRPC",
	"dotfiles",
	"subagent",
	"worktree",
}

func SplitIdentifier(name string) []string {
	re := regexp.MustCompile(`([a-z])([A-Z])`)
	name = re.ReplaceAllString(name, "$1 $2")
	parts := strings.Split(name, "[-_./ \\t]+")

	reSplit := regexp.MustCompile(`[-_./\s]+`)
	parts = reSplit.Split(name, -1)

	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) > 2 && len(p) <= 20 {
			result = append(result, p)
		}
	}
	return result
}

func fileNameWords(filePath string) []string {
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return SplitIdentifier(stem)
}

func GetVoiceKeyterms(projectRoot string, recentFiles []string) []string {
	terms := make(map[string]bool)
	for _, t := range globalKeyterms {
		terms[t] = true
	}

	if projectRoot != "" {
		name := filepath.Base(projectRoot)
		if len(name) > 2 && len(name) <= 50 {
			terms[name] = true
		}
	}

	branch := getGitBranch()
	if branch != "" {
		for _, word := range SplitIdentifier(branch) {
			terms[word] = true
		}
	}

	for _, filePath := range recentFiles {
		if len(terms) >= maxKeyterms {
			break
		}
		for _, word := range fileNameWords(filePath) {
			terms[word] = true
		}
	}

	result := make([]string, 0, len(terms))
	for t := range terms {
		result = append(result, t)
	}

	if len(result) > maxKeyterms {
		result = result[:maxKeyterms]
	}

	return result
}

func getGitBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func GetProjectRoot() string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		cwd, _ := os.Getwd()
		return cwd
	}
	return strings.TrimSpace(string(output))
}