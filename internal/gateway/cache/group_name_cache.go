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

	// DefaultGroupNameFastRefreshInterval 是「首次控制面刷新成功前」的快速重试间隔。
	// 蓝绿发布时网关与控制面同时拉起，控制面可能晚于网关监听，故此时必须快重试。
	DefaultGroupNameFastRefreshInterval = 2 * time.Second
	// DefaultGroupNameSteadyRefreshInterval 是控制面已确认可用后的常规刷新间隔。
	DefaultGroupNameSteadyRefreshInterval = 30 * time.Second
)

// 组名缓存的写入来源（/ready details.group_cache_source）。
// 契约：SetNames / ApplySnapshotGroupNames 一律记为 redis-snapshot（非测试调用方只有
// provider 快照这一条路径）；控制面 HTTP 轮询 Refresh 记为 control-plane。
const (
	GroupNameCacheSourceRedisSnapshot = "redis-snapshot"
	GroupNameCacheSourceControlPlane  = "control-plane"
)

type GroupNameCache struct {
	mu            sync.RWMutex
	names         map[string]struct{}
	source        string
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

// IsInitialized 报告缓存是否已完成至少一次成功加载。
// nil 接收者返回 false：HealthHandler 可能拿到「非 nil 接口包 nil 指针」，不得因此 panic。
func (gc *GroupNameCache) IsInitialized() bool {
	if gc == nil {
		return false
	}
	return gc.initialized.Load()
}

// Len 返回当前缓存的组名数量（供 /ready details.group_cache_count 观测）。nil 接收者返回 0。
func (gc *GroupNameCache) Len() int {
	if gc == nil {
		return 0
	}
	gc.mu.RLock()
	defer gc.mu.RUnlock()
	return len(gc.names)
}

// Source 返回最近一次写入的来源（GroupNameCacheSourceRedisSnapshot /
// GroupNameCacheSourceControlPlane）；从未写入过时返回空串，调用方按「未知」呈现。
// nil 接收者同样返回空串。
func (gc *GroupNameCache) Source() string {
	if gc == nil {
		return ""
	}
	gc.mu.RLock()
	defer gc.mu.RUnlock()
	return gc.source
}

// SetNames 用外部来源整体替换组名集合并标记就绪。
//
// 来源契约：本方法一律记为 GroupNameCacheSourceRedisSnapshot——非测试调用方只有
// 「provider 快照（Redis）」这一条路径（启动预热 + 快照热更新/周期性 reconcile 回流）。
// 控制面 HTTP 轮询必须走 Refresh（记为 GroupNameCacheSourceControlPlane）。
//
// 注意：本方法把空列表也视为合法事实并就地清空（与 Refresh 的控制面语义一致）。
// 读 Redis 快照的调用方不要直接用它：旧控制面不写 group_names 字段时读到的是 nil，
// 无法与「确实没有任何启用组」区分，必须改用 ApplySnapshotGroupNames。
func (gc *GroupNameCache) SetNames(groupNames []string) {
	newNames := make(map[string]struct{}, len(groupNames))
	for _, n := range groupNames {
		if n != "" {
			newNames[n] = struct{}{}
		}
	}
	gc.mu.Lock()
	gc.names = newNames
	gc.source = GroupNameCacheSourceRedisSnapshot
	gc.mu.Unlock()
	gc.initialized.Store(true)
}

// ApplySnapshotGroupNames 用 provider 快照（Redis）携带的启用组名刷新组名缓存，返回是否写入。
//
// 与 SetNames 的唯一区别是空列表语义：快照里的空 group_names 既可能是「控制面确实没有任何
// 启用组」，也可能是「旧控制面不写该字段（omitempty → nil）」，网关侧无法区分，因此
// **空列表必须保持现有缓存不动**，绝不能用空列表覆盖——否则合法组名会被误判为直连模型。
//
// 反向取舍（R3）：控制面 HTTP 轮询 Refresh 拿到 200 + 空 data 是权威事实（全部组被停用），
// 必须照常清空并保持就绪；不要因为「列表为空」就把控制面结果当成未就绪。
// 本方法也不比对快照 Version/UpdatedAt：快照比控制面旧不是拒绝服务的理由（CP 不可达时
// 必须能靠 Redis 快照提供服务），新鲜度只通过 details.group_cache_source 暴露给运维。
func (gc *GroupNameCache) ApplySnapshotGroupNames(groupNames []string, version int64) bool {
	if gc == nil {
		return false
	}
	effective := 0
	for _, n := range groupNames {
		if n != "" {
			effective++
		}
	}
	if effective == 0 {
		return false
	}

	gc.SetNames(groupNames)
	slog.Info("group name cache refreshed from provider snapshot",
		"source", GroupNameCacheSourceRedisSnapshot,
		"count", gc.Len(),
		"version", version,
	)
	return true
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
	gc.source = GroupNameCacheSourceControlPlane
	gc.mu.Unlock()
	gc.initialized.Store(true)
	slog.Info("group name cache refreshed", "source", GroupNameCacheSourceControlPlane, "count", len(newNames))
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

// nextFailureInterval 计算「连续失败 failures 次」后的下一次重试间隔：
// fastInterval 起按 2 倍递增，并以 steadyInterval 为上限（生产默认 2s→4s→8s→16s→30s）。
// 退避的意义：内部 token 不匹配这类永久性错误若仍按 fastInterval 重试，会变成每 2s 一条
// Warn、请求量是基线的约 15 倍。failures <= 0 视为首次失败；非法间隔回落到默认值。
func nextFailureInterval(fastInterval, steadyInterval time.Duration, failures int) time.Duration {
	if fastInterval <= 0 {
		fastInterval = DefaultGroupNameFastRefreshInterval
	}
	if steadyInterval <= 0 {
		steadyInterval = DefaultGroupNameSteadyRefreshInterval
	}
	if failures <= 0 {
		failures = 1
	}
	if fastInterval >= steadyInterval {
		return steadyInterval
	}

	interval := fastInterval
	for i := 1; i < failures; i++ {
		// 先收敛再翻倍：既避免 16s→32s 越过 30s 上限，也避免长失败序列上的溢出。
		if interval > steadyInterval/2 {
			return steadyInterval
		}
		interval *= 2
	}
	if interval > steadyInterval {
		return steadyInterval
	}
	return interval
}

// RefreshLoop 在后台刷新组名缓存，节奏为「先快后慢」：
// 首次「控制面刷新成功」之前按 fastInterval 起步快速重试，连续失败按 2 倍退避并以
// steadyInterval 封顶；成功一次后立即回落到 steadyInterval。
//
// 注意语义：Redis 预热（SetNames）不等于控制面已确认，因此本循环只以真实的刷新成功
// 作为切换依据——这样即使 Redis 快照陈旧，控制面就绪后也能被纠正。
// 每轮刷新失败都打 Warn（附带连续失败次数与下一次间隔），且失败不会清空既有组名与
// initialized 状态（由 Refresh 保证）。
// 循环可被 ctx.Done() 中断。
func (gc *GroupNameCache) RefreshLoop(ctx context.Context, controlPlaneURL string, fastInterval, steadyInterval time.Duration) {
	if fastInterval <= 0 {
		fastInterval = DefaultGroupNameFastRefreshInterval
	}
	if steadyInterval <= 0 {
		steadyInterval = DefaultGroupNameSteadyRefreshInterval
	}

	// 首轮立即执行，不等待
	wait := time.Duration(0)
	confirmed := false
	failures := 0

	for {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}

		if err := gc.Refresh(ctx, controlPlaneURL); err != nil {
			// 控制面尚未确认可用：连续失败按指数退避（fast → 2×fast → … → steady 上限）。
			failures++
			wait = nextFailureInterval(fastInterval, steadyInterval, failures)
			slog.Warn("group name cache refresh failed",
				"consecutive_failures", failures, "next_interval", wait, "error", err)
			continue
		}

		failures = 0
		wait = steadyInterval
		if !confirmed {
			confirmed = true
			slog.Info("group name cache first control-plane refresh succeeded, switching to steady interval",
				"interval", steadyInterval)
		}
	}
}
