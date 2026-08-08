package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/auto-code/auto-code/internal/api"
	"github.com/auto-code/auto-code/internal/engine"
	engctx "github.com/auto-code/auto-code/internal/engine/context"
	"github.com/auto-code/auto-code/internal/server"
	appState "github.com/auto-code/auto-code/internal/state"
	"github.com/auto-code/auto-code/internal/types"
)

// auto-code-server 是 stdio NDJSON-RPC 服务端入口。
// 由 VS Code 插件作为子进程启动，复用 internal/* 全部核心逻辑。
//
// 用法：
//
//	auto-code-server --cwd <workspace>
//
// 也可通过环境变量 AUTO_CODE_CWD 指定工作区目录。
func main() {
	cwd := flag.String("cwd", "", "工作区目录（VS Code 工作区根路径）")
	flag.Parse()

	if *cwd == "" {
		*cwd = os.Getenv("AUTO_CODE_CWD")
	}
	if *cwd == "" {
		var err error
		*cwd, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: cannot determine cwd:", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 信号处理：允许 Ctrl+C 优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// 复用 main.go 中的初始化逻辑（精简版）
	app := appState.NewAppState()
	ctxBuilder := engctx.NewContextBuilder(*cwd)
	_ = ctxBuilder.LoadMemoryFiles(ctx)

	adapter := server.NewAdapter(app, ctxBuilder)

	// 持久化工作区目录，供前端读取
	_ = adapter.SetWorkspace(*cwd)

	// 解析 Ollama 配置：优先 AppState 已加载的配置，其次环境变量，最后默认值
	ollamaConfig := api.DefaultOllamaConfig()
	if apiKey := os.Getenv("OLLAMA_API_KEY"); apiKey != "" {
		ollamaConfig = api.CloudOllamaConfig(apiKey)
	}
	if baseURL := os.Getenv("OLLAMA_BASE_URL"); baseURL != "" {
		ollamaConfig.BaseURL = baseURL
	}
	if appModel := app.GetMainLoopModel(); appModel != "" {
		ollamaConfig.Model = string(appModel)
	} else if model := os.Getenv("OLLAMA_MODEL"); model != "" {
		ollamaConfig.Model = model
	} else {
		ollamaConfig.Model = "qwen3:latest"
	}
	if baseURL, ok := app.GetSetting("ollama_base_url"); ok {
		if s, ok := baseURL.(string); ok && s != "" {
			ollamaConfig.BaseURL = s
		}
	}
	if apiKey, ok := app.GetSetting("ollama_api_key"); ok {
		if s, ok := apiKey.(string); ok && s != "" {
			ollamaConfig.APIKey = s
		}
	}

	engineConfig := &engine.QueryEngineConfig{
		CWD:                *cwd,
		UserSpecifiedModel: types.ModelSetting(ollamaConfig.Model),
		MaxTurns:           100,
		OllamaConfig:       ollamaConfig,
	}

	eng := engine.NewQueryEngine(app, engineConfig)
	eng.SetContextBuilder(ctxBuilder)
	eng.Startup(ctx)
	defer eng.Shutdown(ctx)

	adapter.SetEngine(eng)

	srv := server.NewStdioServer(adapter)

	// 启动提示打到 stderr，避免污染 stdout 的 NDJSON 协议
	fmt.Fprintln(os.Stderr, "auto-code-server ready, cwd="+*cwd+", model="+ollamaConfig.Model)

	if err := srv.Serve(ctx); err != nil && err != context.Canceled {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}
