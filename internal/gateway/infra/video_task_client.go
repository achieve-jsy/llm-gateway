package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/example/llm-gateway/internal/httpclient"
)

// VideoTaskClient 通过控制面内部 API 读写 video_tasks。
type VideoTaskClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewVideoTaskClient 创建客户端。
func NewVideoTaskClient(baseURL, token string) *VideoTaskClient {
	cfg := httpclient.DefaultConfig()
	cfg.Timeout = 3 * time.Second
	cfg.ResponseHeaderTimeout = 3 * time.Second
	return &VideoTaskClient{
		baseURL:    baseURL,
		token:      token,
		httpClient: httpclient.NewHTTPClient(&cfg),
	}
}

// CreateVideoTaskRequest 与控制面 InternalCreate 对齐。
type CreateVideoTaskRequest struct {
	TaskID       string `json:"task_id"`
	APIKeyID     string `json:"api_key_id"`
	APIKeyName   string `json:"api_key_name"`
	APIKeyType   string `json:"api_key_type"`
	UserID       string `json:"user_id"`
	UserName     string `json:"user_name"`
	Model        string `json:"model"`
	ProviderName string `json:"provider_name"`
	ProviderType string `json:"provider_type"`
	Status       string `json:"status,omitempty"`
	RequestBody  string `json:"request_body,omitempty"`
}

// UpdateVideoTaskRequest 与控制面 InternalUpdate 对齐。
type UpdateVideoTaskRequest struct {
	Status       string `json:"status,omitempty"`
	VideoURL     string `json:"video_url,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// Create 写入 video_tasks。
func (c *VideoTaskClient) Create(ctx context.Context, req CreateVideoTaskRequest) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("video task client not configured")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/video/tasks", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-Token", c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("control-plane create video task: status=%d body=%s", resp.StatusCode, string(b))
	}
	return nil
}

// Update 增量更新任务状态和详情字段。
func (c *VideoTaskClient) Update(ctx context.Context, taskID string, req UpdateVideoTaskRequest) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("video task client not configured")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("%s/internal/v1/video/tasks/%s", c.baseURL, url.PathEscape(taskID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-Token", c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("control-plane update video task: status=%d body=%s", resp.StatusCode, string(b))
	}
	return nil
}

// UpdateStatus 更新任务状态（兼容旧调用方）。
func (c *VideoTaskClient) UpdateStatus(ctx context.Context, taskID, status string) error {
	return c.Update(ctx, taskID, UpdateVideoTaskRequest{Status: status})
}

// LookupProviderType 查询任务对应的 provider_type。
func (c *VideoTaskClient) LookupProviderType(ctx context.Context, taskID string) (string, error) {
	if c == nil || c.baseURL == "" {
		return "", fmt.Errorf("video task client not configured")
	}
	u := fmt.Sprintf("%s/internal/v1/video/tasks/%s/provider-type", c.baseURL, url.PathEscape(taskID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("X-Internal-Token", c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("control-plane lookup provider type: status=%d body=%s", resp.StatusCode, string(b))
	}
	var out struct {
		ProviderType string `json:"provider_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ProviderType, nil
}
