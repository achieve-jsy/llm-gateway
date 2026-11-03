package binding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	// keyGroupsCacheTTL 是 key→groups 绑定的本地缓存 TTL（30 秒）。
	// 足够短以保证 binding 变更能在半分钟内生效，又足够长以消除 >99% 的 HTTP 调用。
	keyGroupsCacheTTL = 30 * time.Second
	// keyGroupsCacheCleanup 是过期缓存条目的后台清理间隔。
	keyGroupsCacheCleanup = 1 * time.Minute
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	longClient *http.Client
	token      string

	groupsCache sync.Map
	closeCh     chan struct{}
	closeOnce   sync.Once
}

type cachedGroupsEntry struct {
	data   []BoundGroup
	expiry time.Time
}

func NewClient(baseURL, token string) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 500 * time.Millisecond,
		},
		longClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		token:   token,
		closeCh: make(chan struct{}),
	}
	go c.cleanupLoop()
	return c
}

// Close 停止后台清理协程。
func (c *Client) Close() {
	c.closeOnce.Do(func() { close(c.closeCh) })
}

func (c *Client) cleanupLoop() {
	ticker := time.NewTicker(keyGroupsCacheCleanup)
	defer ticker.Stop()
	for {
		select {
		case <-c.closeCh:
			return
		case <-ticker.C:
			now := time.Now()
			c.groupsCache.Range(func(key, value any) bool {
				e := value.(*cachedGroupsEntry)
				if now.After(e.expiry) {
					c.groupsCache.Delete(key)
				}
				return true
			})
		}
	}
}

type BoundGroup struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (c *Client) GetKeyGroups(ctx context.Context, keyID string) ([]BoundGroup, error) {
	if keyID == "" {
		return nil, nil
	}

	if entry, ok := c.groupsCache.Load(keyID); ok {
		e := entry.(*cachedGroupsEntry)
		if time.Now().Before(e.expiry) {
			return e.data, nil
		}
		c.groupsCache.Delete(keyID)
	}

	groups, err := c.getKeyGroupsHTTP(ctx, keyID)
	if err != nil {
		return nil, err
	}

	c.groupsCache.Store(keyID, &cachedGroupsEntry{
		data:   groups,
		expiry: time.Now().Add(keyGroupsCacheTTL),
	})

	return groups, nil
}

func (c *Client) getKeyGroupsHTTP(ctx context.Context, keyID string) ([]BoundGroup, error) {
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
	slog.Debug("binding: fetched key groups from control-plane", "key_id", keyID, "count", len(groups))
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
