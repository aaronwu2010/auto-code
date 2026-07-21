package main

import (
	"context"
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"github.com/auto-code/auto-code/internal/api"
	bootstrapState "github.com/auto-code/auto-code/internal/bootstrap"
	"github.com/auto-code/auto-code/internal/engine"
	engineContext "github.com/auto-code/auto-code/internal/engine/context"
	appState "github.com/auto-code/auto-code/internal/state"
	"github.com/auto-code/auto-code/internal/types"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	cwd, _ := os.Getwd()

	bootstrap := bootstrapState.InitBootstrap(cwd)

	app := appState.NewAppState()

	ctxBuilder := engineContext.NewContextBuilder(cwd)

	bindings := appState.NewWailsBindings(app, ctxBuilder)

	// 从 AppState 加载已保存的模型配置
	appModel := app.GetMainLoopModel()

	ollamaConfig := api.DefaultOllamaConfig()

	if apiKey := os.Getenv("OLLAMA_API_KEY"); apiKey != "" {
		ollamaConfig = api.CloudOllamaConfig(apiKey)
	}
	if baseURL := os.Getenv("OLLAMA_BASE_URL"); baseURL != "" {
		ollamaConfig.BaseURL = baseURL
	}

	// 优先使用配置文件中的模型，其次使用环境变量，最后使用默认值
	if appModel != "" {
		ollamaConfig.Model = string(appModel)
	} else if model := os.Getenv("OLLAMA_MODEL"); model != "" {
		ollamaConfig.Model = model
	} else {
		ollamaConfig.Model = "qwen3:latest"
	}

	// 从 AppState 加载已保存的 URL 和 API Key
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
		CWD:                cwd,
		UserSpecifiedModel: types.ModelSetting(ollamaConfig.Model),
		MaxTurns:           100,
		OllamaConfig:       ollamaConfig,
	}

	queryEngine := engine.NewQueryEngine(app, engineConfig)

	bindings.SetEngine(queryEngine)

	_ = bootstrap

	err := wails.Run(&options.App{
		Title:     "Auto Code",
		Width:     1200,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: func(ctx context.Context) {
			bindings.Startup(ctx)
			queryEngine.Startup(ctx)
			_ = ctxBuilder.LoadMemoryFiles(ctx)

			health := queryEngine.CheckHealth(ctx)
			if !health.Connected {
				println("Warning: Ollama not connected -", health.Error)
			} else {
				println("Ollama connected:", health.AvailableModels, "models available")
			}
		},
		OnShutdown: func(ctx context.Context) {
			queryEngine.Shutdown(ctx)
		},
		Bind: []interface{}{
			bindings,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
