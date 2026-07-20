package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/auto-code/auto-code/internal/mcp"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	server, err := mcp.NewMCPServer()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error creating MCP server:", err.Error())
		os.Exit(1)
	}

	if err := server.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error serving MCP:", err.Error())
		os.Exit(1)
	}
}