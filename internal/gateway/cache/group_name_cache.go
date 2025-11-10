package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	groupNameRefreshTimeout = 10 * time.Second
	groupNameEndpoint       = "/api/v1/internal/groups"
	groupNameAuthToken      = "pre-shared-token-placeholder"
)

type GroupNameCache struct {
	mu    sync.RWMutex
	names map[string]struct{}
}

func NewGroupNameCache() *GroupNameCache {
	return &GroupNameCache{
		names: make(map[string]struct{}),
	}
}

func (gc *GroupNameCache) IsGroupName(name string) bool {
	gc.mu.RLock()
	_, ok := gc.names[name]
	gc.mu.RUnlock()
	return ok
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
	req.Header.Set("X-Auth-Token", groupNameAuthToken)

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
		Groups []struct {
			GroupName string `json:"group_name"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}

	newNames := make(map[string]struct{}, len(cfg.Groups))
	for _, g := range cfg.Groups {
		if g.GroupName != "" {
			newNames[g.GroupName] = struct{}{}
		}
	}

	gc.mu.Lock()
	gc.names = newNames
	gc.mu.Unlock()

	return nil
}
