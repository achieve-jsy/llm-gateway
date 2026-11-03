package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 本文件是媒体任务平台（C5/C6/C7/C8）组件测试的公共夹具。
//
// 约定（tests/README.md）：手写 fake，不引入 mock 框架；gin 用 TestMode；
// 控制面用 fake HTTP server（tests/gateway/infra 里的客户端契约测试）或 store fake 替代。

// ─────────────────────────────────────────────────────────────────────────────
// fake store：模拟控制面内部 API 的任务表语义（含 CAS）
// ─────────────────────────────────────────────────────────────────────────────

type recordedCreate struct {
	req infra.MediaCreateRequest
}

type recordedUpdate struct {
	taskID string
	req    infra.MediaUpdateRequest
}

type fakeMediaStore struct {
	mu sync.Mutex

	tasks       map[string]*infra.MediaTask
	creates     []recordedCreate
	updates     []recordedUpdate
	getCalls    int
	createCalls int

	// 控制返回行为
	createErr    error
	getErr       error
	updateErr    error
	idempotentOn bool // true：CreateMediaTask 一律返回 200 + 既有任务（模拟幂等命中）
	createNoTask bool // true：Create 返回 Created=true 但 Task=nil
	updateNoTask bool // true：Update 返回 Updated=true 但 Task=nil

	// 强制 Updated=false（模拟 CAS 未命中）
	forceCASMiss bool
}

func newFakeMediaStore() *fakeMediaStore {
	return &fakeMediaStore{tasks: map[string]*infra.MediaTask{}}
}

func (s *fakeMediaStore) seed(task *infra.MediaTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.TaskID] = task
}

func (s *fakeMediaStore) task(taskID string) *infra.MediaTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tasks[taskID]
}

func (s *fakeMediaStore) lastCreate(t *testing.T) recordedCreate {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.creates, "expected at least one CreateMediaTask call")
	return s.creates[len(s.creates)-1]
}

func (s *fakeMediaStore) updateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.updates)
}

func (s *fakeMediaStore) lastUpdate(t *testing.T) recordedUpdate {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.updates, "expected at least one UpdateMediaTask call")
	return s.updates[len(s.updates)-1]
}

func (s *fakeMediaStore) CreateMediaTask(_ context.Context, req infra.MediaCreateRequest) (*infra.MediaCreateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	s.creates = append(s.creates, recordedCreate{req: req})
	if s.createErr != nil {
		return nil, s.createErr
	}
	if s.idempotentOn {
		if existing := s.firstTask(); existing != nil {
			return &infra.MediaCreateResult{StatusCode: http.StatusOK, Created: false, Task: existing}, nil
		}
	}
	task := &infra.MediaTask{
		TaskID:       req.TaskID,
		Kind:         req.Kind,
		Action:       req.Action,
		Status:       req.Status,
		Progress:     req.Progress,
		VendorTaskID: req.VendorTaskID,
		Model:        req.Model,
		ProviderName: req.ProviderName,
		ProviderType: req.ProviderType,
		APIKeyID:     req.APIKeyID,
		APIKeyName:   req.APIKeyName,
		APIKeyType:   req.APIKeyType,
		UserID:       req.UserID,
		UserName:     req.UserName,
		DeptID:       req.DeptID,
		StudioID:     req.StudioID,
		RequestBody:  req.RequestBody,
		PrivateData:  infra.JSONText(string(req.PrivateData)),
		CreatedAt:    time.Now().UTC(),
	}
	if task.Status == "" {
		task.Status = string(core.MediaStatusQueued)
	}
	s.tasks[task.TaskID] = task
	if s.createNoTask {
		return &infra.MediaCreateResult{StatusCode: http.StatusCreated, Created: true, Task: nil}, nil
	}
	return &infra.MediaCreateResult{StatusCode: http.StatusCreated, Created: true, Task: cloneTask(task)}, nil
}

func (s *fakeMediaStore) firstTask() *infra.MediaTask {
	for _, task := range s.tasks {
		return cloneTask(task)
	}
	return nil
}

func (s *fakeMediaStore) GetMediaTask(_ context.Context, taskID string) (*infra.MediaTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls++
	if s.getErr != nil {
		return nil, s.getErr
	}
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, infra.ErrMediaTaskNotFound
	}
	return cloneTask(task), nil
}

func (s *fakeMediaStore) UpdateMediaTask(_ context.Context, taskID string, req infra.MediaUpdateRequest) (*infra.MediaUpdateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, recordedUpdate{taskID: taskID, req: req})
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, infra.ErrMediaTaskNotFound
	}
	// CAS 语义：expected_status 非空且与当前状态不等 → updated=false，不改任何字段。
	if req.ExpectedStatus != "" && req.ExpectedStatus != task.Status {
		return &infra.MediaUpdateResult{Updated: false, Task: cloneTask(task)}, nil
	}
	if s.forceCASMiss {
		return &infra.MediaUpdateResult{Updated: false, Task: cloneTask(task)}, nil
	}
	if req.Status != "" {
		task.Status = req.Status
	}
	if req.Progress != "" {
		task.Progress = req.Progress
	}
	if req.VendorTaskID != "" {
		task.VendorTaskID = req.VendorTaskID
	}
	if req.VideoURL != "" {
		task.VideoURL = req.VideoURL
	}
	if req.ResponseBody != "" {
		task.ResponseBody = req.ResponseBody
	}
	if req.ErrorMessage != "" {
		task.ErrorMessage = req.ErrorMessage
	}
	if len(req.Usage) != 0 {
		task.Usage = infra.JSONText(string(req.Usage))
	}
	if req.CostMicroUSD != nil {
		task.CostMicroUSD = *req.CostMicroUSD
	}
	if req.BilledAt != "" {
		if parsed, err := time.Parse(time.RFC3339, req.BilledAt); err == nil {
			task.BilledAt = &parsed
		}
	}
	if req.PollFailures != nil {
		task.PollFailures = *req.PollFailures
	}
	// 镜像控制面的 finished_at 语义（internal/controlplane/repository/video_task_repo.go：
	// 「首次进入任一终态 → finished_at」）。网关侧 completed_at（OpenAI video 形状）
	// 就是从这里读的，fake 不实现这一条会让 completed_at 永远测不出来。
	if req.Status != "" && mediaFakeTerminalStatus(req.Status) && task.FinishedAt == nil {
		finishedAt := time.Now().UTC()
		task.FinishedAt = &finishedAt
	}
	task.UpdatedAt = time.Now().UTC()
	if s.updateNoTask {
		return &infra.MediaUpdateResult{Updated: true, Task: nil}, nil
	}
	return &infra.MediaUpdateResult{Updated: true, Task: cloneTask(task)}, nil
}

// mediaFakeTerminalStatus 等价于控制面 model.IsMediaTaskTerminalStatus
// （completed | failed | expired，internal/controlplane/model/video_task.go）。
func mediaFakeTerminalStatus(status string) bool {
	switch status {
	case "completed", "failed", "expired":
		return true
	default:
		return false
	}
}

// cloneTask 返回深拷贝，避免 handler 与 fake 共享同一指针掩盖「未写库却生效」的错误。
func cloneTask(task *infra.MediaTask) *infra.MediaTask {
	if task == nil {
		return nil
	}
	out := *task
	return &out
}

// ─────────────────────────────────────────────────────────────────────────────
// fake 适配器 / 调度器
// ─────────────────────────────────────────────────────────────────────────────

type fakeMediaAdapter struct {
	submitResult *core.MediaSubmitResult
	submitErr    error
	queryStatus  *core.MediaTaskStatus
	queryErr     error

	submitCalls   int
	queryCalls    int
	lastSubmit    *core.MediaSubmitRequest
	lastQueryID   string
	submitNoRetry bool
}

func (f *fakeMediaAdapter) Submit(ctx context.Context, req *core.MediaSubmitRequest) (*core.MediaSubmitResult, error) {
	f.submitCalls++
	f.lastSubmit = req
	if llmclient.IsNoRetry(ctx) {
		f.submitNoRetry = true
	}
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	return f.submitResult, nil
}

func (f *fakeMediaAdapter) Query(_ context.Context, vendorTaskID string) (*core.MediaTaskStatus, error) {
	f.queryCalls++
	f.lastQueryID = vendorTaskID
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.queryStatus, nil
}

type fakeGroupDispatcher struct {
	decision *scheduler.RouteDecision
	err      error
	calls    int
}

func (f *fakeGroupDispatcher) Dispatch(_ *gin.Context, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.decision, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 通用装配
// ─────────────────────────────────────────────────────────────────────────────

const (
	testMediaGroup        = "kling-group"
	testMediaUpstream     = "kling-v1-6"
	testMediaProviderType = "kling"
	testMediaKeyID        = "key-owner"
	testMediaUserID       = "user-owner"
)

// testMediaPricing 返回一份可用的视频价（每秒 0.1 美元）。
func testMediaPricing() core.ModelPricing {
	perSecond := 0.1
	perImage := 0.04
	return core.ModelPricing{PerSecondOutput: &perSecond, PerImage: &perImage}
}

// allowMediaPricing 关闭无价拒绝（价表可用但不会命中），保证各测试只需关注自身路径。
func allowMediaPricing(t *testing.T) {
	t.Helper()
	middleware.ConfigureMediaPricingGate(middleware.MediaPricingGateConfig{
		RequirePricing: true,
		Resolver: gwobs.ProviderPricingResolverFunc(func(modelKey, _ string) (core.ModelPricing, bool) {
			return testMediaPricing(), modelKey != ""
		}),
	})
}

// rejectMediaPricing 让准入闸门判定「无价」，用于验证 402。
func rejectMediaPricing(t *testing.T) {
	t.Helper()
	middleware.ConfigureMediaPricingGate(middleware.MediaPricingGateConfig{
		RequirePricing: true,
		Resolver: gwobs.PricingResolverFunc(func(string) (core.ModelPricing, bool) {
			return core.ModelPricing{}, false
		}),
	})
}

type mediaTestRig struct {
	store      *fakeMediaStore
	adapter    *fakeMediaAdapter
	dispatcher *fakeGroupDispatcher
	registry   *providers.MediaAdapterRegistry
	handler    *handler.MediaTaskHandler
	router     *gin.Engine
	capture    *ctxCapture
}

// ctxCapture 在 handler 之后回读 gin context，用于断言埋点上下文
// （媒体用量必须写进 CtxKeyMediaUsage，且必须是 core.MediaCostInput 值类型）。
type ctxCapture struct {
	mu     sync.Mutex
	values map[string]any
}

func (c *ctxCapture) record(gc *gin.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values = map[string]any{}
	for _, key := range []string{"model", "provider", "routed_model", gwobs.CtxKeyMediaUsage} {
		if value, ok := gc.Get(key); ok {
			c.values[key] = value
		}
	}
}

func (c *ctxCapture) str(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, _ := c.values[key].(string)
	return s
}

// mediaUsage 返回媒体用量；第二返回值为 false 表示「本次没有写用量」（即不记账）。
func (c *ctxCapture) mediaUsage() (core.MediaCostInput, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.values[gwobs.CtxKeyMediaUsage]
	if !ok {
		return core.MediaCostInput{}, false
	}
	// 精确类型断言：采集器就是这么读的（collector.go），别名/指针都会读不到。
	usage, ok := value.(core.MediaCostInput)
	return usage, ok
}

// newMediaTestRig 装配一个「一个渠道 + 一个适配器」的最小链路。
func newMediaTestRig(t *testing.T, adapter *fakeMediaAdapter, mutate func(*handler.MediaTaskHandlerConfig)) *mediaTestRig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	allowMediaPricing(t)

	if adapter == nil {
		adapter = &fakeMediaAdapter{
			submitResult: &core.MediaSubmitResult{VendorTaskID: "vendor-1", Status: core.MediaStatusQueued},
		}
	}
	store := newFakeMediaStore()
	registry := providers.NewMediaAdapterRegistry()
	registry.Swap(map[string]core.MediaTaskProvider{testMediaProviderType: adapter}, nil)

	cfg := handler.MediaTaskHandlerConfig{
		Store:    store,
		Registry: registry,
		GroupDispatch: &fakeGroupDispatcher{decision: &scheduler.RouteDecision{
			ModelName:    testMediaUpstream,
			ModelUUID:    "uuid-1",
			ChannelType:  testMediaProviderType,
			ProviderName: "Kling",
		}},
		Pricing: gwobs.ProviderPricingResolverFunc(func(modelKey, _ string) (core.ModelPricing, bool) {
			return testMediaPricing(), modelKey != ""
		}),
		Timeout:         30 * time.Minute,
		MaxPollFailures: 3,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	dispatcher, _ := cfg.GroupDispatch.(*fakeGroupDispatcher)
	h := handler.NewMediaTaskHandler(cfg)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("key_id", testMediaKeyID)
		c.Set("user_id", testMediaUserID)
		c.Set("key_type", "default")
		c.Set("key_name", "owner-key")
		c.Set("user_name", "owner")
		c.Next()
	})
	capture := &ctxCapture{}
	router.Use(func(c *gin.Context) {
		c.Next()
		capture.record(c)
	})
	router.POST("/v1/videos", h.Create)
	router.GET("/v1/videos/:task_id", h.Retrieve)
	router.GET("/v1/videos/:task_id/content", h.Content)

	return &mediaTestRig{store: store, adapter: adapter, dispatcher: dispatcher, registry: registry, handler: h, router: router, capture: capture}
}

// doJSON 发一个带 JSON body 的请求。
func doJSON(t *testing.T, router *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
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

// decodeJSON 解析响应体。
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), "body=%s", rec.Body.String())
	return out
}

// errorCode 取 {error:{code}} 里的 code。
func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	body := decodeJSON(t, rec)
	errObj, ok := body["error"].(map[string]any)
	require.True(t, ok, "响应缺少 error 信封：%s", rec.Body.String())
	code, _ := errObj["code"].(string)
	return code
}

// seedMediaTask 投放一条任务并补全默认字段（默认属于 testMediaKeyID）。
func (r *mediaTestRig) seedMediaTask(task *infra.MediaTask) *infra.MediaTask {
	if task.APIKeyID == "" {
		task.APIKeyID = testMediaKeyID
	}
	if task.Kind == "" {
		task.Kind = "video"
	}
	if task.ProviderType == "" {
		task.ProviderType = testMediaProviderType
	}
	if task.ProviderName == "" {
		task.ProviderName = "Kling"
	}
	if task.Model == "" {
		task.Model = testMediaGroup
	}
	if task.Status == "" {
		task.Status = string(core.MediaStatusQueued)
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	if task.PrivateData == "" {
		raw, _ := json.Marshal(map[string]any{
			"kind":           "video",
			"action":         "text_to_video",
			"upstream_model": testMediaUpstream,
			"provider_name":  "Kling",
			"provider_type":  testMediaProviderType,
		})
		task.PrivateData = infra.JSONText(raw)
	}
	r.store.seed(task)
	return task
}

// newQueuedTask 造一条「已提交上游、处理中」的任务。
func newQueuedTask(taskID string) *infra.MediaTask {
	return &infra.MediaTask{
		TaskID:       taskID,
		VendorTaskID: "vendor-" + taskID,
		Status:       string(core.MediaStatusQueued),
		APIKeyID:     testMediaKeyID,
		UserID:       testMediaUserID,
		CreatedAt:    time.Now().UTC(),
	}
}

func (r *mediaTestRig) retrieve(t *testing.T, taskID string) *httptest.ResponseRecorder {
	t.Helper()
	return doJSON(t, r.router, http.MethodGet, "/v1/videos/"+taskID, "", nil)
}

func (r *mediaTestRig) content(t *testing.T, taskID string) *httptest.ResponseRecorder {
	t.Helper()
	return doJSON(t, r.router, http.MethodGet, "/v1/videos/"+taskID+"/content", "", nil)
}
