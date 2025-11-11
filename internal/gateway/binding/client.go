package binding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	longClient *http.Client
	token      string
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 500 * time.Millisecond,
		},
		longClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		token: token,
	}
}

type BoundGroup struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (c *Client) GetKeyGroups(ctx context.Context, keyID string) ([]BoundGroup, error) {
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