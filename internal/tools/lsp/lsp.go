package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "LSP"
	maxResultChars  = 100000
	descriptionText = "Query LSP servers for code analysis."
	defaultTimeout  = 30 * time.Second
)

type LSPInput struct {
	Action    string `json:"action"`
	FilePath  string `json:"file_path,omitempty"`
	Line      int    `json:"line,omitempty"`
	Character int    `json:"character,omitempty"`
	Symbol    string `json:"symbol,omitempty"`
}

type LSPTool struct {
	*tools.BaseTool
	mu       sync.Mutex
	clients  map[string]*LSPClient
	nextID   int64
}

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type LSPClient struct {
	mu        sync.Mutex
	conn      net.Conn
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	reader    *bufio.Reader
	writer    *bufio.Writer
	pending   map[int64]chan *jsonRPCResponse
	nextID    int64
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

func NewLSPTool() *LSPTool {
	t := &LSPTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
		clients:  make(map[string]*LSPClient),
	}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":    map[string]any{"type": "string", "description": "LSP action: definitions, references, hover, diagnostics", "enum": []string{"definitions", "references", "hover", "diagnostics"}},
			"file_path": map[string]any{"type": "string", "description": "The file path to query"},
			"line":      map[string]any{"type": "integer", "description": "Line number (1-indexed)"},
			"character": map[string]any{"type": "integer", "description": "Character position (0-indexed)"},
			"symbol":    map[string]any{"type": "string", "description": "Symbol name to search for"},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
	return t
}

func (t *LSPTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp LSPInput
	switch v := input.(type) {
	case LSPInput:
		inp = v
	case map[string]any:
		parsed, err := ParseLSPInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for LSPTool: expected LSPInput or map[string]any, got %T", input)
	}

	client, err := t.getOrCreateClient(ctx, inp.FilePath)
	if err != nil {
		return &tools.ToolResult{
			Data: fmt.Sprintf("LSP %s: failed to connect to LSP server: %v", inp.Action, err),
		}, nil
	}

	result, err := client.executeAction(ctx, inp)
	if err != nil {
		return &tools.ToolResult{
			Data: fmt.Sprintf("LSP %s failed: %v", inp.Action, err),
		}, nil
	}

	return &tools.ToolResult{Data: result}, nil
}

func (t *LSPTool) getOrCreateClient(ctx context.Context, filePath string) (*LSPClient, error) {
	lang := detectLanguage(filePath)

	t.mu.Lock()
	if client, ok := t.clients[lang]; ok {
		t.mu.Unlock()
		return client, nil
	}
	t.mu.Unlock()

	client, err := t.connectToServer(ctx, lang)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.clients[lang] = client
	t.mu.Unlock()

	return client, nil
}

func (t *LSPTool) connectToServer(ctx context.Context, lang string) (*LSPClient, error) {
	serverCmd := getLSPCommand(lang)
	if serverCmd == "" {
		return nil, fmt.Errorf("no LSP server configured for language: %s", lang)
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "cmd", "/c", serverCmd)
	cmd.Stderr = os.Stderr

	// StdinPipe/StdoutPipe 会自动设置 cmd.Stdin/cmd.Stdout
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting LSP server %q: %w", serverCmd, err)
	}

	clientCtx, clientCancel := context.WithCancel(context.Background())
	client := &LSPClient{
		cmd:     cmd,
		stdin:   stdinPipe,
		stdout:  stdoutPipe,
		reader:  bufio.NewReader(stdoutPipe),
		writer:  bufio.NewWriter(stdinPipe),
		pending: make(map[int64]chan *jsonRPCResponse),
		cancel:  clientCancel,
		done:    make(chan struct{}),
	}

	go client.readLoop(clientCtx)

	if err := client.initialize(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("initializing LSP server: %w", err)
	}

	return client, nil
}

func (c *LSPClient) readLoop(ctx context.Context) {
	defer close(c.done)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		if resp.ID > 0 {
			c.mu.Lock()
			ch, ok := c.pending[resp.ID]
			if ok {
				delete(c.pending, resp.ID)
			}
			c.mu.Unlock()

			if ok {
				select {
				case ch <- &resp:
				default:
				}
			}
		}
	}
}

func (c *LSPClient) sendRequest(ctx context.Context, method string, params interface{}) (*jsonRPCResponse, error) {
	id := atomic.AddInt64(&c.nextID, 1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	ch := make(chan *jsonRPCResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	reqBytes, err := json.Marshal(req)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	if _, err := c.writer.Write(append(reqBytes, '\n')); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	_ = c.writer.Flush()

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("LSP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	}
}

func (c *LSPClient) initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"processId":             os.Getpid(),
		"capabilities":          map[string]interface{}{},
		"workspaceFolders":      nil,
		"rootUri":               "",
		"initializationOptions": nil,
	}

	resp, err := c.sendRequest(ctx, "initialize", params)
	if err != nil {
		return err
	}

	// 发送 initialized 通知
	initialized := map[string]interface{}{}
	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "initialized",
		Params:  initialized,
	}
	notifBytes, _ := json.Marshal(notif)
	_, _ = c.writer.Write(append(notifBytes, '\n'))
	_ = c.writer.Flush()

	_ = resp
	return nil
}

func (c *LSPClient) executeAction(ctx context.Context, inp LSPInput) (string, error) {
	switch inp.Action {
	case "definitions":
		return c.getDefinitions(ctx, inp)
	case "references":
		return c.getReferences(ctx, inp)
	case "hover":
		return c.getHover(ctx, inp)
	case "diagnostics":
		return c.getDiagnostics(ctx, inp)
	default:
		return "", fmt.Errorf("unsupported action: %s", inp.Action)
	}
}

func (c *LSPClient) getDefinitions(ctx context.Context, inp LSPInput) (string, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": filePathToURI(inp.FilePath),
		},
		"position": map[string]interface{}{
			"line":      inp.Line - 1,
			"character": inp.Character,
		},
	}

	resp, err := c.sendRequest(ctx, "textDocument/definition", params)
	if err != nil {
		return "", err
	}

	if resp == nil || len(resp.Result) == 0 || string(resp.Result) == "null" {
		return "No definition found", nil
	}

	var loc interface{}
	if err := json.Unmarshal(resp.Result, &loc); err != nil {
		return fmt.Sprintf("Raw response: %s", string(resp.Result)), nil
	}

	resultBytes, _ := json.MarshalIndent(loc, "", "  ")
	return string(resultBytes), nil
}

func (c *LSPClient) getReferences(ctx context.Context, inp LSPInput) (string, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": filePathToURI(inp.FilePath),
		},
		"position": map[string]interface{}{
			"line":      inp.Line - 1,
			"character": inp.Character,
		},
		"context": map[string]interface{}{
			"includeDeclaration": true,
		},
	}

	resp, err := c.sendRequest(ctx, "textDocument/references", params)
	if err != nil {
		return "", err
	}

	if resp == nil || len(resp.Result) == 0 || string(resp.Result) == "null" {
		return "No references found", nil
	}

	var locs []interface{}
	if err := json.Unmarshal(resp.Result, &locs); err != nil {
		return fmt.Sprintf("Raw response: %s", string(resp.Result)), nil
	}

	resultBytes, _ := json.MarshalIndent(locs, "", "  ")
	return string(resultBytes), nil
}

func (c *LSPClient) getHover(ctx context.Context, inp LSPInput) (string, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": filePathToURI(inp.FilePath),
		},
		"position": map[string]interface{}{
			"line":      inp.Line - 1,
			"character": inp.Character,
		},
	}

	resp, err := c.sendRequest(ctx, "textDocument/hover", params)
	if err != nil {
		return "", err
	}

	if resp == nil || len(resp.Result) == 0 || string(resp.Result) == "null" {
		return "No hover information available", nil
	}

	var hover interface{}
	if err := json.Unmarshal(resp.Result, &hover); err != nil {
		return fmt.Sprintf("Raw response: %s", string(resp.Result)), nil
	}

	resultBytes, _ := json.MarshalIndent(hover, "", "  ")
	return string(resultBytes), nil
}

func (c *LSPClient) getDiagnostics(ctx context.Context, inp LSPInput) (string, error) {
	// 诊断信息由服务器通过推送发送，这里请求 documentDiagnostic（LSP 3.16+）
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": filePathToURI(inp.FilePath),
		},
	}

	resp, err := c.sendRequest(ctx, "textDocument/diagnostic", params)
	if err != nil {
		// 如果服务器不支持 diagnostic 请求，返回空结果
		return "Diagnostics not available for this language server", nil
	}

	if resp == nil || len(resp.Result) == 0 || string(resp.Result) == "null" {
		return "No diagnostics found", nil
	}

	var diag interface{}
	if err := json.Unmarshal(resp.Result, &diag); err != nil {
		return fmt.Sprintf("Raw response: %s", string(resp.Result)), nil
	}

	resultBytes, _ := json.MarshalIndent(diag, "", "  ")
	return string(resultBytes), nil
}

func (c *LSPClient) Close() {
	c.closeOnce.Do(func() {
		c.cancel()
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		if c.stdout != nil {
			_ = c.stdout.Close()
		}
		if c.conn != nil {
			_ = c.conn.Close()
		}
		<-c.done
	})
}

func (t *LSPTool) CloseAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, client := range t.clients {
		client.Close()
	}
	t.clients = make(map[string]*LSPClient)
}

func (t *LSPTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Query LSP servers for code analysis. Actions:
- definitions: Go to definition of a symbol (requires file_path, line, character)
- references: Find all references to a symbol (requires file_path, line, character)
- hover: Get hover information for a symbol (requires file_path, line, character)
- diagnostics: Get diagnostics for a file (requires file_path)

Supported languages: Go, TypeScript, JavaScript, Python, Rust, and more (if LSP server installed).`, nil
}

func ParseLSPInput(raw map[string]any) (LSPInput, error) {
	inp := LSPInput{}
	if v, ok := raw["action"].(string); ok {
		inp.Action = v
	}
	if v, ok := raw["file_path"].(string); ok {
		inp.FilePath = v
	}
	if v, ok := raw["line"].(float64); ok {
		inp.Line = int(v)
	} else if v, ok := raw["line"].(int); ok {
		inp.Line = v
	}
	if v, ok := raw["character"].(float64); ok {
		inp.Character = int(v)
	} else if v, ok := raw["character"].(int); ok {
		inp.Character = v
	}
	if v, ok := raw["symbol"].(string); ok {
		inp.Symbol = v
	}
	if strings.TrimSpace(inp.Action) == "" {
		return inp, fmt.Errorf("action is required")
	}
	return inp, nil
}

// detectLanguage 根据文件扩展名检测编程语言
func detectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".cpp", ".cc", ".cxx", ".c", ".h", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".cs":
		return "csharp"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".lua":
		return "lua"
	case ".r":
		return "r"
	case ".sql":
		return "sql"
	case ".html", ".htm":
		return "html"
	case ".css", ".scss", ".less":
		return "css"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}

// getLSPCommand 获取指定语言的 LSP 服务器命令
func getLSPCommand(lang string) string {
	switch lang {
	case "go":
		return "gopls"
	case "typescript", "javascript":
		return "typescript-language-server --stdio"
	case "python":
		return "pylsp"
	case "rust":
		return "rust-analyzer"
	case "java":
		return "jdtls"
	case "cpp":
		return "clangd"
	case "ruby":
		return "ruby-lsp"
	case "csharp":
		return "dotnet csharp-language-server"
	case "swift":
		return "sourcekit-lsp"
	case "kotlin":
		return "kotlin-language-server"
	case "lua":
		return "lua-language-server"
	case "r":
		return "R-language-server"
	case "html":
		return "vscode-html-language-server --stdio"
	case "css":
		return "vscode-css-language-server --stdio"
	default:
		return ""
	}
}

// filePathToURI 将文件路径转换为 LSP URI
func filePathToURI(path string) string {
	if strings.HasPrefix(path, "file://") {
		return path
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "file://" + path
	}
	return "file://" + filepath.ToSlash(absPath)
}

// LSPPosition 辅助：转换 1-indexed 行为 LSP 0-indexed
func lspPosition(line, character int) map[string]interface{} {
	return map[string]interface{}{
		"line":      line - 1,
		"character": character,
	}
}

// LSPRange 辅助：创建 LSP range
func lspRange(startLine, startChar, endLine, endChar int) map[string]interface{} {
	return map[string]interface{}{
		"start": map[string]interface{}{
			"line":      startLine - 1,
			"character": startChar,
		},
		"end": map[string]interface{}{
			"line":      endLine - 1,
			"character": endChar,
		},
	}
}

// LSPDiagnosticSeverity 诊断严重级别
func lspDiagnosticSeverity(level int) string {
	switch level {
	case 1:
		return "Error"
	case 2:
		return "Warning"
	case 3:
		return "Info"
	case 4:
		return "Hint"
	default:
		return "Unknown (level " + strconv.Itoa(level) + ")"
	}
}
