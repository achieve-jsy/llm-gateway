// Package coldstart 是 T5「独立验证（verifier）」的判据集合，只做验证与取证，
// 不实现任何生产逻辑，也不修改 T1–T4 的文件。
//
// 复核四个方面：
//
//	V1 冷启动窗口：真实 GroupNameCache + 真实 scheduler + 「控制面尚未监听」的假控制面，
//	   在同一个 fixture 内断言「未就绪 → 503 group_cache_not_ready」「已就绪但名字不在名单 → 403
//	   direct_model_not_allowed」「名字在名单 → 正常路由成功」。
//	V2 Redis 预热链路（退化验证：main.go 的 preheatGroupNamesFromSnapshot 不可导出）。
//	V3 快速重试语义：烘焙/预热成功不得让刷新循环直接进入 30s。
//	V4 支持证据：输出真实 /ready 载荷（含/不含就绪的 group_cache），供部署门禁 jq/python
//	   判别力取证复用（脚本 filter 抽取到 /tmp 后对这两份载荷运行）。
package coldstart

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	gatewaymodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

const (
	testInternalToken = "coldstart-internal-token"

	// groupName 是 2026-10-16 线上事故中被误判为直连的合法模型组名。
	groupName = "gpt-5.6-terra"
	// otherGroupName 是另一个合法组名，用于构造「缓存就绪但名字确实不在名单里」。
	otherGroupName = "gpt-5.6-luna"

	groupsEndpoint = "/api/v1/internal/groups"
)

// ---------------------------------------------------------------- 假控制面载荷

type cpModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ModelType    string `json:"model_type"`
	ProviderType string `json:"provider_type"`
	ProviderName string `json:"provider_name"`
	Endpoint     string `json:"endpoint"`
	Priority     int    `json:"priority"`
	Weight       int    `json:"weight"`
}

type cpGroup struct {
	ID             uint      `json:"id"`
	Name           string    `json:"name"`
	Strategy       string    `json:"strategy"`
	Enabled        bool      `json:"enabled"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	Models         []cpModel `json:"models"`
}

// groupsPayload 构造控制面 /api/v1/internal/groups 的真实响应形状：
// GroupNameCache.Refresh 只读 data[].name；真实 scheduler 的 ConfigSyncer 解析同一份 data。
func groupsPayload(names ...string) []byte {
	data := make([]cpGroup, 0, len(names))
	for i, n := range names {
		data = append(data, cpGroup{
			ID:             uint(i + 1),
			Name:           n,
			Strategy:       string(scheduler.StrategyPriority),
			Enabled:        true,
			TimeoutSeconds: 60,
			Models: []cpModel{{
				ID:           fmt.Sprintf("uuid-%d", i+1),
				Name:         n + "-upstream",
				ModelType:    "openai",
				ProviderType: "openai",
				ProviderName: "provider-1",
				Endpoint:     "http://127.0.0.1:1/v1",
				Priority:     1,
				Weight:       1,
			}},
		})
	}
	payload, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		panic(err)
	}
	return payload
}

// ------------------------------------------------- 可启停假控制面（V1 冷启动窗口）

// restartableControlPlane 先只预留一个 TCP 地址、不监听：等价于蓝绿发布时控制面尚未 Listen，
// 网关侧表现为 connection refused。Start 之后在同一地址上返回控制面形状的组配置。
type restartableControlPlane struct {
	addr string

	mu      sync.Mutex
	payload []byte
	srv     *http.Server
}

func newRestartableControlPlane(t *testing.T) *restartableControlPlane {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	cp := &restartableControlPlane{addr: addr}
	t.Cleanup(cp.Stop)
	return cp
}

func (cp *restartableControlPlane) URL() string { return "http://" + cp.addr }

func (cp *restartableControlPlane) Start(t *testing.T, names ...string) {
	t.Helper()
	payload := groupsPayload(names...)

	ln, err := net.Listen("tcp", cp.addr)
	require.NoError(t, err, "在 %s 重新监听失败（端口可能被抢占）", cp.addr)

	mux := http.NewServeMux()
	mux.HandleFunc(groupsEndpoint, func(w http.ResponseWriter, r *http.Request) {
		cp.mu.Lock()
		body := cp.payload
		cp.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	srv := &http.Server{Handler: mux}

	cp.mu.Lock()
	cp.payload = payload
	cp.srv = srv
	cp.mu.Unlock()

	go func() { _ = srv.Serve(ln) }()

	require.Eventually(t, func() bool {
		resp, err := http.Get(cp.URL() + groupsEndpoint)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	}, 3*time.Second, 10*time.Millisecond, "假控制面应在 %s 上就绪", cp.addr)
}

func (cp *restartableControlPlane) Stop() {
	cp.mu.Lock()
	srv := cp.srv
	cp.srv = nil
	cp.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// ------------------------------------------------ 可切换健康状态的假控制面（V3）

// flakyControlPlane 始终监听：failing=true 时返回 500（可观测到请求到达），
// failing=false 时返回组配置。用于观测 RefreshLoop 真实的请求节奏。
type flakyControlPlane struct {
	srv      *httptest.Server
	failing  atomic.Bool
	requests atomic.Int64
	payload  []byte
}

func newFlakyControlPlane(names ...string) *flakyControlPlane {
	f := &flakyControlPlane{payload: groupsPayload(names...)}
	mux := http.NewServeMux()
	mux.HandleFunc(groupsEndpoint, f.handle)
	f.srv = httptest.NewServer(mux)
	return f
}

func (f *flakyControlPlane) handle(w http.ResponseWriter, _ *http.Request) {
	f.requests.Add(1)
	if f.failing.Load() {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(f.payload)
}

// ------------------------------------------------------------------- 测试替身

// recordingTierResolver 是 budget.TierResolver 的独立 fake（不复用 tests/gateway/orchestrator
// 的私有符号），用于证明「拒绝发生在档位解析之前」。
type recordingTierResolver struct {
	mu    sync.Mutex
	calls int
}

func (r *recordingTierResolver) Resolve(groupName string) (budget.TierContext, bool) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return budget.TierContext{TierID: "tier-1", TierName: "T1", GroupName: groupName}, true
}

func (r *recordingTierResolver) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type staticModelsRouter struct {
	resp *core.ModelsResponse
	err  error
}

func (f *staticModelsRouter) ListModels(_ context.Context) (*core.ModelsResponse, error) {
	return f.resp, f.err
}

// dispatchContext 构造一个只带 identity 的 gin.Context（与网关调度入口等价的最小上下文）。
func dispatchContext(identity *gatewaymodel.UserIdentity) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if identity != nil {
		c.Set("identity", identity)
	}
	return c
}

// ------------------------------------------------------------------------- V1

// TestV1_ColdStartWindow_NotReady503ThenDirect403ThenRouted 在同一个 fixture 里复现
// 2026-10-16 蓝绿发布的冷启动窗口，并用「只改缓存就绪状态」的对照证明 126 笔 403 与修复后
// 503 的唯一差异就是缓存是否就绪。
func TestV1_ColdStartWindow_NotReady503ThenDirect403ThenRouted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// ① 控制面尚未监听：等价线上 14:32:33 green 网关启动窗口。
	cp := newRestartableControlPlane(t) // 已预留地址但未监听 → connection refused
	nc := cache.NewGroupNameCache(testInternalToken)

	// 真实启动重试路径（main.go:232 为 5 次 × 2s，这里只缩短间隔以便测试）全部失败。
	err := nc.RefreshWithRetry(ctx, cp.URL(), 5, 20*time.Millisecond)
	require.Error(t, err, "控制面不可用期间启动重试必须全部失败")
	require.False(t, nc.IsInitialized(), "冷启动窗口内组名缓存必须处于未就绪状态")
	require.False(t, nc.IsGroupName(groupName))
	require.Empty(t, ncSnapshotNames(t, nc), "冷启动窗口内组名集合应为空")

	// 真实 scheduler（与控制面同源地址）：此刻同样拉不到配置。
	schCfg := scheduler.DefaultConfig(cp.URL())
	schCfg.InternalToken = testInternalToken
	schCfg.ConfigSyncInterval = 50 * time.Millisecond
	sch, err := scheduler.New(schCfg)
	require.NoError(t, err)
	sch.Start(ctx)
	defer sch.Shutdown()

	resolver := &recordingTierResolver{}
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, resolver)
	identity := &gatewaymodel.UserIdentity{
		KeyID:           "key-1",
		AccessAllGroups: true,
		UsableGroups:    []string{groupName, otherGroupName},
	}

	// ② 未就绪 + tierResolver != nil + 合法组名 → 必须 503 group_cache_not_ready。
	decision, err := dispatcher.Dispatch(dispatchContext(identity), groupName, nil)
	require.Nil(t, decision)
	require.ErrorIs(t, err, gatewaymodel.ErrGroupCacheNotReady)
	unified, ok := err.(*gatewaymodel.UnifiedError)
	require.True(t, ok, "错误必须是 UnifiedError，实际 %T", err)
	require.Equal(t, http.StatusServiceUnavailable, unified.HTTPStatus)
	require.Equal(t, "group_cache_not_ready", unified.Code)
	require.Zero(t, resolver.Calls(), "未就绪判定必须先于档位解析")

	// ③ 对照：仅把缓存变成「已就绪但该名字确实不在名单里」→ 403 direct_model_not_allowed。
	//    同一个 fixture、同一个 model、同一个 identity、同一个 tierResolver，唯一差异是缓存就绪。
	nc.SetNames([]string{otherGroupName})
	require.True(t, nc.IsInitialized())
	require.False(t, nc.IsGroupName(groupName))

	decision, err = dispatcher.Dispatch(dispatchContext(identity), groupName, nil)
	require.Nil(t, decision)
	require.ErrorIs(t, err, gatewaymodel.ErrDirectModelNotAllowed)
	unified, ok = err.(*gatewaymodel.UnifiedError)
	require.True(t, ok, "错误必须是 UnifiedError，实际 %T", err)
	require.Equal(t, http.StatusForbidden, unified.HTTPStatus)
	require.Equal(t, "direct_model_not_allowed", unified.Code)
	require.Zero(t, resolver.Calls(), "拒绝发生在档位解析之前")

	// ④ 控制面恢复 → 真实 scheduler 同步到组配置、真实缓存刷新到位 → 组名正常路由。
	cp.Start(t, groupName, otherGroupName)
	require.Eventually(t, func() bool {
		d, routeErr := sch.Route(groupName, nil)
		return routeErr == nil && d != nil
	}, 5*time.Second, 20*time.Millisecond, "真实 scheduler 应从同一控制面同步到组配置")

	require.NoError(t, nc.Refresh(ctx, cp.URL()), "控制面恢复后真实刷新路径必须成功")
	require.True(t, nc.IsInitialized())
	require.True(t, nc.IsGroupName(groupName))

	routeCtx := dispatchContext(identity)
	decision, err = dispatcher.Dispatch(routeCtx, groupName, nil)
	require.NoError(t, err)
	require.NotNil(t, decision)
	require.Equal(t, groupName, decision.GroupName)
	require.Equal(t, groupName+"-upstream", decision.ModelName)
	require.Equal(t, 1, resolver.Calls(), "组名路径应解析一次档位")

	tier, hasTier := budget.GetTierContext(routeCtx)
	require.True(t, hasTier, "组名路径必须写入档位上下文")
	require.Equal(t, "tier-1", tier.TierID)

	routedViaGroup, exists := routeCtx.Get("routed_via_group")
	require.True(t, exists)
	require.Equal(t, true, routedViaGroup)
}

// fakeChatOrchestrator 记录是否被调用：拒绝路径必须在触达上游之前返回。
type fakeChatOrchestrator struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeChatOrchestrator) Chat(_ context.Context, _ *core.ChatRequest) (*core.ChatResponse, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return &core.ChatResponse{ID: "resp-1", Object: "chat.completion", Model: groupName}, nil
}

func (f *fakeChatOrchestrator) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeChatOrchestrator) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestV1b_HTTPLevel_503Then403 把 V1 的判据抬到真实 HTTP 层：
// 真实 ChatHandler（NewChatHandlerWithGroup）+ 真实 GroupAwareDispatcher + 真实 GroupNameCache，
// 确认错误码最终落到 HTTP 503 group_cache_not_ready / 403 direct_model_not_allowed，
// 且两次拒绝都没有触达上游 orchestrator。
func TestV1b_HTTPLevel_503Then403(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	cp := newRestartableControlPlane(t) // 控制面未监听 → 缓存无法就绪
	nc := cache.NewGroupNameCache(testInternalToken)
	require.Error(t, nc.RefreshWithRetry(ctx, cp.URL(), 2, 20*time.Millisecond))
	require.False(t, nc.IsInitialized())

	sch, err := scheduler.New(scheduler.DefaultConfig(cp.URL())) // 未 Start：拒绝路径不会触达它
	require.NoError(t, err)
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, &recordingTierResolver{})

	orch := &fakeChatOrchestrator{}
	chatHandler := handler.NewChatHandlerWithGroup(orch, dispatcher)
	engine := gin.New()
	engine.POST("/v1/chat/completions", chatHandler.Handle)

	post := func() (int, map[string]any) {
		body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, groupName)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, req)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
		return w.Code, payload
	}

	// 冷启动窗口：HTTP 503 group_cache_not_ready（可重试，不是 403）。
	status, payload := post()
	require.Equal(t, http.StatusServiceUnavailable, status)
	errObj, ok := payload["error"].(map[string]any)
	require.True(t, ok, "响应体必须含 error 对象，实际 %v", payload)
	require.Equal(t, "group_cache_not_ready", errObj["code"])

	// 仅把缓存置为「已就绪但名字不在名单」→ HTTP 403 direct_model_not_allowed。
	nc.SetNames([]string{otherGroupName})
	status, payload = post()
	require.Equal(t, http.StatusForbidden, status)
	errObj, ok = payload["error"].(map[string]any)
	require.True(t, ok, "响应体必须含 error 对象，实际 %v", payload)
	require.Equal(t, "direct_model_not_allowed", errObj["code"])

	require.Zero(t, orch.Calls(), "两次拒绝都必须在触达上游 orchestrator 之前返回")
}

// ncSnapshotNames 只通过公开 API 观察缓存内容（IsGroupName 名单探测）。
func ncSnapshotNames(t *testing.T, nc *cache.GroupNameCache) []string {
	t.Helper()
	var found []string
	for _, n := range []string{groupName, otherGroupName} {
		if nc.IsGroupName(n) {
			found = append(found, n)
		}
	}
	return found
}

// ------------------------------------------------------------------------- V2

// TestV2_RedisSnapshotPreheat_Degraded 验证「Redis 里有含 group_names 的 provider snapshot →
// 组名缓存可被预热为就绪」。
//
// 退化说明：main.go 的 preheatGroupNamesFromSnapshot 属于 package main 且不可导出，本判据文件
// 按任务约束只能放在 tests/gateway/coldstart，无法直接调用它；因此这里逐字执行它内部的两步
// （configsync.LoadProviderSnapshot → GroupNameCache.SetNames），胶水层由代码阅读证据补齐
// （cmd/gateway/main.go:231 与 535-557）。
func TestV2_RedisSnapshotPreheat_Degraded(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()

	// ① Redis 无快照：LoadProviderSnapshot 返回 redis.Nil（main.go:542 视为正常，跳过预热）。
	_, err := configsync.LoadProviderSnapshot(ctx, rdb)
	require.ErrorIs(t, err, redis.Nil)

	nc := cache.NewGroupNameCache(testInternalToken)
	require.False(t, nc.IsInitialized(), "无快照时预热不得把缓存置为就绪")

	// ② 控制面下发快照（含 group_names）：写入与预热读取必须是同一个 Redis key。
	require.NoError(t, configsync.SaveProviderSnapshot(ctx, rdb, &configsync.ProviderSnapshot{
		Version:    1,
		UpdatedAt:  time.Now(),
		GroupNames: []string{groupName, otherGroupName},
	}))
	snap, err := configsync.LoadProviderSnapshot(ctx, rdb)
	require.NoError(t, err, "写入的快照必须能被读回（同一个 key）")
	require.Equal(t, []string{groupName, otherGroupName}, snap.GroupNames, "读回的组名集合必须与写入一致")

	// ③ main.go:555 的等价链路
	nc.SetNames(snap.GroupNames)
	require.True(t, nc.IsInitialized(), "预热后缓存必须就绪")
	require.True(t, nc.IsGroupName(groupName))
	require.True(t, nc.IsGroupName(otherGroupName))

	// ④ 预热后的可观测差异：同一 model 进入组路由分支，而不是 503/403。
	//    这里 scheduler 未 Start（路由表为空），因此组分支的最终错误是 ErrNoAvailableModel。
	sch, err := scheduler.New(scheduler.DefaultConfig("http://127.0.0.1:1"))
	require.NoError(t, err)
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, &recordingTierResolver{})

	decision, err := dispatcher.Dispatch(dispatchContext(nil), groupName, nil)
	require.Nil(t, decision)
	require.ErrorIs(t, err, gatewaymodel.ErrNoAvailableModel, "预热后必须按组名进入组路由")
	require.NotErrorIs(t, err, gatewaymodel.ErrGroupCacheNotReady)
	require.NotErrorIs(t, err, gatewaymodel.ErrDirectModelNotAllowed)
}

// ------------------------------------------------------------------------- V3

// TestV3_RefreshLoopUsesFastRetryUntilFirstControlPlaneSuccess 用生产默认间隔（2s/30s）
// 证明：Redis 预热成功（SetNames）不会让刷新循环直接进入 30s——循环只以「真实的控制面刷新成功」
// 作为切换依据；连续失败只按退避（R8：2s→4s→…→30s 上限）拉长间隔，不会退到 steady 之外。
func TestV3_RefreshLoopUsesFastRetryUntilFirstControlPlaneSuccess(t *testing.T) {
	// 生产默认值本身就是门禁语义的一部分（main.go:526-528 直接传这两个常量）。
	require.Equal(t, 2*time.Second, cache.DefaultGroupNameFastRefreshInterval)
	require.Equal(t, 30*time.Second, cache.DefaultGroupNameSteadyRefreshInterval)

	cp := newFlakyControlPlane(otherGroupName)
	defer cp.srv.Close()
	cp.failing.Store(true)

	nc := cache.NewGroupNameCache(testInternalToken)
	// 模拟 Redis 预热成功：缓存已就绪，但控制面仍未确认。
	nc.SetNames([]string{groupName})
	require.True(t, nc.IsInitialized())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		nc.RefreshLoop(ctx, cp.srv.URL,
			cache.DefaultGroupNameFastRefreshInterval,
			cache.DefaultGroupNameSteadyRefreshInterval)
	}()

	// ① 首轮立即执行且失败（控制面返回 500）：预热成功不得让循环直接走 30s。
	require.Eventually(t, func() bool { return cp.requests.Load() >= 1 }, 3*time.Second, 10*time.Millisecond,
		"RefreshLoop 应立即执行首轮刷新")
	require.False(t, nc.IsGroupName(otherGroupName), "首轮失败不得写入控制面内容")

	// ② 控制面恢复 → 必须在快速节奏内（远小于 steady=30s）成功并替换组名。
	//    若预热成功被误当成「控制面已确认」，这里只会在 30s 后有结果，从而超时失败。
	cp.failing.Store(false)
	require.Eventually(t, func() bool { return nc.IsGroupName(otherGroupName) }, 6*time.Second, 20*time.Millisecond,
		"预热成功（SetNames）后仍必须按快速节奏（fast=2s 起步）重试：控制面恢复后 6s 内应成功")

	// ③ 首次成功后必须回落到 30s steady：3s 内不应再有请求。
	afterSuccess := cp.requests.Load()
	time.Sleep(3 * time.Second) // 远小于 steady=30s
	require.Equal(t, afterSuccess, cp.requests.Load(),
		"首次控制面刷新成功后必须回落到 30s steady 间隔，3s 内不应再有请求")

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx 取消后 RefreshLoop 应退出")
	}
}

// ------------------------------------------------------------------------- V4 支持证据

// TestV4Support_ReadyPayloadsForDeployGate 用真实 HealthHandler 产出 /ready 载荷，
// 落盘到临时目录供「抽取 deploy-blue-green.sh 的 jq/python filter」判别力取证复用。
func TestV4Support_ReadyPayloadsForDeployGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	state := &provider.RuntimeState{}
	state.MarkLoadSuccess("coldstart-verifier", 2, 7)
	modelsRouter := &staticModelsRouter{resp: &core.ModelsResponse{
		Object: "list",
		Data: []core.Model{
			{ID: groupName, Object: "model"},
			{ID: otherGroupName, Object: "model"},
		},
	}}

	nc := cache.NewGroupNameCache(testInternalToken)
	h := handler.NewHealthHandler(rdb, modelsRouter, state, nil)
	h.SetGroupNameCache(nc)

	engine := gin.New()
	engine.GET("/ready", h.Ready)

	// 冷启动窗口：组名缓存未就绪 → /ready 必须 503 且 checks.group_cache=not_initialized。
	notReady := serveReady(t, engine)
	require.Equal(t, http.StatusServiceUnavailable, notReady.status)
	require.Equal(t, "not_initialized", notReady.checks["group_cache"])

	// 预热/刷新就绪 → /ready 200 且 checks.group_cache=ok。
	nc.SetNames([]string{groupName, otherGroupName})
	ready := serveReady(t, engine)
	require.Equal(t, http.StatusOK, ready.status)
	require.Equal(t, "ok", ready.checks["group_cache"])
	require.Equal(t, "ready", ready.payload["status"])

	dir := filepath.Join(os.TempDir(), "group-cache-coldstart-evidence")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ready_payload_ok.json"), ready.body, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ready_payload_not_initialized.json"), notReady.body, 0o644))
	t.Logf("真实 /ready 载荷已写入 %s", dir)
}

type readyResult struct {
	status  int
	body    []byte
	payload map[string]any
	checks  map[string]any
}

func serveReady(t *testing.T, engine *gin.Engine) readyResult {
	t.Helper()
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))

	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	checks, _ := payload["checks"].(map[string]any)
	return readyResult{status: w.Code, body: w.Body.Bytes(), payload: payload, checks: checks}
}
