package volcengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/core/video"
)

// Adapter 火山引擎官方适配器
type Adapter struct {
	name    string
	baseURL string
	signer  *Signer
	client  *http.Client
}

// NewAdapter 创建官方适配器
func NewAdapter(name, baseURL, accessKey, secretKey, region string) *Adapter {
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &Adapter{
		name:    name,
		baseURL: baseURL,
		signer:  NewSigner(accessKey, secretKey, region),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name 返回适配器名称
func (a *Adapter) Name() string {
	return a.name
}

// CreateTask 创建视频任务
func (a *Adapter) CreateTask(ctx context.Context, req json.RawMessage) (string, error) {
	url := a.baseURL + "/api/v3/contents/generations/tasks"

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(req))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Host = strings.TrimPrefix(strings.TrimPrefix(a.baseURL, "https://"), "http://")
	a.signer.Sign(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upstream error: status=%d body=%s", resp.StatusCode, string(body))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.ID, nil
}

// GetTask 查询任务状态
func (a *Adapter) GetTask(ctx context.Context, taskID string) (json.RawMessage, error) {
	url := a.baseURL + "/api/v3/contents/generations/tasks/" + taskID

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Host = strings.TrimPrefix(strings.TrimPrefix(a.baseURL, "https://"), "http://")
	a.signer.Sign(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream error: status=%d body=%s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// DoAction 执行素材操作
func (a *Adapter) DoAction(ctx context.Context, action string, req json.RawMessage) (json.RawMessage, error) {
	url := a.baseURL + "/assets/" + action

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(req))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Host = strings.TrimPrefix(strings.TrimPrefix(a.baseURL, "https://"), "http://")
	a.signer.Sign(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream error: status=%d body=%s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// Ensure Adapter implements video.VideoProvider and video.AssetProvider
var _ video.VideoProvider = (*Adapter)(nil)
var _ video.AssetProvider = (*Adapter)(nil)
