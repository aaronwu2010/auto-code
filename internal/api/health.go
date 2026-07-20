package api

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type HealthStatus struct {
	Connected       bool   `json:"connected"`
	IsLocal         bool   `json:"is_local"`
	ServerVersion   string `json:"server_version,omitempty"`
	AvailableModels int    `json:"available_models"`
	Error           string `json:"error,omitempty"`
}

func (c *Client) CheckHealth(ctx context.Context) *HealthStatus {
	status := &HealthStatus{
		IsLocal: c.config.IsLocal,
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(checkCtx, http.MethodGet, c.config.BaseURL+"/tags", nil)
	if err != nil {
		status.Error = fmt.Sprintf("creating health check request: %v", err)
		return status
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if IsConnectionRefused(err) {
			status.Error = "Ollama 服务未启动，请先运行 `ollama serve`"
		} else {
			status.Error = fmt.Sprintf("连接失败: %v", err)
		}
		return status
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		status.Error = fmt.Sprintf("服务返回异常状态码: %d", resp.StatusCode)
		return status
	}

	status.Connected = true

	models, err := c.ListModels(checkCtx)
	if err == nil {
		status.AvailableModels = len(models)
	}

	return status
}

func (c *Client) WaitForConnection(ctx context.Context, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		status := c.CheckHealth(ctx)
		if status.Connected {
			return nil
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("等待 Ollama 连接超时 (%v)", maxWait)
}