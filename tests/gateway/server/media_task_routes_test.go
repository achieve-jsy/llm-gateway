package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/gateway/server"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 本文件验证 routes_media.go 的 C 队接缝（C4/C5）：
//   - registerMediaTaskRoutes 真的把三个端点挂在 /v1 上（且与既有 Seedance 路由并存）；
//   - 未接线的降级：store 缺失 → 503、适配器未注册 → 501；
//   - 适配器整表热更新（factory + ReloadMediaAdapters + Swap）不是增量 merge；
//   - 走**真实** infra.VideoTaskClient + 真实 orchestrator.GroupAwareDispatcher 的端到端链路。

const (
	routesGroup    = "kling-group"
	routesUpstream = "kling-v1-6"
	routesChannel  = "kling"
	routesKeyID    = "key-1"
	routesTaskID   = "task_e2e"
)

// ─────────────────────────────────────────────────────────────────────────────
// fake 控制面（实现冻结的内部 API）
// ─────────────────────────────────────────────────────────────────────────────

type fakeControlPlane struct {
	mu     sync.Mutex
	tasks  map[string]*infra.MediaTask
	create int
	get    int
	patch  int
}

func newFakeControlPlane() *fakeControlPlane {
	return &fakeControlPlane{tasks: map[string]*infra.MediaTask{}}
}

func (f *fakeControlPlane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Internal-Token") == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/video/tasks")
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.Method == http.MethodPost && path == "":
		f.create++
		raw, _ := io.ReadAll(r.Body)
		var req infra.MediaCreateRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad body"}`))
			return
		}
		if req.IdempotencyKey != "" {
			for _, existing := range f.tasks {
				if existing.APIKeyID == req.APIKeyID {
					// 幂等命中：200 + 既有任务。
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(mustJSON(map[string]any{"task": existing}))
					return
				}
			}
		}
		task := &infra.MediaTask{
			TaskID:       req.TaskID,
			Kind:         req.Kind,
			Action:       req.Action,
			Status:       req.Status,
			VendorTaskID: req.VendorTaskID,
			Model:        req.Model,
			ProviderName: req.ProviderName,
			ProviderType: req.ProviderType,
			APIKeyID:     req.APIKeyID,
			UserID:       req.UserID,
			DeptID:       req.DeptID,
			StudioID:     req.StudioID,
			PrivateData:  infra.JSONText(string(req.PrivateData)),
			CreatedAt:    time.Now().UTC(),
		}
		if task.Status == "" {
			task.Status = "queued"
		}
		f.tasks[task.TaskID] = task
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(mustJSON(map[string]any{"task": task}))

	case r.Method == http.MethodGet:
		f.get++
		task, ok := f.tasks[strings.TrimPrefix(path, "/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		_, _ = w.Write(mustJSON(map[string]any{"task": task}))

	case r.Method == http.MethodPatch:
		f.patch++
		task, ok := f.tasks[strings.TrimPrefix(path, "/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req infra.MediaUpdateRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.ExpectedStatus != "" && req.ExpectedStatus != task.Status {
			_, _ = w.Write(mustJSON(map[string]any{"updated": false, "task": task}))
			return
		}
		if req.Status != "" {
			task.Status = req.Status
		}
		if req.VendorTaskID != "" {
			task.VendorTaskID = req.VendorTaskID
		}
		if req.VideoURL != "" {
			task.VideoURL = req.VideoURL
		}
		if req.Progress != "" {
			task.Progress = req.Progress
		}
		if len(req.Usage) != 0 {
			task.Usage = infra.JSONText(string(req.Usage))
		}
		if req.CostMicroUSD != nil {
			task.CostMicroUSD = *req.CostMicroUSD
		}
		_, _ = w.Write(mustJSON(map[string]any{"updated": true, "task": task}))

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

// ─────────────────────────────────────────────────────────────────────────────
// fake 调度器 / 适配器
// ─────────────────────────────────────────────────────────────────────────────

type fakeScheduler struct {
	decision *scheduler.RouteDecision
}

func (f *fakeScheduler) Route(_ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.decision, nil
}

func (f *fakeScheduler) Failover(_, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.decision, nil
}

func (f *fakeScheduler) FailoverExcluding(_ string, _ []string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.decision, nil
}

func (f *fakeScheduler) GetGroupConfig(_ string) *scheduler.GroupRoutingConfig { return nil }
func (f *fakeScheduler) NotifyFailure(_ string)                                {}
func (f *fakeScheduler) SetCooldown(_ string, _ time.Duration)                 {}
func (f *fakeScheduler) PersistSticky(_ context.Context, _, _ string, _ *scheduler.RouteDecision) {
}
func (f *fakeScheduler) UpdateSticky(_ context.Context, _, _, _, _ string) bool { return false }

type fakeAdapter struct {
	mu          sync.Mutex
	submitCalls int
	queryCalls  int
}

func (f *fakeAdapter) Submit(_ context.Context, _ *core.MediaSubmitRequest) (*core.MediaSubmitResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitCalls++
	return &core.MediaSubmitResult{VendorTaskID: "vendor-e2e", Status: core.MediaStatusQueued}, nil
}

func (f *fakeAdapter) Query(_ context.Context, _ string) (*core.MediaTaskStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryCalls++
	return &core.MediaTaskStatus{
		Status:    core.MediaStatusCompleted,
		Progress:  "100",
		Artifacts: []core.MediaArtifact{{Kind: core.MediaKindVideo, URL: "https://cdn.example.com/e2e.mp4"}},
		Usage:     map[string]any{"duration": float64(4)},
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 装配
// ─────────────────────────────────────────────────────────────────────────────

type routesRig struct {
	router *gin.Engine
	cp     *fakeControlPlane
	group  *gin.RouterGroup
}

func newRoutesRig(t *testing.T, controlPlaneURL string) *routesRig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	middleware.ConfigureMediaPricingGate(middleware.MediaPricingGateConfig{
		RequirePricing: true,
		Resolver: gwobs.ProviderPricingResolverFunc(func(modelKey, _ string) (core.ModelPricing, bool) {
			perSecond := 0.5
			return core.ModelPricing{PerSecondOutput: &perSecond}, modelKey != ""
		}),
	})
	server.ConfigureMediaTaskPolicy(server.MediaTaskPolicy{
		TimeoutMinutes:  30,
		PollMaxFailures: 20,
		// 与准入闸门同源价表：终态记账按每秒 0.5 美元。
		PricingResolver: gwobs.ProviderPricingResolverFunc(func(modelKey, _ string) (core.ModelPricing, bool) {
			perSecond := 0.5
			return core.ModelPricing{PerSecondOutput: &perSecond}, modelKey != ""
		}),
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("key_id", routesKeyID)
		c.Set("user_id", "user-1")
		c.Set("key_type", "default")
		c.Set("dept_id", "dept-1")
		c.Set("studio_id", "studio-1")
		c.Next()
	})
	v1 := router.Group("/v1")

	names := cache.NewGroupNameCache("")
	names.SetNames([]string{routesGroup})
	dispatcher := orchestrator.NewGroupAwareDispatcher(&fakeScheduler{decision: &scheduler.RouteDecision{
		ModelID:      "1",
		ModelUUID:    "uuid-1",
		ModelName:    routesUpstream,
		ChannelType:  routesChannel,
		ProviderName: "Kling",
	}}, names)

	var cp *fakeControlPlane
	if controlPlaneURL == "" {
		cp = newFakeControlPlane()
	}
	server.RegisterMediaTaskRoutes(server.MediaDeps{
		V1:                   v1,
		GroupDispatch:        dispatcher,
		ControlPlaneURL:      controlPlaneURL,
		InternalToken:        "internal-token",
		MediaAdapterRegistry: server.MediaAdapterRegistry(),
	})
	return &routesRig{router: router, cp: cp, group: v1}
}

func newFakeControlPlaneServer(t *testing.T) (*fakeControlPlane, string) {
	t.Helper()
	cp := newFakeControlPlane()
	srv := httptest.NewServer(cp)
	t.Cleanup(srv.Close)
	return cp, srv.URL
}

func do(t *testing.T, router *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// ─────────────────────────────────────────────────────────────────────────────
// 测试
// ─────────────────────────────────────────────────────────────────────────────

func TestMediaTaskRoutes_RegisteredAndDegradeWithoutStore(t *testing.T) {
	rig := newRoutesRig(t, "") // 未配置控制面地址 → store 为 nil

	post := do(t, rig.router, http.MethodPost, "/v1/videos", `{"model":"kling-group","prompt":"p"}`, nil)
	require.Equal(t, http.StatusServiceUnavailable, post.Code, post.Body.String())

	get := do(t, rig.router, http.MethodGet, "/v1/videos/task_x", "", nil)
	require.Equal(t, http.StatusServiceUnavailable, get.Code, get.Body.String())

	content := do(t, rig.router, http.MethodGet, "/v1/videos/task_x/content", "", nil)
	require.Equal(t, http.StatusServiceUnavailable, content.Code, content.Body.String())
}

func TestMediaTaskRoutes_NilV1DoesNotPanic(t *testing.T) {
	require.NotPanics(t, func() {
		server.RegisterMediaTaskRoutes(server.MediaDeps{})
	})
}

func TestMediaTaskRoutes_UnwiredAdapterIs501(t *testing.T) {
	// 注册表为空（D 队未接线）→ 501，且**不落库、不打上游**。
	cp, url := newFakeControlPlaneServer(t)
	rig := newRoutesRig(t, url)
	server.ReloadMediaAdapters(nil) // 清空（factory 未注册时为整表替换成空）
	t.Cleanup(func() { server.ReloadMediaAdapters(nil) })

	rec := do(t, rig.router, http.MethodPost, "/v1/videos", `{"model":"kling-group","prompt":"p"}`, nil)
	require.Equal(t, http.StatusNotImplemented, rec.Code, rec.Body.String())
	require.Zero(t, cp.create, "501 之前不得落库")
}

func TestMediaTaskRoutes_EndToEndCreateRetrieveContent(t *testing.T) {
	cp, url := newFakeControlPlaneServer(t)
	rig := newRoutesRig(t, url)

	adapter := &fakeAdapter{}
	server.RegisterMediaAdapterFactory(func(_ []gwmodel.ProviderEntry) (map[string]core.MediaTaskProvider, map[string]string) {
		return map[string]core.MediaTaskProvider{routesChannel: adapter}, map[string]string{routesChannel: "Kling"}
	})
	t.Cleanup(func() {
		server.RegisterMediaAdapterFactory(nil)
		server.ReloadMediaAdapters(nil)
	})
	server.ReloadMediaAdapters(nil)
	require.Equal(t, 1, server.MediaAdapterRegistry().Len())

	// ── 创建 ────────────────────────────────────────────────────────────────
	rec := do(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"a cat","duration":5}`, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var created map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	taskID, _ := created["id"].(string)
	require.NotEmpty(t, taskID)
	require.Equal(t, "queued", created["status"])
	require.Equal(t, routesGroup, created["model"])
	require.Equal(t, 1, adapter.submitCalls)

	// 控制面真的收到了 vendor id（说明「同步落库」发生了）。
	stored := cp.tasks[taskID]
	require.NotNil(t, stored)
	require.Equal(t, "vendor-e2e", stored.VendorTaskID)
	require.Equal(t, "kling", stored.ProviderType)
	require.Equal(t, "key-1", stored.APIKeyID)
	require.Equal(t, "dept-1", stored.DeptID)

	// ── 查询（上游已完成 → CAS 推进 + 记账）────────────────────────────────
	get := do(t, rig.router, http.MethodGet, "/v1/videos/"+taskID, "", nil)
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	var fetched map[string]any
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &fetched))
	require.Equal(t, "completed", fetched["status"])
	require.Equal(t, 1, adapter.queryCalls)
	require.EqualValues(t, 2000000, cp.tasks[taskID].CostMicroUSD, "4s × 0.5 USD/s = 2 USD = 2_000_000 微美元")

	// 重复查询：终态缓存，不再打上游。
	again := do(t, rig.router, http.MethodGet, "/v1/videos/"+taskID, "", nil)
	require.Equal(t, http.StatusOK, again.Code)
	require.Equal(t, 1, adapter.queryCalls, "终态不再打上游")

	// ── 产物（Phase 1：302 直跳）────────────────────────────────────────────
	content := do(t, rig.router, http.MethodGet, "/v1/videos/"+taskID+"/content", "", nil)
	require.Equal(t, http.StatusFound, content.Code, content.Body.String())
	require.Equal(t, "https://cdn.example.com/e2e.mp4", content.Header().Get("Location"))
}

func TestMediaTaskRoutes_IdempotencyKeyHitsExistingTask(t *testing.T) {
	cp, url := newFakeControlPlaneServer(t)
	rig := newRoutesRig(t, url)
	adapter := &fakeAdapter{}
	server.RegisterMediaAdapterFactory(func(_ []gwmodel.ProviderEntry) (map[string]core.MediaTaskProvider, map[string]string) {
		return map[string]core.MediaTaskProvider{routesChannel: adapter}, nil
	})
	t.Cleanup(func() {
		server.RegisterMediaAdapterFactory(nil)
		server.ReloadMediaAdapters(nil)
	})
	server.ReloadMediaAdapters(nil)

	first := do(t, rig.router, http.MethodPost, "/v1/videos", `{"model":"kling-group","prompt":"p"}`,
		map[string]string{"Idempotency-Key": "idem-e2e"})
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.Equal(t, 1, adapter.submitCalls)
	require.Equal(t, 1, cp.create)

	second := do(t, rig.router, http.MethodPost, "/v1/videos", `{"model":"kling-group","prompt":"p"}`,
		map[string]string{"Idempotency-Key": "idem-e2e"})
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.Equal(t, 1, adapter.submitCalls, "幂等命中绝不能再次提交上游")
	require.Equal(t, 2, cp.create, "第二次仍是 POST（由控制面判定命中）")

	var firstBody, secondBody map[string]any
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstBody))
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &secondBody))
	require.Equal(t, firstBody["id"], secondBody["id"], "两次必须返回同一个任务 id")
}

func TestMediaTaskRoutes_DirectModelIDRejected(t *testing.T) {
	_, url := newFakeControlPlaneServer(t)
	rig := newRoutesRig(t, url)
	// "kling-v1-6" 不在 GroupNameCache 里 → 组调度返回 (nil, nil) → 403。
	rec := do(t, rig.router, http.MethodPost, "/v1/videos", `{"model":"kling-v1-6","prompt":"p"}`, nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	errObj, _ := body["error"].(map[string]any)
	require.Equal(t, "direct_model_not_allowed", errObj["code"])
}

func TestMediaTaskRoutes_CrossRefIs404(t *testing.T) {
	cp, url := newFakeControlPlaneServer(t)
	rig := newRoutesRig(t, url)
	cp.tasks["task_someone_else"] = &infra.MediaTask{
		TaskID: "task_someone_else", Status: "completed", APIKeyID: "other-key",
		VideoURL: "https://cdn.example.com/other.mp4", CreatedAt: time.Now().UTC(),
	}
	rec := do(t, rig.router, http.MethodGet, "/v1/videos/task_someone_else", "", nil)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestMediaTaskRoutes_CoexistWithSeedanceTaskRoutes(t *testing.T) {
	_, url := newFakeControlPlaneServer(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/v1")
	// 先注册既有 Seedance 端点（真实路径形状），再注册任务平台端点。
	v1.GET("/contents/generations/tasks/:task_id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"legacy": c.Param("task_id")})
	})
	require.NotPanics(t, func() {
		server.RegisterMediaTaskRoutes(server.MediaDeps{V1: v1, ControlPlaneURL: url, InternalToken: "t"})
	})

	legacy := do(t, router, http.MethodGet, "/v1/contents/generations/tasks/t1", "", nil)
	require.Equal(t, http.StatusOK, legacy.Code)
	require.Contains(t, legacy.Body.String(), "legacy")

	// 新链路仍然可达（未被 Seedance 路由抢走）。
	missing := do(t, router, http.MethodGet, "/v1/videos/t1", "", nil)
	require.Equal(t, http.StatusNotFound, missing.Code, missing.Body.String())
}

// ─────────────────────────────────────────────────────────────────────────────
// C4：适配器整表热更新
// ─────────────────────────────────────────────────────────────────────────────

func TestMediaAdapterReload_IsWholeTableSwap(t *testing.T) {
	registry := server.MediaAdapterRegistry()

	server.RegisterMediaAdapterFactory(func(_ []gwmodel.ProviderEntry) (map[string]core.MediaTaskProvider, map[string]string) {
		return map[string]core.MediaTaskProvider{
			"kling": &fakeAdapter{},
			"vidu":  &fakeAdapter{},
		}, map[string]string{"kling": "Kling", "vidu": "Vidu"}
	})
	t.Cleanup(func() {
		server.RegisterMediaAdapterFactory(nil)
		server.ReloadMediaAdapters(nil)
	})

	server.ReloadMediaAdapters(nil)
	require.Equal(t, 2, registry.Len())
	require.ElementsMatch(t, []string{"kling", "vidu"}, registry.ProviderTypes())
	require.Equal(t, "Kling", registry.DisplayName("kling"))
	require.NotNil(t, registry.Get("vidu"))

	// 渠道被删除：整表替换后必须消失（绝不能增量 merge 留下旧凭证的适配器）。
	server.RegisterMediaAdapterFactory(func(_ []gwmodel.ProviderEntry) (map[string]core.MediaTaskProvider, map[string]string) {
		return map[string]core.MediaTaskProvider{"vidu": &fakeAdapter{}}, nil
	})
	server.ReloadMediaAdapters(nil)
	require.Equal(t, 1, registry.Len())
	require.Nil(t, registry.Get("kling"), "下线的渠道必须从注册表消失")
	require.NotNil(t, registry.Get("vidu"))

	// 工厂返回 nil（没有任何可用媒体渠道）→ 清空注册表，Get 返回 nil（调用方按 501 处理）。
	server.RegisterMediaAdapterFactory(func(_ []gwmodel.ProviderEntry) (map[string]core.MediaTaskProvider, map[string]string) {
		return nil, nil
	})
	server.ReloadMediaAdapters(nil)
	require.Zero(t, registry.Len())
	require.Nil(t, registry.Get("vidu"))
}

func TestMediaAdapterReload_WithoutFactoryKeepsRegistryEmpty(t *testing.T) {
	server.RegisterMediaAdapterFactory(nil)
	server.ReloadMediaAdapters(nil)
	require.Zero(t, server.MediaAdapterRegistry().Len())
}

// TestMediaTaskPolicy_DefaultsMatchConfig 固化策略兜底：未注入时 30 分钟 / 20 次。
func TestMediaTaskPolicy_DefaultsMatchConfig(t *testing.T) {
	defaults := server.MediaTaskPolicyDefaults()
	require.Equal(t, 30, defaults.TimeoutMinutes)
	require.Equal(t, 20, defaults.PollMaxFailures)
}
