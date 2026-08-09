package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/auto-code/auto-code/internal/api"
	"github.com/auto-code/auto-code/internal/engine"
	"github.com/auto-code/auto-code/internal/state"
	"github.com/auto-code/auto-code/internal/types"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	appState := state.NewAppState()

	ollamaConfig := api.DefaultOllamaConfig()
	if apiKey := os.Getenv("OLLAMA_API_KEY"); apiKey != "" {
		ollamaConfig = api.CloudOllamaConfig(apiKey)
	}
	if baseURL := os.Getenv("OLLAMA_BASE_URL"); baseURL != "" {
		ollamaConfig.BaseURL = baseURL
	}
	if model := os.Getenv("OLLAMA_MODEL"); model != "" {
		ollamaConfig.Model = model
	} else {
		ollamaConfig.Model = "qwen3:latest"
	}

	engineConfig := &engine.QueryEngineConfig{
		MaxTurns:           100,
		UserSpecifiedModel: types.ModelSetting(ollamaConfig.Model),
		OllamaConfig:       ollamaConfig,
	}

	queryEngine := engine.NewQueryEngine(appState, engineConfig)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		queryEngine.Interrupt()
		cancel()
	}()

	queryEngine.Startup(ctx)
	defer queryEngine.Shutdown(ctx)

	health := queryEngine.CheckHealth(ctx)
	if !health.Connected {
		return fmt.Errorf("%s", health.Error)
	}
	fmt.Printf("Ollama connected: %d models available\n", health.AvailableModels)

	if len(os.Args) < 2 {
		return fmt.Errorf("usage: auto-code-cli <prompt>")
	}

	prompt := os.Args[1]
	outputCh := queryEngine.SubmitMessage(ctx, prompt)

	for msg := range outputCh {
		switch msg.Type {
		case "assistant":
			if msg.Message != nil {
				if msg.Message.Content != "" {
					fmt.Print(msg.Message.Content)
				}
				if msg.Message.Thinking != "" {
					fmt.Printf("[Thinking: %s]", msg.Message.Thinking)
				}
			}
		case "result":
			fmt.Printf("\n[%s]\n", msg.Subtype)
			return nil
		case "error":
			if msg.Message != nil {
				return fmt.Errorf("%s", msg.Message.Content)
			}
			return fmt.Errorf("unknown error")
		}
	}
	return nil
}
