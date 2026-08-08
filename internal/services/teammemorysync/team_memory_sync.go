package teammemorysync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/memdir"
)

const (
	TeamMemorySyncTimeoutMs = 30000
	MaxFileSizeBytes        = 250000
	MaxPutBodyBytes         = 200000
	MaxRetries              = 3
	MaxConflictRetries      = 2
)

type SyncState struct {
	mu                sync.RWMutex
	LastKnownChecksum string            `json:"last_known_checksum"`
	ServerChecksums   map[string]string `json:"server_checksums"`
	ServerMaxEntries  *int              `json:"server_max_entries,omitempty"`
}

func CreateSyncState() *SyncState {
	return &SyncState{
		ServerChecksums: make(map[string]string),
	}
}

type PullResult struct {
	Success      bool   `json:"success"`
	FilesWritten int    `json:"files_written"`
	EntryCount   int    `json:"entry_count"`
	Error        string `json:"error,omitempty"`
}

type PushResult struct {
	Success     bool   `json:"success"`
	FilesPushed int    `json:"files_pushed"`
	Error       string `json:"error,omitempty"`
}

type SyncResult struct {
	Success     bool `json:"success"`
	FilesPulled int  `json:"files_pulled"`
	FilesPushed int  `json:"files_pushed"`
}

func HashContent(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func BatchDeltaByBytes(delta map[string]string, maxBytes int) []map[string]string {
	if len(delta) == 0 {
		return nil
	}

	keys := make([]string, 0, len(delta))
	for k := range delta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var batches []map[string]string
	current := make(map[string]string)
	currentSize := 0

	for _, k := range keys {
		v := delta[k]
		entrySize := len(k) + len(v) + 4

		if currentSize+entrySize > maxBytes && len(current) > 0 {
			batches = append(batches, current)
			current = make(map[string]string)
			currentSize = 0
		}

		current[k] = v
		currentSize += entrySize
	}

	if len(current) > 0 {
		batches = append(batches, current)
	}

	return batches
}

func IsTeamMemorySyncAvailable() bool {
	return memdir.IsTeamMemoryEnabled() && os.Getenv("CLAUDE_CODE_API_URL") != ""
}

func PullTeamMemory(ctx context.Context, state *SyncState, opts ...PullOption) (*PullResult, error) {
	apiURL := os.Getenv("CLAUDE_CODE_API_URL")
	if apiURL == "" {
		return &PullResult{Error: "API URL not configured"}, nil
	}

	repo := getRepoName()
	if repo == "" {
		return &PullResult{Error: "no git repository found"}, nil
	}

	url := fmt.Sprintf("%s/api/claude_code/team_memory?repo=%s", apiURL, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return &PullResult{Error: err.Error()}, nil
	}

	if state.LastKnownChecksum != "" {
		req.Header.Set("If-None-Match", state.LastKnownChecksum)
	}

	client := &http.Client{Timeout: TeamMemorySyncTimeoutMs * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return &PullResult{Error: err.Error()}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return &PullResult{Success: true, EntryCount: len(state.ServerChecksums)}, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		state.mu.Lock()
		state.ServerChecksums = make(map[string]string)
		state.mu.Unlock()
		return &PullResult{Success: true}, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &PullResult{Error: fmt.Sprintf("server returned %d: %s", resp.StatusCode, string(body))}, nil
	}

	var data struct {
		Entries        map[string]string `json:"entries"`
		EntryChecksums map[string]string `json:"entry_checksums"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return &PullResult{Error: fmt.Sprintf("decode response: %v", err)}, nil
	}

	state.mu.Lock()
	etag := resp.Header.Get("ETag")
	state.LastKnownChecksum = etag
	state.ServerChecksums = data.EntryChecksums
	if state.ServerChecksums == nil {
		state.ServerChecksums = make(map[string]string)
	}
	state.mu.Unlock()

	written := writeRemoteEntriesToLocal(data.Entries)

	return &PullResult{
		Success:      true,
		FilesWritten: written,
		EntryCount:   len(data.Entries),
	}, nil
}

func PushTeamMemory(ctx context.Context, state *SyncState) (*PushResult, error) {
	apiURL := os.Getenv("CLAUDE_CODE_API_URL")
	if apiURL == "" {
		return &PushResult{Error: "API URL not configured"}, nil
	}

	repo := getRepoName()
	if repo == "" {
		return &PushResult{Error: "no git repository found"}, nil
	}

	localFiles, err := readLocalTeamMemory()
	if err != nil {
		return &PushResult{Error: fmt.Sprintf("read local: %v", err)}, nil
	}

	for key, content := range localFiles {
		if secrets := scanForSecrets(content); len(secrets) > 0 {
			delete(localFiles, key)
		}
	}

	state.mu.RLock()
	delta := make(map[string]string)
	for key, content := range localFiles {
		localHash := HashContent(content)
		if serverHash, ok := state.ServerChecksums[key]; !ok || serverHash != localHash {
			delta[key] = content
		}
	}
	state.mu.RUnlock()

	if len(delta) == 0 {
		return &PushResult{Success: true}, nil
	}

	url := fmt.Sprintf("%s/api/claude_code/team_memory?repo=%s", apiURL, repo)
	batches := BatchDeltaByBytes(delta, MaxPutBodyBytes)

	totalPushed := 0
	for _, batch := range batches {
		pushed, err := pushBatch(ctx, url, batch, state)
		if err != nil {
			return &PushResult{Error: err.Error()}, nil
		}
		totalPushed += pushed
	}

	return &PushResult{
		Success:     true,
		FilesPushed: totalPushed,
	}, nil
}

func SyncTeamMemory(ctx context.Context, state *SyncState) (*SyncResult, error) {
	pullResult, err := PullTeamMemory(ctx, state)
	if err != nil {
		return nil, err
	}

	pushResult, err := PushTeamMemory(ctx, state)
	if err != nil {
		return nil, err
	}

	return &SyncResult{
		Success:     pullResult.Success && pushResult.Success,
		FilesPulled: pullResult.FilesWritten,
		FilesPushed: pushResult.FilesPushed,
	}, nil
}

func pushBatch(ctx context.Context, url string, batch map[string]string, state *SyncState) (int, error) {
	body, err := json.Marshal(batch)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	if state.LastKnownChecksum != "" {
		req.Header.Set("If-Match", state.LastKnownChecksum)
	}

	client := &http.Client{Timeout: TeamMemorySyncTimeoutMs * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return len(batch), nil
	}

	if resp.StatusCode == http.StatusPreconditionFailed {
		return 0, fmt.Errorf("conflict: server state changed")
	}

	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		return 0, fmt.Errorf("payload too large")
	}

	respBody, _ := io.ReadAll(resp.Body)
	return 0, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
}

func writeRemoteEntriesToLocal(entries map[string]string) int {
	teamDir := memdir.GetTeamMemPath()
	_ = os.MkdirAll(teamDir, 0o755)

	written := 0
	for key, content := range entries {
		validated, err := memdir.ValidateTeamMemKey(key)
		if err != nil {
			continue
		}

		filePath := filepath.Join(teamDir, validated)
		dir := filepath.Dir(filePath)
		_ = os.MkdirAll(dir, 0o755)

		existing, err := os.ReadFile(filePath)
		if err == nil && string(existing) == content {
			continue
		}

		if err := os.WriteFile(filePath, []byte(content), 0o644); err == nil {
			written++
		}
	}

	return written
}

func readLocalTeamMemory() (map[string]string, error) {
	teamDir := memdir.GetTeamMemPath()
	files := make(map[string]string)

	err := filepath.WalkDir(teamDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		info, err := d.Info()
		if err != nil || info.Size() > MaxFileSizeBytes {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(teamDir, path)
		if err != nil {
			return nil
		}

		files[relPath] = string(data)
		return nil
	})

	return files, err
}

var secretRegexPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`AGPA[0-9A-Z]{16}`),
	regexp.MustCompile(`AIDA[0-9A-Z]{16}`),
	regexp.MustCompile(`ASIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ghp_[0-9a-zA-Z]{36}`),
	regexp.MustCompile(`gho_[0-9a-zA-Z]{36}`),
	regexp.MustCompile(`ghs_[0-9a-zA-Z]{36}`),
	regexp.MustCompile(`ghu_[0-9a-zA-Z]{36}`),
	regexp.MustCompile(`ghr_[0-9a-zA-Z]{76}`),
	regexp.MustCompile(`glpat-[0-9a-zA-Z\-_]{20}`),
	regexp.MustCompile(`AIza[0-9a-zA-Z\-_]{35}`),
	regexp.MustCompile(`xox[baprs]-[0-9a-zA-Z\-]{10,48}`),
	regexp.MustCompile(`sk_live_[0-9a-zA-Z]{24,99}`),
	regexp.MustCompile(`rk_live_[0-9a-zA-Z]{24,99}`),
	regexp.MustCompile(`sk_test_[0-9a-zA-Z]{24,99}`),
	regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----`),
	regexp.MustCompile(`eyJ[0-9a-zA-Z\-_]+\.eyJ[0-9a-zA-Z\-_]+`),
	regexp.MustCompile(`ya29\.[0-9a-zA-Z\-_]+`),
	regexp.MustCompile(`[0-9a-f]{32}`),
	regexp.MustCompile(`[0-9a-f]{40}`),
	regexp.MustCompile(`[0-9a-f]{64}`),
}

var secretKeywords = []string{
	"api_key", "apikey", "secret_key", "secretkey",
	"private_key", "privatekey",
	"password", "passwd",
	"bearer", "authorization:",
	"access_token", "refresh_token",
	"client_secret", "clientsecret",
	"aws_secret", "aws_access",
	"private_token", "pat_",
	"npm_[0-9a-zA-Z]{36}",
	"pypi-",
	"x-api-key:",
	"vault_token",
}

func scanForSecrets(content string) []string {
	var found []string

	for _, re := range secretRegexPatterns {
		if re.MatchString(content) {
			found = append(found, re.String())
		}
	}

	lower := strings.ToLower(content)
	for _, pattern := range secretKeywords {
		if strings.Contains(lower, pattern) {
			found = append(found, pattern)
		}
	}

	return found
}

func ScanForSecretsPublic(content string) []string {
	return scanForSecrets(content)
}

func getRepoName() string {
	dir, _ := os.Getwd()
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return ""
	}

	configPath := filepath.Join(gitDir, "config")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "url = ") {
			url := strings.TrimPrefix(line, "url = ")
			url = strings.TrimSuffix(url, ".git")
			if idx := strings.LastIndex(url, ":"); idx >= 0 {
				url = url[idx+1:]
			}
			if idx := strings.LastIndex(url, "/"); idx >= 0 {
				parts := strings.Split(url, "/")
				if len(parts) >= 2 {
					return parts[len(parts)-2] + "/" + parts[len(parts)-1]
				}
			}
			return url
		}
	}

	return ""
}

type PullOption func(*pullConfig)

type pullConfig struct {
	skipETagCache bool
}

func WithSkipETagCache() PullOption {
	return func(c *pullConfig) { c.skipETagCache = true }
}
