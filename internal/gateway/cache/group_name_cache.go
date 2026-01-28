package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	groupNameRefreshTimeout = 10 * time.Second
	groupNameEndpoint       = "/api/v1/internal/groups"
)

type GroupNameCache struct {
	mu            sync.RWMutex
	names         map[string]struct{}
	internalToken string
	initialized   atomic.Bool
}

func NewGroupNameCache(internalToken string) *GroupNameCache {
	return &GroupNameCache{
		names:         make(map[string]struct{}),
		internalToken: internalToken,
	}
}

func (gc *GroupNameCache) IsGroupName(name string) bool {
	gc.mu.RLock()
	_, ok := gc.names[name]
	gc.mu.RUnlock()
	return ok
}

func (gc *GroupNameCache) IsInitialized() bool {
	return gc.initialized.Load()
}

func (gc *GroupNameCache) SetNames(groupNames []string) {
	newNames := make(map[string]struct{}, len(groupNames))
	for _, n := range groupNames {
		if n != "" {
			newNames[n] = struct{}{}
		}
	}
	gc.mu.Lock()
	gc.names = newNames
	gc.mu.Unlock()
	gc.initialized.Store(true)
}

func (gc *GroupNameCache) Refresh(ctx context.Context, controlPlaneURL string) error {
	cpBaseURL := strings.TrimRight(controlPlaneURL, "/")
	url := cpBaseURL + groupNameEndpoint

	reqCtx, cancel := context.WithTimeout(ctx, groupNameRefreshTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Auth-Token", gc.internalToken)

	client := &http.Client{Timeout: groupNameRefreshTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	var cfg struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}

	newNames := make(map[string]struct{}, len(cfg.Data))
	for _, g := range cfg.Data {
		if g.Name != "" {
			newNames[g.Name] = struct{}{}
		}
	}

	gc.mu.Lock()
	gc.names = newNames
	gc.mu.Unlock()
	gc.initialized.Store(true)
	slog.Info("group name cache refreshed", "count", len(newNames))
	return nil
}

// RefreshWithRetry 在启动阶段阻塞调用，确保首次成功刷新（最多尝试 maxRetries 次，
// 每次间隔 retryInterval），避免 GroupNameCache 尚未就绪时就让请求走 direct 路径误返回 404。
// 调用方应在启动 HTTP 服务之前等待本方法返回。
func (gc *GroupNameCache) RefreshWithRetry(ctx context.Context, controlPlaneURL string, maxRetries int, retryInterval time.Duration) error {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := gc.Refresh(ctx, controlPlaneURL); err != nil {
			lastErr = err
			slog.Warn("group name cache startup refresh failed",
				"attempt", attempt, "max", maxRetries, "error", err)
			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(retryInterval):
				}
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("group name cache failed to initialize after %d attempts: %w", maxRetries, lastErr)
}
