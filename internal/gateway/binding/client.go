package binding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/httpclient"
	"golang.org/x/sync/singleflight"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	longClient *http.Client
	token      string
	cache      KeyGroupCache    // 进程内 Key-Group 绑定缓存，TTL 60s
	sf         singleflight.Group // 防缓存穿透：同一 keyID 同时只有一个飞行中 HTTP 请求
}

func NewClient(baseURL, token string) *Client {
	// 使用项目标准 HTTP Client（MaxIdleConnsPerHost=100），替代裸 http.Client
	shortCfg := httpclient.DefaultConfig()
	shortCfg.Timeout = 500 * time.Millisecond
	shortCfg.ResponseHeaderTimeout = 500 * time.Millisecond

	longCfg := httpclient.DefaultConfig()
	longCfg.Timeout = 5 * time.Second
	longCfg.ResponseHeaderTimeout = 5 * time.Second

	return &Client{
		baseURL:    baseURL,
		httpClient: httpclient.NewHTTPClient(&shortCfg),
		longClient: httpclient.NewHTTPClient(&longCfg),
		token:      token,
	}
}

// Cache 返回内部 KeyGroupCache，供外部注册事件失效回调。
func (c *Client) Cache() *KeyGroupCache {
	return &c.cache
}

type BoundGroup struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// GetKeyGroups 返回 keyID 绑定的模型组列表。
// 命中本地缓存（TTL 60s）直接返回，否则通过 singleflight 合并并发请求，
// 仅发起一次 HTTP 调用到控制面并回填缓存。
func (c *Client) GetKeyGroups(ctx context.Context, keyID string) ([]BoundGroup, error) {
	// 1. 本地缓存命中，纳秒级返回
	if groups, ok := c.cache.Get(keyID); ok {
		return groups, nil
	}

	// 2. 缓存未命中：用 singleflight 合并同一 keyID 的并发穿透
	val, err, _ := c.sf.Do(keyID, func() (any, error) {
		// double-check：等待期间可能已被其他 goroutine 填充
		if groups, ok := c.cache.Get(keyID); ok {
			return groups, nil
		}
		groups, err := c.fetchKeyGroupsHTTP(ctx, keyID)
		if err != nil {
			return nil, err
		}
		c.cache.Set(keyID, groups)
		return groups, nil
	})
	if err != nil {
		return nil, err
	}
	return val.([]BoundGroup), nil
}

// fetchKeyGroupsHTTP 向控制面发起实际 HTTP 请求。
func (c *Client) fetchKeyGroupsHTTP(ctx context.Context, keyID string) ([]BoundGroup, error) {
	url := fmt.Sprintf("%s/internal/v1/keys/%s/groups", c.baseURL, keyID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Internal-Token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call control-plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("control-plane returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var groups []BoundGroup
	if err := json.Unmarshal(body, &groups); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return groups, nil
}

func ContainsGroup(groups []BoundGroup, groupName string) bool {
	for _, g := range groups {
		if g.Name == groupName && g.Enabled {
			return true
		}
	}
	return false
}

type allGroupItem struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// GroupModelInfo 模型组聚合能力信息（与 control-plane service.GroupModelInfo 对应）。
type GroupModelInfo struct {
	Mode                    string `json:"mode"`
	MaxInputTokens          int    `json:"max_input_tokens"`
	MaxOutputTokens         int    `json:"max_output_tokens"`
	SupportsVision          bool   `json:"supports_vision"`
	SupportsFunctionCalling bool   `json:"supports_function_calling"`
	SupportsToolChoice      bool   `json:"supports_tool_choice"`
}

// GetGroupModelInfo 从 control-plane 拉取指定模型组名的聚合能力信息。
// names 为空时返回所有启用组。
func (c *Client) GetGroupModelInfo(ctx context.Context, names []string) (map[string]GroupModelInfo, error) {
	u := fmt.Sprintf("%s/internal/v1/groups/model-info", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	q := req.URL.Query()
	for _, n := range names {
		if n != "" {
			q.Add("names", n)
		}
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-Internal-Token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.longClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call control-plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("control-plane returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Data map[string]GroupModelInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result.Data, nil
}

// ListAllGroups 从 control-plane 拉取所有启用的模型组名（供万能模型组使用）
func (c *Client) ListAllGroups(ctx context.Context) ([]string, error) {
	url := fmt.Sprintf("%s/api/v1/internal/groups", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Auth-Token", c.token)

	resp, err := c.longClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call control-plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("control-plane returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Data []allGroupItem `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	names := make([]string, 0, len(result.Data))
	for _, g := range result.Data {
		if g.Enabled && g.Name != "" {
			names = append(names, g.Name)
		}
	}
	return names, nil
}
