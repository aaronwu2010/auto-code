package query

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// LandscaperConfig Landscaper 配置
type LandscaperConfig struct {
	Enabled         bool          // 总开关
	Timeout         time.Duration // 扫描总超时
	MaxFiles        int           // 最多扫描文件数
	MaxFileSize     int64         // 单文件最大读取大小（字节）
	MaxKeywords     int           // 关键词数量上限
	GrepMaxMatches  int           // grep 最大匹配数
	MaxReadFiles    int           // 自动读取关键文件数上限
	MaxFileChars    int           // 单文件最多读多少字符
}

// DefaultLandscaperConfig 默认配置
func DefaultLandscaperConfig() LandscaperConfig {
	return LandscaperConfig{
		Enabled:        true,
		Timeout:        2 * time.Second,
		MaxFiles:       2000,
		MaxFileSize:    200 * 1024, // 200KB
		MaxKeywords:    30,
		GrepMaxMatches: 20,
		MaxReadFiles:   8,
		MaxFileChars:   2000,
	}
}

// Landscaper 前置环境扫描器（方案 P0）
// 设计要点：
//   - 不调用任何 LLM，零 API 开销
//   - 全部并发执行，硬超时 2s，超时直接返回空串
//   - 所有错误内部吞掉，失败即跳过
//   - 输出注入为 meta message，不影响正常对话流
type Landscaper struct {
	cfg LandscaperConfig
}

// NewLandscaper 创建 Landscaper
func NewLandscaper(cfg LandscaperConfig) *Landscaper {
	return &Landscaper{cfg: cfg}
}

// dirNode 项目树节点
type dirNode struct {
	name    string
	subdirs map[string]*dirNode
	files   []string
}

// LandscapingResult 扫描结果（中间结构）
type LandscapingResult struct {
	ProjectDir   string
	FilesScanned int
	Structure    string
	TotalSize    int64
	Keywords     []string
	GrepHits     []GrepHit
	KeyFiles     []KeyFileRead
}

// GrepHit grep 命中
type GrepHit struct {
	File    string
	Line    int
	Content string
	Keyword string
}

// KeyFileRead 关键文件读取
type KeyFileRead struct {
	Path    string
	Content string
	Reason  string // 为什么读这个文件（file matched keyword / auto detected）
}

// Run 执行扫描，返回要注入 messages 的文本（可空）。
// 所有错误内部吞掉，失败即返回空串。
func (l *Landscaper) Run(ctx context.Context, projectDir, userPrompt string) string {
	if !l.cfg.Enabled || projectDir == "" {
		return ""
	}

	start := time.Now()
	scanCtx, cancel := context.WithTimeout(ctx, l.cfg.Timeout)
	defer cancel()

	result := &LandscapingResult{ProjectDir: projectDir}
	var resultMu sync.Mutex

	// === 1. 并发执行扫描 ===
	var wg sync.WaitGroup

	// 1a. 扫文件树
	wg.Add(1)
	go func() {
		defer wg.Done()
		files, structure, totalSize := l.scanProject(scanCtx, projectDir)
		resultMu.Lock()
		result.FilesScanned = len(files)
		result.Structure = structure
		result.TotalSize = totalSize
		resultMu.Unlock()
	}()

	// 1b. 提取关键词（零 I/O，直接做）
	keywords := extractKeywords(userPrompt)

	wg.Wait()

	// === 2. 关键词 grep ===
	if len(keywords) > 0 && result.FilesScanned > 0 {
		grepCtx, grepCancel := context.WithTimeout(scanCtx, 800*time.Millisecond)
		defer grepCancel()

		// 扫一遍文件找匹配
		pattern := strings.Join(keywords[:minInt(len(keywords), 15)], "|")
		hits := l.grepFiles(grepCtx, projectDir, pattern, keywords)

		resultMu.Lock()
		result.Keywords = keywords
		result.GrepHits = hits
		resultMu.Unlock()
	} else {
		result.Keywords = keywords
	}

	// === 3. 自动读取关键文件 ===
	// 触发条件：grep 命中了某个文件 or 检测到常见项目入口文件
	if len(result.GrepHits) > 0 {
		readCtx, readCancel := context.WithTimeout(scanCtx, 800*time.Millisecond)
		defer readCancel()
		keyFiles := l.readKeyFiles(readCtx, projectDir, result.GrepHits, keywords)
		resultMu.Lock()
		result.KeyFiles = keyFiles
		resultMu.Unlock()
	} else {
		// 无 grep 命中，尝试自动读一些高价值文件
		readCtx, readCancel := context.WithTimeout(scanCtx, 500*time.Millisecond)
		defer readCancel()
		keyFiles := l.autoDetectFiles(readCtx, projectDir)
		resultMu.Lock()
		result.KeyFiles = keyFiles
		resultMu.Unlock()
	}

	// === 4. 渲染注入文本 ===
	landscape := l.render(result, time.Since(start))
	return landscape
}

// scanProject 扫描项目目录，返回文件列表、树结构字符串、总大小
func (l *Landscaper) scanProject(ctx context.Context, root string) ([]string, string, int64) {
	var files []string
	var totalSize int64
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		defer close(done)
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if info.IsDir() {
				name := info.Name()
				if shouldSkipDir(name) {
					return filepath.SkipDir
				}
				return nil
			}

			// 跳过二进制文件和大文件
			if !isTextFile(path) {
				return nil
			}
			if info.Size() > l.cfg.MaxFileSize {
				return nil
			}

			mu.Lock()
			if len(files) >= l.cfg.MaxFiles {
				mu.Unlock()
				return filepath.SkipAll
			}
			rel, _ := filepath.Rel(root, path)
			files = append(files, rel)
			totalSize += info.Size()
			mu.Unlock()
			return nil
		})
	}()

	select {
	case <-ctx.Done():
		// 超时了，用已有的文件构建树
	case <-done:
	}

	mu.Lock()
	filesCopy := make([]string, len(files))
	copy(filesCopy, files)
	mu.Unlock()

	sort.Strings(filesCopy)
	structure := buildTree(filesCopy, root, 4, 10)
	return filesCopy, structure, totalSize
}

// shouldSkipDir 是否跳过某个目录
func shouldSkipDir(name string) bool {
	skip := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		"dist": true, "build": true, ".next": true, ".cache": true,
		"__pycache__": true, ".idea": true, ".vscode": true,
		"target": true, "bin": true, "obj": true,
	}
	return skip[name] || strings.HasPrefix(name, ".")
}

// isTextFile 是否文本文件（按扩展名判断）
func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	textExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
		".java": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
		".rs": true, ".rb": true, ".php": true, ".swift": true, ".kt": true,
		".cs": true, ".scala": true, ".sh": true, ".bash": true, ".ps1": true,
		".bat": true, ".md": true, ".txt": true, ".json": true, ".yaml": true,
		".yml": true, ".toml": true, ".xml": true, ".html": true, ".css": true,
		".scss": true, ".less": true, ".vue": true, ".sql": true, ".r": true,
		".pl": true, ".lua": true, ".dart": true, ".ex": true, ".exs": true,
		".erl": true, ".hs": true, ".fs": true, ".ml": true, ".jl": true,
		".ini": true, ".cfg": true, ".conf": true, ".properties": true,
		".dockerfile": true, ".gitignore": true, ".editorconfig": true,
		".mod": true, ".sum": true, ".csv": true,
	}
	if textExts[ext] {
		return true
	}
	// 一些无扩展名的文本文件
	base := strings.ToLower(filepath.Base(path))
	textFiles := map[string]bool{
		"makefile": true, "dockerfile": true, "readme": true,
		"license": true, "changelog": true, "contributing": true,
	}
	return textFiles[base]
}

// buildTree 构建目录树字符串
func buildTree(files []string, root string, maxDepth, maxItemsPerDir int) string {
	if len(files) == 0 {
		return "(empty project)"
	}

	rootNode := &dirNode{name: filepath.Base(root), subdirs: make(map[string]*dirNode)}

	for _, f := range files {
		parts := strings.Split(f, string(filepath.Separator))
		node := rootNode
		for i, part := range parts {
			if i == len(parts)-1 {
				node.files = append(node.files, part)
			} else {
				child, ok := node.subdirs[part]
				if !ok {
					child = &dirNode{name: part, subdirs: make(map[string]*dirNode)}
					node.subdirs[part] = child
				}
				node = child
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Project Structure] %s (%d files scanned)\n", rootNode.name, len(files)))
	sb.WriteString(renderNode(rootNode, "", maxDepth, maxItemsPerDir, 0))
	return sb.String()
}

// renderNode 递归渲染目录树
func renderNode(node *dirNode, prefix string, maxDepth, maxItems int, depth int) string {
	if depth > maxDepth {
		return ""
	}
	var sb strings.Builder

	// 排序子目录和文件
	subdirNames := make([]string, 0, len(node.subdirs))
	for name := range node.subdirs {
		subdirNames = append(subdirNames, name)
	}
	sort.Strings(subdirNames)
	sort.Strings(node.files)

	for _, name := range subdirNames {
		if len(node.subdirs) >= maxItems+1 && name == subdirNames[maxItems] {
			sb.WriteString(prefix + "  ...\n")
			break
		}
		sb.WriteString(prefix + name + "/\n")
		childPrefix := prefix + "  "
		sb.WriteString(renderNode(node.subdirs[name], childPrefix, maxDepth, maxItems, depth+1))
	}

	for i, fname := range node.files {
		if i >= maxItems {
			sb.WriteString(prefix + "  ...\n")
			break
		}
		sb.WriteString(prefix + "  " + fname + "\n")
	}

	return sb.String()
}

// extractKeywords 从用户 prompt 提取关键词
func extractKeywords(prompt string) []string {
	// 停用词
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
		"were": true, "be": true, "been": true, "being": true, "have": true, "has": true,
		"had": true, "do": true, "does": true, "did": true, "will": true, "would": true,
		"shall": true, "should": true, "may": true, "might": true, "must": true, "can": true,
		"could": true, "need": true, "dare": true, "ought": true, "used": true, "to": true,
		"of": true, "in": true, "for": true, "on": true, "with": true, "at": true,
		"by": true, "from": true, "as": true, "into": true, "through": true, "during": true,
		"before": true, "after": true, "above": true, "below": true, "between": true, "out": true,
		"off": true, "over": true, "under": true, "again": true, "further": true, "then": true,
		"once": true, "here": true, "there": true, "when": true, "where": true, "why": true,
		"how": true, "all": true, "both": true, "each": true, "few": true, "more": true,
		"most": true, "other": true, "some": true, "such": true, "no": true, "nor": true,
		"not": true, "only": true, "own": true, "same": true, "so": true, "than": true,
		"too": true, "very": true, "just": true, "because": true, "but": true, "and": true,
		"or": true, "if": true, "while": true, "about": true, "up": true, "down": true,
		"i": true, "you": true, "he": true, "she": true, "it": true, "we": true,
		"they": true, "me": true, "him": true, "her": true, "us": true, "them": true,
		"my": true, "your": true, "his": true, "its": true, "our": true, "their": true,
		"this": true, "that": true, "these": true, "those": true, "what": true, "which": true,
		"who": true, "whom": true, "go": true, "please": true, "help": true, "want": true,
		"make": true, "get": true, "take": true, "give": true, "find": true, "think": true,
		"let": true, "using": true, "use": true, "also": true, "add": true, "new": true,
		"code": true, "program": true, "function": true,
		// 中文停用词
		"的": true, "了": true, "是": true, "在": true, "我": true, "有": true,
		"和": true, "就": true, "不": true, "人": true, "都": true, "一": true,
		"一个": true, "上": true, "也": true, "很": true, "到": true, "说": true,
		"要": true, "去": true, "你": true, "会": true, "着": true, "没有": true,
		"看": true, "好": true, "自己": true, "这": true, "那": true, "什么": true,
		"怎么": true, "如何": true, "帮": true, "请": true, "给": true, "写": true,
		"改": true, "修": true, "做": true, "加": true, "里": true, "中": true,
	}

	// 1. 分词（英文用空格，中文按字拆分+bigram）
	var tokens []string

	// 英文/数字：按空格+标点拆分
	sepRe := regexp.MustCompile(`[\s,.;:!?，。；：！？、()（）\[\]【】"'<>/\\|+=\-*&^%$#@~]+`)
	enTokens := sepRe.Split(prompt, -1)
	for _, t := range enTokens {
		t = strings.TrimSpace(t)
		if len(t) >= 2 && len(t) <= 50 {
			// 纯英文/数字 token 直接加
			if isASCII(t) {
				tokens = append(tokens, strings.ToLower(t))
			}
		}
	}

	// 2. 中文：bigram 提取
	runes := []rune(prompt)
	for i := 0; i < len(runes)-1; i++ {
		r1, r2 := runes[i], runes[i+1]
		if isChinese(r1) && isChinese(r2) {
			bigram := string(r1) + string(r2)
			tokens = append(tokens, bigram)
		}
	}

	// 3. 提取可能的文件名
	fileRe := regexp.MustCompile(`[a-zA-Z0-9_.-]+\.(?:go|py|ts|js|java|c|cpp|rs|md|json|yaml|toml|html)`)
	for _, m := range fileRe.FindAllString(prompt, -1) {
		tokens = append(tokens, strings.ToLower(m))
	}

	// 4. 去重 + 去停用词 + 过滤短词
	seen := make(map[string]bool)
	var keywords []string
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if seen[t] {
			continue
		}
		if stopWords[t] {
			continue
		}
		// 去掉全是数字的
		allDigits := true
		for _, r := range t {
			if !unicode.IsDigit(r) {
				allDigits = false
				break
			}
		}
		if allDigits {
			continue
		}
		seen[t] = true
		keywords = append(keywords, t)
	}

	// 5. 截断
	if len(keywords) > 20 {
		keywords = keywords[:20]
	}
	return keywords
}

// isASCII 是否纯 ASCII
func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// isChinese 是否中文字符
func isChinese(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

// grepFiles 在文件里 grep 关键词
func (l *Landscaper) grepFiles(ctx context.Context, root, pattern string, keywords []string) []GrepHit {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil
	}

	var hits []GrepHit
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		defer close(done)
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if !isTextFile(path) {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil || int64(len(data)) > l.cfg.MaxFileSize {
				return nil
			}

			rel, _ := filepath.Rel(root, path)
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if len(hits) >= l.cfg.GrepMaxMatches*5 { // 多找点后面再过滤
					return filepath.SkipAll
				}
				if re.MatchString(line) {
					mu.Lock()
					hit := GrepHit{
						File:    rel,
						Line:    i + 1,
						Content: strings.TrimSpace(truncateString(line, 120)),
					}
					// 找命中的具体关键词
					for _, kw := range keywords {
						if strings.Contains(strings.ToLower(line), strings.ToLower(kw)) {
							hit.Keyword = kw
							break
						}
					}
					hits = append(hits, hit)
					mu.Unlock()
				}
			}
			return nil
		})
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}

	// 按文件去重，每文件只保留前几个
	perFileLimit := 3
	fileHits := make(map[string][]GrepHit)
	for _, h := range hits {
		if len(fileHits[h.File]) < perFileLimit {
			fileHits[h.File] = append(fileHits[h.File], h)
		}
	}

	var result []GrepHit
	for _, hs := range fileHits {
		result = append(result, hs...)
	}

	// 总数限制
	if len(result) > l.cfg.GrepMaxMatches {
		result = result[:l.cfg.GrepMaxMatches]
	}
	return result
}

// readKeyFiles 读取 grep 命中的关键文件（按命中次数排序）
func (l *Landscaper) readKeyFiles(ctx context.Context, root string, hits []GrepHit, keywords []string) []KeyFileRead {
	// 统计每个文件的命中数
	fileScore := make(map[string]int)
	fileReason := make(map[string]string)
	for _, h := range hits {
		fileScore[h.File]++
		if fileReason[h.File] == "" {
			fileReason[h.File] = fmt.Sprintf("matched keyword \"%s\"", h.Keyword)
		}
	}

	// 排序
	type scoredFile struct {
		path  string
		score int
	}
	var ranked []scoredFile
	for p, s := range fileScore {
		ranked = append(ranked, scoredFile{p, s})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	var result []KeyFileRead
	for i, f := range ranked {
		if i >= l.cfg.MaxReadFiles {
			break
		}
		select {
		case <-ctx.Done():
			break
		default:
		}

		fullPath := filepath.Join(root, f.path)
		content := readFileSafe(fullPath, l.cfg.MaxFileSize, l.cfg.MaxFileChars)
		if content != "" {
			result = append(result, KeyFileRead{
				Path:    f.path,
				Content: content,
				Reason:  fileReason[f.path],
			})
		}
	}

	return result
}

// autoDetectFiles 自动检测高价值文件（无 grep 命中时的 fallback）
func (l *Landscaper) autoDetectFiles(ctx context.Context, root string) []KeyFileRead {
	highValueFiles := []string{
		"go.mod", "package.json", "setup.py", "Cargo.toml", "Makefile",
		"README.md", "main.go", "main.py", "index.ts", "pom.xml",
		"build.gradle", "requirements.txt", "pyproject.toml",
	}

	var result []KeyFileRead
	for _, name := range highValueFiles {
		if len(result) >= l.cfg.MaxReadFiles {
			break
		}
		select {
		case <-ctx.Done():
			break
		default:
		}

		fullPath := filepath.Join(root, name)
		if _, err := os.Stat(fullPath); err != nil {
			continue
		}
		content := readFileSafe(fullPath, l.cfg.MaxFileSize, l.cfg.MaxFileChars)
		if content != "" {
			result = append(result, KeyFileRead{
				Path:    name,
				Content: content,
				Reason:  "auto-detected project entry file",
			})
		}
	}
	return result
}

// readFileSafe 安全读取文件
func readFileSafe(path string, maxSize int64, maxChars int) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if info.Size() > maxSize {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	if len(content) > maxChars {
		content = content[:maxChars] + "\n... [truncated]"
	}
	return content
}

// truncateString 截断字符串
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// minInt 小整数最小值
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// render 渲染注入文本
func (l *Landscaper) render(r *LandscapingResult, elapsed time.Duration) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("<landscaping-context>\n"))
	sb.WriteString(fmt.Sprintf("<!-- 自动前置扫描完成 (%.0fms)，无需 LLM 推理 -->\n", float64(elapsed)/float64(time.Millisecond)))
	sb.WriteString(fmt.Sprintf("<!-- 项目目录: %s -->\n", r.ProjectDir))
	sb.WriteString(fmt.Sprintf("<!-- 扫描文件数: %d, 总大小: %s -->\n", r.FilesScanned, humanBytes(r.TotalSize)))
	sb.WriteString(fmt.Sprintf("<!-- 关键词提取: %d 个 -->\n", len(r.Keywords)))

	// 项目结构
	if r.Structure != "" && r.FilesScanned > 0 {
		sb.WriteString("\n")
		sb.WriteString(r.Structure)
	}

	// 关键词
	if len(r.Keywords) > 0 {
		sb.WriteString("\n[Extracted Keywords]\n")
		sb.WriteString(strings.Join(r.Keywords, ", "))
		sb.WriteString("\n")
	}

	// grep 命中
	if len(r.GrepHits) > 0 {
		sb.WriteString("\n[Grep Matches]\n")
		// 按文件分组
		fileHits := make(map[string][]GrepHit)
		for _, h := range r.GrepHits {
			fileHits[h.File] = append(fileHits[h.File], h)
		}
		for file, hits := range fileHits {
			sb.WriteString(fmt.Sprintf("  %s (%d matches for \"%s\"):\n", file, len(hits), hits[0].Keyword))
			for _, h := range hits {
				sb.WriteString(fmt.Sprintf("    L%d: %s\n", h.Line, h.Content))
			}
		}
	}

	// 关键文件内容
	if len(r.KeyFiles) > 0 {
		sb.WriteString("\n[Key File Contents]\n")
		for _, kf := range r.KeyFiles {
			sb.WriteString(fmt.Sprintf("\n### %s (%s) ###\n", kf.Path, kf.Reason))
			sb.WriteString(kf.Content)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("</landscaping-context>\n")

	log.Printf("[Landscaper] completed in %v: %d files, %d keywords, %d grep hits, %d key files",
		elapsed, r.FilesScanned, len(r.Keywords), len(r.GrepHits), len(r.KeyFiles))

	return sb.String()
}

// humanBytes 格式化字节数
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// 确保 runtime 导入（某些系统可能用到）
var _ = runtime.Version
