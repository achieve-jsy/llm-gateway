package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/gin-gonic/gin"
)

// 本文件实现「媒体任务平台」的对外三件套（C5/C6/C7/C8）：
//
//	POST /v1/videos                   创建任务（组调度 → 准入门禁 → 适配器提交 → **同步**落库）
//	GET  /v1/videos/:task_id          查询任务（归属校验 → 终态缓存 → 查上游 → CAS → 记账）
//	GET  /v1/videos/:task_id/content  产物（Phase 1：302 直跳上游）
//
// 设计依据：docs/media-channels-integration-design.md §2（架构）、§6.3（路由与流程）、
// §6.4（状态机）、§8（计费）；对外契约 docs/media-api.md；内部契约见
// internal/gateway/infra/video_task_client.go 的文件头（冻结，与 C1 队同一份）。
//
// 与既有 Seedance 链路（video_handler.go + /v1/contents/generations/tasks）的关系：
// **并存、互不顶替**。既有链路是「纯直通上游 JSON」，本链路是「翻译 + 归一 + 归属 + 计费」。
// 本文件刻意不复用 VideoHandler 的任何状态：GetTask 无归属校验是设计文档 §1.3 记录的
// 既有缺陷，不扩散到新链路（tests/gateway/handler/media_task_idor_test.go 有对照测试）。

const (
	// maxMediaTaskBodyBytes 创建请求体的读取上限。
	maxMediaTaskBodyBytes = 64 * 1024
	// mediaTaskObjectValue 对外 object 字段（OpenAI 形状）。
	mediaTaskObjectValue = "video"
	// mediaProgressZero 刚受理时写入 media_tasks.progress 的**存储层**值（varchar 列，
	// 见 infra.MediaCreateRequest.Progress 注释：「0-100」或阶段名）。
	// 注意这是存储层表示，与对外响应里的 progress（整数）不是同一个东西。
	mediaProgressZero = "0"
	// mediaStoredStatusExpired 是控制面的存储层状态；对外必须归一为 failed（不记账）。
	mediaStoredStatusExpired = "expired"
)

// 对外 progress 的展示档位（E4b 定稿）。
//
// 为什么需要这张表：对外 progress 对齐 OpenAI video 对象，是 **0-100 的整数**
// （New API 的权威形状 dto/openai_video.go: `Progress int` + SetProgressStr 把上游字符串
// 转成整数）；而 media_tasks.progress 是 varchar，上游可能给百分比数字、也可能只给阶段名
// （Kling 只有 submitted/processing/succeed/failed，不报百分比）。
//
// 取值原则（绝不谎报）：
//   - 能解析成数字 → 用数字（clamp 到 0-100）；
//   - 阶段名 → 下面这张**显式**映射表；
//   - 解析不出/未知 → 0（明确的下界，而不是 100 或 99）；
//   - 状态已是 completed → 强制 100（与 status 自洽，避免出现「completed + progress 0」）。
//
// 这套数字是**展示约定**，不是实测百分比：网关只知道粗粒度阶段（进度只有上游知道，
// 而上游的百分比口径各不相同）。因此文档与前端都不应把它当作精确进度。
const (
	// mediaProgressUnknown 未知（空值 / 无法解析 / 失败终态）：取区间下界。
	mediaProgressUnknown = 0
	// mediaProgressQueueing 已进入上游队列、尚未开始生成。
	mediaProgressQueueing = 10
	// mediaProgressRunning 上游已开始生成但未回报百分比：取「进行中」的中位展示值。
	mediaProgressRunning = 50
	// mediaProgressDone 已完成。
	mediaProgressDone = 100
)

// mediaProgressStageValues 是「存储层阶段名 → 对外整数进度」的显式映射表。
//
// 词表与 mapStoredStatus 的归一表**刻意同源**：状态词能认出来的，进度也一定认得出
// （新增状态词时必须同时补这张表，否则会静默退化为 0）。
// 映射为 0 的失败类词不是「进度归零」的意思，而是「失败时刻的实际进度不可知，
// 取下界」——配合 status=failed 一起读才是完整语义。
var mediaProgressStageValues = map[string]int{
	// 已受理、上游尚未开始
	"queued": mediaProgressUnknown, "submitted": mediaProgressUnknown,
	"created": mediaProgressUnknown, "pending": mediaProgressUnknown,
	"not_start": mediaProgressUnknown, "waiting": mediaProgressUnknown,
	// 上游排队中（已进入上游，但还没开始算）
	"queueing": mediaProgressQueueing,
	// 生成中
	"in_progress": mediaProgressRunning, "processing": mediaProgressRunning,
	"running": mediaProgressRunning, "started": mediaProgressRunning,
	"generating": mediaProgressRunning,
	// 终态成功
	"completed": mediaProgressDone, "succeed": mediaProgressDone,
	"succeeded": mediaProgressDone, "success": mediaProgressDone,
	"finished": mediaProgressDone, "done": mediaProgressDone,
	// 终态失败（进度不可知 → 下界）
	"failed": mediaProgressUnknown, "failure": mediaProgressUnknown,
	"error": mediaProgressUnknown, "expired": mediaProgressUnknown,
	"cancelled": mediaProgressUnknown, "canceled": mediaProgressUnknown,
	"rejected": mediaProgressUnknown,
}

// 对外错误码（冻结在 docs/media-api.md §6 与 tests/contract/contracts.json 的
// media_channels.error_codes；**不得新造 code**，前端与对接方按 code 分支）。
const (
	mediaErrInvalidRequest        = "invalid_request_error"
	mediaErrDirectModelNotAllowed = "direct_model_not_allowed"
	mediaErrNotFound              = "not_found_error"
	mediaErrNotImplemented        = "not_implemented"
	mediaErrUpstream              = "upstream_error"
	mediaErrUnavailable           = "service_unavailable"
)

// ─────────────────────────────────────────────────────────────────────────────
// 依赖与构造
// ─────────────────────────────────────────────────────────────────────────────

// MediaTaskStore 是任务持久化依赖（生产实现是 infra.VideoTaskClient，测试用 fake）。
//
// gateway **不直连数据库**（防护栏 A1）：任务读写一律经控制面内部 API。
type MediaTaskStore interface {
	CreateMediaTask(ctx context.Context, req infra.MediaCreateRequest) (*infra.MediaCreateResult, error)
	GetMediaTask(ctx context.Context, taskID string) (*infra.MediaTask, error)
	UpdateMediaTask(ctx context.Context, taskID string, req infra.MediaUpdateRequest) (*infra.MediaUpdateResult, error)
}

// MediaTaskHandlerConfig 汇总任务平台链路所需的依赖。
//
// 全部字段都可缺省：Store 为 nil 时端点返回 503；Registry 未接线（Get 返回 nil）时
// 返回 501；GroupDispatch 为 nil 或模型不是组名时返回 403（只接受模型组名）。
type MediaTaskHandlerConfig struct {
	Store         MediaTaskStore
	Registry      *providers.MediaAdapterRegistry
	GroupDispatch VideoGroupDispatcher
	// RouterGetter 仅用于「组调度没给渠道类型」时的兜底解析（可为 nil）。
	RouterGetter func() *providers.Router

	BudgetGate     *budget.Gate
	RequestQuota   RequestQuotaGate
	ModelQuotaGate *quota.ModelQuotaGate

	// Pricing 与准入闸门同源的媒体价表（C8 终态记账用）。
	// 为 nil 时记不上成本，但状态推进照常，且会明确 usage_missing（绝不静默记 0 元）。
	Pricing gwobs.PricingResolver

	// Timeout 任务超时阈值（MEDIA_TASK_TIMEOUT_MINUTES，默认 30 分钟）。
	Timeout time.Duration
	// MaxPollFailures 轮询失败上限（MEDIA_POLL_MAX_FAILURES，默认 20）。
	MaxPollFailures int

	// Now / NewTaskID 便于测试注入（为 nil 时用 time.Now / crypto/rand）。
	Now       func() time.Time
	NewTaskID func() (string, error)
}

// MediaTaskHandler 媒体任务平台三件套的 handler。
type MediaTaskHandler struct {
	cfg MediaTaskHandlerConfig
}

// NewMediaTaskHandler 构造 handler（缺省值在这里兜底，保证零配置也安全）。
func NewMediaTaskHandler(cfg MediaTaskHandlerConfig) *MediaTaskHandler {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Minute
	}
	if cfg.MaxPollFailures <= 0 {
		cfg.MaxPollFailures = 20
	}
	return &MediaTaskHandler{cfg: cfg}
}

func (h *MediaTaskHandler) now() time.Time {
	if h != nil && h.cfg.Now != nil {
		return h.cfg.Now()
	}
	return time.Now()
}

func (h *MediaTaskHandler) newTaskID() (string, error) {
	if h != nil && h.cfg.NewTaskID != nil {
		return h.cfg.NewTaskID()
	}
	return generateMediaTaskID()
}

// generateMediaTaskID 生成对外任务 id（128 位随机）。
//
// 契约 3（core/media_task.go）：对外 id 与上游 id 严格分离——**禁止**把 vendor id
// 直接返回给客户端（否则泄露渠道细节，且上游轮询地址可被枚举）。
func generateMediaTaskID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "task_" + hex.EncodeToString(buf), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 对外响应/错误形状
// ─────────────────────────────────────────────────────────────────────────────

// mediaTaskResponse 是 /v1/videos 的响应（OpenAI video 对象形状，E4b 对齐）。
//
// 形状依据：D1 决策选型理由就是「对齐 OpenAI 生态，客户端 SDK 可直接用」，权威形状取自
// New API 的 dto/openai_video.go（它就是照着 OpenAI 实现的）：
//
//	id / object / model / status / progress(int) / created_at / completed_at /
//	expires_at / seconds / size / error
//
// 本结构**刻意不实现**的三个字段（OpenAI 有、本网关无对应语义，宁可不发也不编造）：
//   - task_id：OpenAI 侧是待废弃的旧 id 别名，本网关对外 id 只有一个（id）；
//   - remixed_from_video_id：本网关没有 remix 能力；
//   - metadata：当前没有任何需要下发的自定义元数据。
//
// 字段集合冻结在 tests/contract/contracts.json 的 media_channels.video_response_fields。
type mediaTaskResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Status    string `json:"status"`
	Model     string `json:"model"`
	CreatedAt int64  `json:"created_at"`
	// CompletedAt 完成时间（Unix 秒）；非终态省略。取自 media_tasks.finished_at
	// （控制面在**首次**进入终态时写入，见 internal/controlplane/repository/video_task_repo.go）。
	CompletedAt int64 `json:"completed_at,omitempty"`
	// Progress 是 0-100 的**整数**（OpenAI 形状；不是字符串，也没有 omitempty——
	// SDK 按必填字段读）。归一规则见 mediaProgressStageValues。
	Progress int `json:"progress"`
	// Seconds 时长（秒的十进制字符串，OpenAI 形状如 "5"）。
	// 优先取上游实际用量，退化取请求参数 duration；都取不到则省略（不编造）。
	Seconds string `json:"seconds,omitempty"`
	// Size 产物尺寸（如 "1280x720"）。只在请求参数里**字面**给出了 WxH 时回填，
	// 不做 "720p" → "1280x720" 的推导（见 mediaResponseSize 注释）。
	Size  string                  `json:"size,omitempty"`
	Error *mediaTaskResponseError `json:"error,omitempty"`
	// Artifacts 是本网关的**扩展字段**（OpenAI video 对象没有它）：
	// 免重定向的直连产物地址；标准取产物方式仍是 GET /v1/videos/{id}/content。
	// 保留它是刻意决策——前端需要直接播放/下载，不必为每个产物再发一次请求换 302。
	Artifacts []core.MediaArtifact `json:"artifacts,omitempty"`
}

// mediaTaskResponseError 是任务级业务失败（200 响应体里的字段，与 HTTP 错误信封区分）。
type mediaTaskResponseError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// writeMediaTaskError 按 docs/media-api.md §6 的 OpenAI 错误信封写响应。
//
// 为什么不用 internal/errors 的统一渲染器：媒体链路的 code（not_found_error /
// direct_model_not_allowed / not_implemented …）尚未登记进 errors.registry，
// Lookup 会回退成 internal_error（状态码与 code 都会错）。这里直接按已冻结的
// 对外契约构造信封，不新增/修改公共错误码表（避免与 E 队冲突）。
func writeMediaTaskError(c *gin.Context, status int, code, message, param string) {
	body := gin.H{
		"message": message,
		"type":    code,
		"code":    code,
	}
	if param != "" {
		body["param"] = param
	} else {
		body["param"] = nil
	}
	c.AbortWithStatusJSON(status, gin.H{"error": body})
}

// ─────────────────────────────────────────────────────────────────────────────
// 创建（C5/C6）
// ─────────────────────────────────────────────────────────────────────────────

// Create 处理 POST /v1/videos。
//
// 流程（顺序是硬约束）：
//
//	解析 body → 组调度（PrepareAuthorizedGates：预算 + 请求配额）
//	→ 模型层配额 → 无价拒绝（唯一接线点）→ [幂等预占] → 适配器 Submit（WithNoRetry）
//	→ **同步**落库（失败必须 5xx，禁止 fire-and-forget）
//
// 幂等（C6）：Idempotency-Key 非空时**先占位**——冻结契约规定
// (api_key_id, idempotency_key) 已存在 → 200 + 既有任务，此时**直接返回既有任务、
// 不打上游**。之所以「先占位」：契约没有提供「按键查询」的端点，只有 POST 本身能告诉
// 调用方是否命中幂等；而「命中不打得上游」是硬要求，因此必须先 POST 占位、拿到 201
// 才继续提交上游，拿到 200 立即返回。
func (h *MediaTaskHandler) Create(c *gin.Context) {
	if h == nil || h.cfg.Store == nil {
		writeMediaTaskError(c, http.StatusServiceUnavailable, mediaErrUnavailable,
			"任务持久化未接线（控制面内部 API 不可用）", "")
		return
	}

	raw, rawBody, ok := readMediaTaskJSON(c)
	if !ok {
		return
	}
	model := rawString(raw, "model")
	if model == "" {
		writeMediaTaskError(c, http.StatusBadRequest, mediaErrInvalidRequest, "model 字段是必填项", "model")
		return
	}
	// 归一参数（冻结键集，见 mediaSubmitParams 注释）。图片列表同时决定 action。
	submitParams := mediaSubmitParams(raw)
	images, _ := submitParams["images"].([]string)
	if rawString(raw, "prompt") == "" && len(images) == 0 {
		// prompt 与输入图至少需要一个（纯图生视频可以没有 prompt）。
		writeMediaTaskError(c, http.StatusBadRequest, mediaErrInvalidRequest,
			"prompt 与 image 至少需要一个", "prompt")
		return
	}
	kind := mediaTaskKind(raw)
	action := mediaTaskAction(images)
	if declared := core.MediaAction(rawString(raw, "action")); declared.IsValid() && declared != action {
		// 显式 action 与图片数量矛盾：fail-closed（不静默按其中一个执行）。
		writeMediaTaskError(c, http.StatusBadRequest, mediaErrInvalidRequest,
			"action 与图片数量不一致：图片 "+strconv.Itoa(len(images))+" 张对应 "+string(action), "action")
		return
	}

	decision, releaseQuota := h.dispatch(c, model)
	if releaseQuota != nil {
		defer releaseQuota()
	}
	if c.IsAborted() {
		return
	}
	if decision == nil {
		// 图像/视频链路只接受模型组名（docs/media-api.md §1.2）。
		writeMediaTaskError(c, http.StatusForbidden, mediaErrDirectModelNotAllowed,
			"model 必须填模型组名，直连上游模型 ID 被拒绝", model)
		return
	}

	upstreamModel := strings.TrimSpace(decision.ModelName)
	providerType := strings.TrimSpace(decision.ChannelType)
	providerName := strings.TrimSpace(decision.ProviderName)
	if providerType == "" && h.cfg.RouterGetter != nil {
		if r := h.cfg.RouterGetter(); r != nil {
			providerType = strings.TrimSpace(r.GetProviderType(upstreamModel))
		}
	}
	if upstreamModel == "" || providerType == "" {
		// 组调度没给出可路由的上游模型/渠道类型：既不能落库（控制面要求这两个字段必填），
		// 也不能提交上游（不知道打哪个渠道）。
		writeMediaTaskError(c, http.StatusNotImplemented, mediaErrNotImplemented,
			"该模型组未能解析出上游模型或渠道类型", model)
		return
	}
	if !h.checkModelQuota(c, decision.ModelUUID) {
		return
	}
	// 无价拒绝的**唯一接线点**：组调度之后、调上游之前（A2 / 设计文档 §8.2）。
	// retrieve / content 端点不得调用（它们不产生新用量）。
	if !middleware.EnsureMediaPricing(c, upstreamModel, providerType) {
		return
	}

	adapter := h.cfg.Registry.Get(providerType)
	if adapter == nil {
		writeMediaTaskError(c, http.StatusNotImplemented, mediaErrNotImplemented,
			"该渠道未实现视频任务能力（未接线或渠道不支持）", providerType)
		return
	}

	taskID, err := h.newTaskID()
	if err != nil {
		slog.Error("media task: 生成任务 id 失败", "error", err)
		writeMediaTaskError(c, http.StatusBadGateway, mediaErrUpstream, "任务 id 生成失败", "")
		return
	}
	// 幂等键：优先 Idempotency-Key 头，其次 body 里的 client_request_id
	// （设计文档 §6.3「Idempotency-Key 头（或 body 内 client_request_id）」）。
	// 注意 client_request_id **不进 Params**（冻结键集里没有它），不会被透传给适配器。
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = rawString(raw, "client_request_id")
	}
	privateData, perr := marshalMediaPrivateData(mediaTaskPrivateData{
		Kind:          string(kind),
		Action:        string(action),
		UpstreamModel: upstreamModel,
		ProviderName:  providerName,
		ProviderType:  providerType,
		SubmitParams:  submitParams,
	})
	if perr != nil {
		// private_data 只承载渠道细节（记账口径 + 上游模型名），序列化失败不阻断主流程。
		slog.Error("media task: private_data 序列化失败", "error", perr)
	}

	createReq := infra.MediaCreateRequest{
		TaskID:         taskID,
		APIKeyID:       c.GetString("key_id"),
		APIKeyName:     c.GetString("key_name"),
		APIKeyType:     c.GetString("key_type"),
		UserID:         c.GetString("user_id"),
		UserName:       c.GetString("user_name"),
		Model:          model,
		ProviderName:   providerName,
		ProviderType:   providerType,
		Status:         string(core.MediaStatusQueued),
		RequestBody:    truncateVideoTaskBody(rawBody),
		Kind:           string(kind),
		Action:         string(action),
		Progress:       mediaProgressZero,
		IdempotencyKey: idempotencyKey,
		PrivateData:    json.RawMessage(privateData),
		DeptID:         c.GetString("dept_id"),
		StudioID:       c.GetString("studio_id"),
	}

	reserved := false
	if idempotencyKey != "" {
		res, err := h.cfg.Store.CreateMediaTask(c.Request.Context(), createReq)
		if err != nil {
			h.respondStoreFailure(c, "preempt", taskID, err)
			return
		}
		if !res.Created {
			// 幂等命中：直接返回既有任务，**不打上游、不重复计费**。
			if res.Task == nil {
				slog.Error("media task: 幂等命中但控制面未回传既有任务",
					"task_id", taskID, "idempotency_key", idempotencyKey)
				writeMediaTaskError(c, http.StatusBadGateway, mediaErrUpstream,
					"幂等命中但控制面未回传既有任务（内部 API 契约不符）", taskID)
				return
			}
			slog.Info("media task: 幂等命中，直接返回既有任务",
				"task_id", res.Task.TaskID, "idempotency_key", idempotencyKey)
			c.JSON(http.StatusOK, h.buildResponse(res.Task))
			return
		}
		reserved = true
	}

	// 提交路径必须「至多发出一次」：上游创建接口没有幂等键，重发 = 上游第二个任务 + 重复计费。
	// handler 层不做任何「换渠道重发」；group failover 由 B 队在 chat.go 统一按 IsNoRetry 门控。
	submitCtx := llmclient.WithNoRetry(c.Request.Context())
	submitRes, err := adapter.Submit(submitCtx, &core.MediaSubmitRequest{
		Kind:          kind,
		Action:        action,
		UpstreamModel: upstreamModel,
		Params:        submitParams,
	})
	if err != nil {
		h.abortSubmit(c, taskID, reserved, "上游提交失败: "+err.Error())
		return
	}
	if submitRes == nil || strings.TrimSpace(submitRes.VendorTaskID) == "" {
		// 契约 3：适配器必须回上游任务 id；空 id 属协议失败，绝不能伪造一个对外 id 落库。
		h.abortSubmit(c, taskID, reserved, "适配器未返回上游任务 id（协议失败）")
		return
	}
	vendorTaskID := strings.TrimSpace(submitRes.VendorTaskID)
	initialStatus := submitRes.Status
	if !initialStatus.IsValid() {
		initialStatus = core.MediaStatusQueued
	}

	if reserved {
		// 补齐全量：幂等预占行已存在，这里同步回填上游 id 与初始状态。
		// **必须同步**——写失败就返回 5xx，否则客户端会拿到一个永远查不到上游的任务。
		upd, err := h.cfg.Store.UpdateMediaTask(c.Request.Context(), taskID, infra.MediaUpdateRequest{
			Status:         string(initialStatus),
			VendorTaskID:   vendorTaskID,
			Progress:       mediaProgressZero,
			ExpectedStatus: string(core.MediaStatusQueued),
		})
		if err != nil {
			h.respondStoreFailure(c, "backfill", taskID, err)
			return
		}
		if upd != nil && upd.Task != nil {
			c.JSON(http.StatusOK, h.buildResponse(upd.Task))
			return
		}
		c.JSON(http.StatusOK, h.newlyCreatedResponse(taskID, model, initialStatus, submitParams))
		return
	}

	createReq.VendorTaskID = vendorTaskID
	createReq.Status = string(initialStatus)
	res, err := h.cfg.Store.CreateMediaTask(c.Request.Context(), createReq)
	if err != nil {
		h.respondStoreFailure(c, "create", taskID, err)
		return
	}
	if res != nil && !res.Created {
		// 契约：未带 Idempotency-Key 的创建**必须**是 201。收到 200 说明控制面按别的
		// 维度去重了（契约违背）。这里刻意**不返回 5xx**：上游任务已经创建（会产生费用），
		// 返回 5xx 会让客户端重试并再创建一个上游任务（重复计费），比返回错任务更糟。
		// 因此响亮记录 + 照常返回任务，交由对账发现。
		slog.Error("media task: 未带幂等键的创建却收到 200（控制面契约违背，疑似按其他维度去重）",
			"task_id", taskID, "returned_task_id", taskIDOf(res.Task))
	}
	if res != nil && res.Task != nil {
		c.JSON(http.StatusOK, h.buildResponse(res.Task))
		return
	}
	c.JSON(http.StatusOK, h.newlyCreatedResponse(taskID, model, initialStatus, submitParams))
}

// newlyCreatedResponse 在控制面没回传任务行时构造创建响应（幂等预占等场景）。
//
// params 是本次请求的归一参数集（submit_params）：用它回填 seconds / size——
// 这两项来自**调用方自己的请求**，不依赖控制面是否回传 private_data
// （控制面内部 API 不返回 private_data，见 mediaRequestParams 注释）。
func (h *MediaTaskHandler) newlyCreatedResponse(
	taskID, model string, status core.MediaStatus, params map[string]any,
) mediaTaskResponse {
	return mediaTaskResponse{
		ID:        taskID,
		Object:    mediaTaskObjectValue,
		Status:    string(status),
		Model:     model,
		CreatedAt: h.now().Unix(),
		// 刚受理：整数 0（对外进度，不是存储层的 mediaProgressZero 字符串）。
		Progress: mediaProgressUnknown,
		Seconds:  mediaResponseSeconds(params, nil),
		Size:     mediaResponseSize(params),
	}
}

// dispatch 执行组调度与准入闸门。
//
// 返回（决策, 释放函数）：决策为 nil 表示「不是模型组」（调用方按 403 处理）。
// 释放函数必须由**请求作用域**（handler 本体）defer，不能在 dispatch 内 defer——
// 那样配额会在返回前就被释放，等于没占住并发额度。
func (h *MediaTaskHandler) dispatch(c *gin.Context, model string) (*scheduler.RouteDecision, func()) {
	var decision *scheduler.RouteDecision
	releaseQuota, err := PrepareAuthorizedGates(c, model, func() error {
		if h.cfg.GroupDispatch == nil {
			return nil
		}
		d, dispatchErr := h.cfg.GroupDispatch.Dispatch(c, model, nil)
		if dispatchErr != nil {
			return dispatchErr
		}
		decision = d
		return nil
	}, h.cfg.BudgetGate, h.cfg.RequestQuota)
	if err != nil {
		if c.IsAborted() {
			return nil, nil
		}
		if !budget.WriteHTTPError(c, err) {
			respondGroupError(c, err)
		}
		return nil, nil
	}
	return decision, releaseQuota
}

// checkModelQuota 模型层配额（路由选定具体 model 之后、转发上游之前）。
// 返回 false 表示已被拒绝并写好响应。
func (h *MediaTaskHandler) checkModelQuota(c *gin.Context, modelUUID string) bool {
	if h.cfg.ModelQuotaGate == nil || modelUUID == "" {
		return true
	}
	st, _ := c.Get("quota_subject_type")
	si, _ := c.Get("quota_subject_id")
	subjectType, _ := st.(string)
	subjectID, _ := si.(string)
	if subjectType == "" || subjectID == "" {
		return true
	}
	// BeforeUpstream 自己写响应（与 chat.go 的调用方式一致）。
	return h.cfg.ModelQuotaGate.BeforeUpstream(c, subjectType, subjectID, modelUUID) == nil
}

// abortSubmit 提交上游失败时的统一收尾：占位行尽力标记 failed（避免幂等键被永久卡在 queued），
// 然后返回 5xx。
func (h *MediaTaskHandler) abortSubmit(c *gin.Context, taskID string, reserved bool, reason string) {
	if reserved {
		h.markTaskFailedBestEffort(c.Request.Context(), taskID, reason)
	}
	slog.Warn("media task: 提交上游失败", "task_id", taskID, "reserved", reserved, "reason", reason)
	writeMediaTaskError(c, http.StatusBadGateway, mediaErrUpstream, reason, taskID)
}

// ─────────────────────────────────────────────────────────────────────────────
// 查询（C5/C6/C7/C8）
// ─────────────────────────────────────────────────────────────────────────────

// Retrieve 处理 GET /v1/videos/:task_id。
func (h *MediaTaskHandler) Retrieve(c *gin.Context) {
	if h == nil || h.cfg.Store == nil {
		writeMediaTaskError(c, http.StatusServiceUnavailable, mediaErrUnavailable, "任务持久化未接线", "")
		return
	}
	task, ok := h.loadOwnedTask(c)
	if !ok {
		return
	}
	h.setCallLogContext(c, task)

	if mapStoredStatus(task.Status).IsTerminal() {
		// 终态缓存：终态不可回退，直接返回，不再打上游（也不重复记账）。
		c.JSON(http.StatusOK, h.buildResponse(task))
		return
	}
	if h.timedOut(task) {
		h.expireTask(c, task)
		return
	}

	adapter := h.cfg.Registry.Get(task.ProviderType)
	if adapter == nil {
		writeMediaTaskError(c, http.StatusNotImplemented, mediaErrNotImplemented,
			"该任务的渠道未实现视频任务能力", task.ProviderType)
		return
	}
	vendorTaskID := strings.TrimSpace(task.VendorTaskID)
	if vendorTaskID == "" {
		// 幂等预占成功但回填尚未完成（或进程在上游提交后被中断）：返回当前状态，不打上游。
		c.JSON(http.StatusOK, h.buildResponse(task))
		return
	}

	// 查询路径**保留**重试/失败转移（不加 WithNoRetry）：与提交路径的语义刻意相反。
	st, err := adapter.Query(c.Request.Context(), vendorTaskID)
	if err != nil {
		h.handleQueryFailure(c, task, err)
		return
	}
	if st == nil {
		h.handleQueryFailure(c, task, errors.New("adapter returned nil status"))
		return
	}
	if !st.Status.IsValid() {
		// 契约 2：上游未知状态词是协议失败，既不当 completed 也不无限当 in_progress。
		h.handleQueryFailure(c, task, errors.New("adapter returned invalid status word: "+string(st.Status)))
		return
	}

	update := infra.MediaUpdateRequest{
		Status:       string(st.Status),
		Progress:     st.Progress,
		VendorTaskID: vendorTaskID,
	}
	var mediaInput core.MediaCostInput
	if st.Status == core.MediaStatusCompleted {
		plan := h.buildCompletionUpdate(task, st)
		update.VideoURL = plan.VideoURL
		update.ResponseBody = plan.ResponseBody
		update.Usage = plan.Usage
		update.CostMicroUSD = plan.CostMicroUSD
		update.BilledAt = plan.BilledAt
		mediaInput = plan.CostInput
	} else if st.Status == core.MediaStatusFailed {
		update.ErrorMessage = st.FailReason
		update.ResponseBody = marshalMediaStatusPayload(mediaTaskStatusPayload{
			Status:     string(st.Status),
			Progress:   st.Progress,
			FailReason: st.FailReason,
			Usage:      st.Usage,
		})
	}

	latest, advanced, err := h.casAdvance(c.Request.Context(), task, st.Status.IsTerminal(), update)
	if err != nil {
		slog.Error("media task: CAS 更新失败", "task_id", task.TaskID, "error", err)
		writeMediaTaskError(c, http.StatusBadGateway, mediaErrUpstream, "任务状态写入失败", task.TaskID)
		return
	}

	// 终态成功且「本次 CAS 真的推进了」才记账一次（C8）：
	// 重复 retrieve、并发 retrieve 的败者、失败/超时都不会记账。
	if advanced && st.Status == core.MediaStatusCompleted {
		// 精确类型 core.MediaCostInput（值，不是指针/别名）——采集器做精确类型断言。
		c.Set(gwobs.CtxKeyMediaUsage, mediaInput)
	}

	c.JSON(http.StatusOK, h.buildResponse(latest))
}

// casAdvance 按「只前进」语义做 CAS 推进，返回（最新任务、本次是否推进成功）。
//
// 关键点：冻结契约要求「expected_status 非空时仅在当前状态相等时更新」，
// 但没有明确「是否允许跨级前进」（queued → completed）。为兼容两种实现，
// 当 from=queued 且目标为终态时，先补写一次 in_progress，再写终态；
// 这样无论服务端是「相邻步」还是「可跳步」语义都能推进，且记账只在最后一次成功写入时发生。
func (h *MediaTaskHandler) casAdvance(
	ctx context.Context,
	task *infra.MediaTask,
	targetTerminal bool,
	update infra.MediaUpdateRequest,
) (*infra.MediaTask, bool, error) {
	if task == nil {
		return nil, false, errors.New("media task: nil task")
	}
	current := task
	if mapStoredStatus(current.Status) == core.MediaStatusQueued && targetTerminal {
		res, err := h.cfg.Store.UpdateMediaTask(ctx, current.TaskID, infra.MediaUpdateRequest{
			Status:         string(core.MediaStatusInProgress),
			ExpectedStatus: strings.TrimSpace(current.Status),
		})
		if err != nil {
			return current, false, err
		}
		if res != nil && res.Task != nil {
			current = res.Task
		} else {
			current = &infra.MediaTask{TaskID: current.TaskID, Status: string(core.MediaStatusInProgress)}
		}
		if mapStoredStatus(current.Status).IsTerminal() {
			// 并发写入者已经推进到终态：本次不再写终态（也就不会重复记账）。
			return current, false, nil
		}
	}

	update.ExpectedStatus = strings.TrimSpace(current.Status)
	res, err := h.cfg.Store.UpdateMediaTask(ctx, current.TaskID, update)
	if err != nil {
		return current, false, err
	}
	latest := current
	if res != nil && res.Task != nil {
		latest = res.Task
	}
	return latest, res != nil && res.Updated, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 产物（C5，Phase 1：302 直跳上游）
// ─────────────────────────────────────────────────────────────────────────────

// Content 处理 GET /v1/videos/:task_id/content。
func (h *MediaTaskHandler) Content(c *gin.Context) {
	if h == nil || h.cfg.Store == nil {
		writeMediaTaskError(c, http.StatusServiceUnavailable, mediaErrUnavailable, "任务持久化未接线", "")
		return
	}
	task, ok := h.loadOwnedTask(c)
	if !ok {
		return
	}
	h.setCallLogContext(c, task)

	artifactURL := mediaArtifactURL(task)
	if artifactURL == "" {
		writeMediaTaskError(c, http.StatusNotFound, mediaErrNotFound,
			"任务尚未产出可下载的产物（或任务未完成）", task.TaskID)
		return
	}
	// 只允许 http/https：产物 URL 来自上游（外部输入），避免异常 scheme 变成可利用的跳转。
	parsed, err := url.Parse(artifactURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		slog.Error("media task: 产物 URL 非法", "task_id", task.TaskID, "scheme", parsed.Scheme)
		writeMediaTaskError(c, http.StatusBadGateway, mediaErrUpstream, "产物地址非法", task.TaskID)
		return
	}
	// Phase 1-4：credentialless 直跳上游（设计文档 §6.3 / D4）。
	// 上游签名链接有效期短，禁止任何中间层缓存。
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, artifactURL)
}

// ─────────────────────────────────────────────────────────────────────────────
// 失败分类与超时（C7）
// ─────────────────────────────────────────────────────────────────────────────

// mediaPollFailureClass 是上游轮询失败的归一分类（对齐 New API 的
// not_found/auth/transient/unrecognized，设计文档 §4.1）。
type mediaPollFailureClass string

const (
	pollClassNotFound     mediaPollFailureClass = "not_found"
	pollClassAuth         mediaPollFailureClass = "auth"
	pollClassTransient    mediaPollFailureClass = "transient"
	pollClassUnrecognized mediaPollFailureClass = "unrecognized"
)

// classifyMediaPollError 把适配器返回的 error 归一为四类。
//
// 识别顺序：
//  1. core.ErrMediaTaskNotFound（适配器显式包装）→ not_found；
//  2. core.GatewayError.StatusCode（llmclient 的既有错误类型）→ 按状态码分类；
//  3. context 超时/取消 → transient；
//  4. 其它 → 文本兜底，仍无法识别则 unrecognized（按可恢复处理并计数）。
//
// 注：这是**基于既有错误类型 + 文本兜底**的启发式。若 D 队后续为适配器错误提供
// 显式状态访问器（如 StatusCode() int），这里应改为优先使用它。
func classifyMediaPollError(err error) mediaPollFailureClass {
	if err == nil {
		return pollClassTransient
	}
	if errors.Is(err, core.ErrMediaTaskNotFound) {
		return pollClassNotFound
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return pollClassTransient
	}
	var gwErr *core.GatewayError
	if errors.As(err, &gwErr) && gwErr != nil && gwErr.StatusCode > 0 {
		return classifyPollStatusCode(gwErr.StatusCode)
	}
	return classifyPollErrorText(err.Error())
}

func classifyPollStatusCode(status int) mediaPollFailureClass {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return pollClassAuth
	case status == http.StatusNotFound:
		return pollClassNotFound
	case status == http.StatusTooManyRequests || status >= 500:
		return pollClassTransient
	default:
		return pollClassUnrecognized
	}
}

func classifyPollErrorText(msg string) mediaPollFailureClass {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "401"), strings.Contains(lower, "403"),
		strings.Contains(lower, "unauthorized"), strings.Contains(lower, "forbidden"),
		strings.Contains(lower, "invalid access key"), strings.Contains(lower, "auth"):
		return pollClassAuth
	case strings.Contains(lower, "429"), strings.Contains(lower, "500"), strings.Contains(lower, "502"),
		strings.Contains(lower, "503"), strings.Contains(lower, "504"),
		strings.Contains(lower, "timeout"), strings.Contains(lower, "timed out"),
		strings.Contains(lower, "connection refused"), strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "eof"), strings.Contains(lower, "no such host"),
		strings.Contains(lower, "temporary failure"):
		return pollClassTransient
	default:
		return pollClassUnrecognized
	}
}

// handleQueryFailure 处理上游查询失败（C7）：分类 → 计数 → 决定继续或放弃。
func (h *MediaTaskHandler) handleQueryFailure(c *gin.Context, task *infra.MediaTask, err error) {
	class := classifyMediaPollError(err)
	failures := task.PollFailures + 1
	giveUp := class == pollClassNotFound || failures >= h.cfg.MaxPollFailures
	slog.Warn("media task: 上游轮询失败",
		"task_id", task.TaskID, "class", string(class), "failures", failures,
		"max_failures", h.cfg.MaxPollFailures, "give_up", giveUp, "error", err.Error())

	if giveUp {
		h.giveUpTask(c, task, "上游轮询失败（"+string(class)+"）："+err.Error())
		return
	}
	// 未达上限：只累加失败次数（不改状态），并向客户端返回 5xx（传输/协议失败语义）。
	h.recordPollFailure(c.Request.Context(), task, failures)
	writeMediaTaskError(c, http.StatusBadGateway, mediaErrUpstream,
		"上游任务查询失败（"+string(class)+"），请稍后重试", task.TaskID)
}

// giveUpTask 放弃轮询：CAS 推进到 failed（业务失败语义：200 + status=failed，不记账）。
func (h *MediaTaskHandler) giveUpTask(c *gin.Context, task *infra.MediaTask, reason string) {
	update := infra.MediaUpdateRequest{
		Status:       string(core.MediaStatusFailed),
		ErrorMessage: reason,
		ResponseBody: marshalMediaStatusPayload(mediaTaskStatusPayload{
			Status:     string(core.MediaStatusFailed),
			FailReason: reason,
		}),
	}
	latest, _, err := h.casAdvance(c.Request.Context(), task, true, update)
	if err != nil {
		slog.Error("media task: 放弃轮询时写入失败", "task_id", task.TaskID, "error", err)
		writeMediaTaskError(c, http.StatusBadGateway, mediaErrUpstream, "任务状态写入失败", task.TaskID)
		return
	}
	c.JSON(http.StatusOK, h.buildResponse(latest))
}

// recordPollFailure 累加轮询失败次数（CAS：仅当状态未变时写入；失败只记日志）。
func (h *MediaTaskHandler) recordPollFailure(ctx context.Context, task *infra.MediaTask, failures int) {
	count := failures
	if _, err := h.cfg.Store.UpdateMediaTask(ctx, task.TaskID, infra.MediaUpdateRequest{
		PollFailures:   &count,
		ExpectedStatus: strings.TrimSpace(task.Status),
	}); err != nil {
		slog.Warn("media task: 累加轮询失败次数失败", "task_id", task.TaskID, "error", err)
	}
}

// timedOut 报告任务是否已超过 MEDIA_TASK_TIMEOUT_MINUTES 仍未终态。
//
// 这里**只做前台判定**（查询请求内），不做后台扫描——超时 sweeper 归控制面（C1）。
func (h *MediaTaskHandler) timedOut(task *infra.MediaTask) bool {
	if task == nil || h.cfg.Timeout <= 0 || task.CreatedAt.IsZero() {
		return false
	}
	return h.now().Sub(task.CreatedAt) >= h.cfg.Timeout
}

// expireTask 把超时任务 CAS 推进到 expired（对外归一为 failed、不记账）。
func (h *MediaTaskHandler) expireTask(c *gin.Context, task *infra.MediaTask) {
	reason := "任务超时（网关侧前台判定，阈值 " + h.cfg.Timeout.String() + "）"
	update := infra.MediaUpdateRequest{
		Status:       mediaStoredStatusExpired,
		ErrorMessage: reason,
		ResponseBody: marshalMediaStatusPayload(mediaTaskStatusPayload{
			Status:     string(core.MediaStatusFailed),
			FailReason: reason,
		}),
	}
	latest, _, err := h.casAdvance(c.Request.Context(), task, true, update)
	if err != nil {
		slog.Error("media task: 超时置位失败", "task_id", task.TaskID, "error", err)
		writeMediaTaskError(c, http.StatusBadGateway, mediaErrUpstream, "任务状态写入失败", task.TaskID)
		return
	}
	slog.Warn("media task: 任务超时（对外归一为 failed，不记账）", "task_id", task.TaskID)
	c.JSON(http.StatusOK, h.buildResponse(latest))
}

// markTaskFailedBestEffort 尽力把任务标记为失败（提交失败时收尾，避免幂等键永久卡在 queued）。
func (h *MediaTaskHandler) markTaskFailedBestEffort(ctx context.Context, taskID, reason string) {
	if h.cfg.Store == nil || taskID == "" {
		return
	}
	task, err := h.cfg.Store.GetMediaTask(ctx, taskID)
	if err != nil || task == nil {
		return
	}
	// 只在任务仍为非终态时才写：绝不把已经推进到终态的任务改回 failed。
	if mapStoredStatus(task.Status).IsTerminal() {
		return
	}
	if _, err := h.cfg.Store.UpdateMediaTask(ctx, taskID, infra.MediaUpdateRequest{
		Status:         string(core.MediaStatusFailed),
		ErrorMessage:   reason,
		ExpectedStatus: strings.TrimSpace(task.Status),
	}); err != nil {
		slog.Warn("media task: 收尾标记失败未成功", "task_id", taskID, "error", err)
	}
}

// respondStoreFailure 统一处理控制面写失败：**必须 5xx**（禁止 fire-and-forget 造成假成功）。
func (h *MediaTaskHandler) respondStoreFailure(c *gin.Context, op, taskID string, err error) {
	var cpErr *infra.ControlPlaneError
	if errors.As(err, &cpErr) && cpErr.IsClientError() {
		// 4xx 大概率是两端契约漂移（字段类型/必填约束不一致），必须响亮记录，
		// 否则会表现为「偶发 502」而难以定位。
		slog.Error("media task: 控制面拒绝写入（疑似契约漂移）",
			"op", op, "task_id", taskID, "status", cpErr.Status, "body", cpErr.Body)
	} else {
		slog.Error("media task: 控制面写入失败", "op", op, "task_id", taskID, "error", err.Error())
	}
	writeMediaTaskError(c, http.StatusBadGateway, mediaErrUpstream,
		"任务持久化失败（控制面内部 API）："+err.Error(), taskID)
}

// ─────────────────────────────────────────────────────────────────────────────
// 归属校验、状态映射、响应构造
// ─────────────────────────────────────────────────────────────────────────────

// loadOwnedTask 取任务并做归属校验；第二返回值为 false 时已写好响应。
//
// 归属规则（C6 / docs/media-api.md §4.2）：
//   - 任务有 api_key_id → 必须与调用方 key_id 相等；
//   - 任务没有 api_key_id 但有 user_id → 退化为「同一用户」判定；
//   - **不属于调用方一律 404**（不是 403）：403 会泄露「这个 task_id 存在」这一事实，
//     让攻击者可以枚举探测他人任务。
func (h *MediaTaskHandler) loadOwnedTask(c *gin.Context) (*infra.MediaTask, bool) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		writeMediaTaskError(c, http.StatusNotFound, mediaErrNotFound, "任务不存在", "")
		return nil, false
	}
	task, err := h.cfg.Store.GetMediaTask(c.Request.Context(), taskID)
	if err != nil {
		if errors.Is(err, infra.ErrMediaTaskNotFound) {
			writeMediaTaskError(c, http.StatusNotFound, mediaErrNotFound, "任务不存在", taskID)
			return nil, false
		}
		slog.Error("media task: 查询任务失败", "task_id", taskID, "error", err)
		writeMediaTaskError(c, http.StatusBadGateway, mediaErrUpstream, "任务查询失败", taskID)
		return nil, false
	}
	if task == nil || !mediaTaskOwnedBy(task, c.GetString("key_id"), c.GetString("user_id")) {
		writeMediaTaskError(c, http.StatusNotFound, mediaErrNotFound, "任务不存在", taskID)
		return nil, false
	}
	return task, true
}

// mediaTaskOwnedBy 判断任务是否属于调用方。
//
// 包级函数便于单测直接覆盖（IDOR 是数据安全边界）。
func mediaTaskOwnedBy(task *infra.MediaTask, keyID, userID string) bool {
	if task == nil {
		return false
	}
	if task.APIKeyID != "" {
		return keyID != "" && task.APIKeyID == keyID
	}
	if task.UserID != "" {
		return userID != "" && task.UserID == userID
	}
	// 既无 key 也无 user（历史脏数据）：拒绝，宁可 404 也不放行。
	return false
}

// mapStoredStatus 把存储层状态归一为对外的 OpenAI 词表（唯一对外口径）。
//
// 为什么需要映射（而不是直接透传）：存量 video_tasks 行用的是 Seedance 词表
// （submitted / processing / succeed…），控制面 sweeper 还会写入 expired；
// 对外必须只有 queued / in_progress / completed / failed 四个值。
func mapStoredStatus(raw string) core.MediaStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "queued", "submitted", "created", "pending", "not_start", "waiting":
		return core.MediaStatusQueued
	case "in_progress", "processing", "running", "queueing", "started", "generating":
		return core.MediaStatusInProgress
	case "completed", "succeed", "succeeded", "success", "finished", "done":
		return core.MediaStatusCompleted
	case "failed", "failure", "error", "expired", "cancelled", "canceled", "rejected":
		return core.MediaStatusFailed
	default:
		// 未知存储值：保守当作「非终态」，让查询继续走上游轮询而不是误判终态。
		return core.MediaStatusQueued
	}
}

func (h *MediaTaskHandler) setCallLogContext(c *gin.Context, task *infra.MediaTask) {
	if task == nil {
		return
	}
	// 真实 model（模型组名）——**不要**再引入 :CreateTask/:GetTask 之类的伪模型后缀：
	// call_type 由采集侧 DeriveCallType(method, path) 统一算出（A11）；
	// 伪后缀是既有 Seedance 链路的遗留约定，读侧过滤仍依赖它，新链路不扩散。
	c.Set("model", task.Model)
	if task.ProviderName != "" {
		c.Set("provider", task.ProviderName)
	}
	if private, ok := parseMediaPrivateData(task.PrivateData.String()); ok && private.UpstreamModel != "" {
		// 采集器按 routed_model 解析价表（model 是组名时价表查不到）。
		c.Set("routed_model", private.UpstreamModel)
	}
}

// buildResponse 把存储行转成对外响应（OpenAI video 对象形状）。
func (h *MediaTaskHandler) buildResponse(task *infra.MediaTask) mediaTaskResponse {
	resp := mediaTaskResponse{Object: mediaTaskObjectValue}
	if task == nil {
		return resp
	}
	mapped := mapStoredStatus(task.Status)
	private, _ := parseMediaPrivateData(task.PrivateData.String())
	payload, _ := parseMediaStatusPayload(task.ResponseBody)

	resp.ID = task.TaskID
	resp.Status = string(mapped)
	resp.Model = task.Model
	resp.Progress = mediaProgressValue(task.Progress, mapped)
	if !task.CreatedAt.IsZero() {
		resp.CreatedAt = task.CreatedAt.Unix()
	}
	if task.FinishedAt != nil && !task.FinishedAt.IsZero() {
		resp.CompletedAt = task.FinishedAt.Unix()
	}
	if len(payload.Artifacts) > 0 {
		resp.Artifacts = payload.Artifacts
	}
	params := mediaRequestParams(private, task)
	resp.Seconds = mediaResponseSeconds(params, task)
	resp.Size = mediaResponseSize(params)
	reason := strings.TrimSpace(task.ErrorMessage)
	if reason == "" {
		reason = strings.TrimSpace(payload.FailReason)
	}
	if mapped == core.MediaStatusFailed && reason != "" {
		resp.Error = &mediaTaskResponseError{Message: reason, Code: mediaErrUpstream}
	}
	return resp
}

// mediaRequestParams 取「本次创建时的归一参数集」（冻结 7 键），用于回填 seconds / size。
//
// 取数顺序（都可缺省，缺省即省略字段）：
//
//  1. private_data.submit_params —— 网关在创建时写入的归一副本。**注意**：控制面把
//     private_data 标了 json:"-"，任何 HTTP 出口都不序列化它（internal/controlplane/model/video_task.go），
//     所以经控制面往返拿到的任务里这个字段是空的；保留这一路是为了兼容测试夹具与
//     将来控制面开放该字段。
//  2. media_tasks.request_body —— **生产环境的实际来源**：内部 API 会返回它
//     （json:"request_body,omitempty"），内容是网关自己持久化的原始请求体。
//     它可能被截断（truncateVideoTaskBody，见 video_handler.go）：解析失败则当作取不到，
//     绝不猜测字段。
func mediaRequestParams(private mediaTaskPrivateData, task *infra.MediaTask) map[string]any {
	if len(private.SubmitParams) > 0 {
		return private.SubmitParams
	}
	if task == nil {
		return nil
	}
	raw := strings.TrimSpace(task.RequestBody)
	if raw == "" {
		return nil
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return nil
	}
	return mediaSubmitParams(body)
}

// mediaProgressValue 把存储层的 media_tasks.progress（varchar：百分比字符串**或**阶段名）
// 归一为对外 progress（0-100 整数）。
//
// 规则与理由（E4b 定稿；对接方与前端按此读）：
//
//  1. 能解析成数字（含上游偶发的 "45%"、前后空白、小数）→ 用该数字，clamp 到 [0,100]，
//     小数**向下取整**（宁可低估，也不把 99.4% 说成 100%）；
//  2. 阶段名 → mediaProgressStageValues 的显式映射（0 / 10 / 50 / 100 四档）；
//  3. 空值或认不出的词 → 0（明确的下界；绝不填 100，也绝不外推）；
//  4. 归一后的状态是 completed → 强制 100：完成了就是 100%，否则会出现
//     「status=completed + progress=0」这种自相矛盾的响应（上游不报百分比的渠道很常见）。
//
// 已知局限（如实标注）：网关**不保证** progress 单调不减——存储层只保留最后一次上报值，
// 若某渠道先报百分比数字、后又只报阶段名（或者反过来），对外可能回退（如 80 → 50）。
// 现有渠道的词表是稳定的（Vidu 报百分比、Kling 报阶段名），因此该情形未观察到；
// 若未来出现，应由适配器统一上报口径，而不是在响应层造假收敛。
func mediaProgressValue(raw string, mapped core.MediaStatus) int {
	if mapped == core.MediaStatusCompleted {
		return mediaProgressDone
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		return mediaProgressUnknown
	}
	// 上游偶发百分比形态（New API 的 SetProgressStr 也做同样的 TrimSuffix 处理）。
	if number, ok := parseMediaProgressNumber(strings.TrimSuffix(value, "%")); ok {
		return clampMediaProgress(number)
	}
	if mappedValue, ok := mediaProgressStageValues[strings.ToLower(value)]; ok {
		return mappedValue
	}
	return mediaProgressUnknown
}

// parseMediaProgressNumber 解析进度数字；第二返回值为 false 表示「不是数字」。
func parseMediaProgressNumber(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	// NaN / ±Inf 会污染 int 转换（NaN 在部分平台上转出非确定值），一律当作认不出。
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return int(parsed), true
}

func clampMediaProgress(value int) int {
	if value < 0 {
		return mediaProgressUnknown
	}
	if value > mediaProgressDone {
		return mediaProgressDone
	}
	return value
}

// mediaResponseSeconds 取任务时长（OpenAI video 对象的 seconds，字符串秒）。
//
// 取数顺序：上游实际用量 → 请求参数 duration。**取不到就省略，绝不编造**
// （「没有时长」比「时长猜错」对账/展示的伤害都更小）。
func mediaResponseSeconds(params map[string]any, task *infra.MediaTask) string {
	if seconds := mediaUsageSeconds(task); seconds > 0 {
		return formatMediaSeconds(seconds)
	}
	if seconds := mapFloat(params, "duration"); seconds > 0 {
		return formatMediaSeconds(seconds)
	}
	return ""
}

// mediaUsageSeconds 从 media_tasks.usage（jsonb 文本）里读实际时长。
//
// usage 的形状见 mediaTaskUsagePayload：{vendor:{...}, cost_input:{...}}。
// vendor 是上游原始用量（Kling 的 duration、Vidu 的 duration/output_seconds…），
// cost_input.output_seconds 是 C8 记账时按实际用量或请求参数归一出来的时长。
func mediaUsageSeconds(task *infra.MediaTask) float64 {
	if task == nil {
		return 0
	}
	raw := strings.TrimSpace(task.Usage.String())
	if raw == "" || raw == "null" {
		return 0
	}
	var usage map[string]any
	if err := json.Unmarshal([]byte(raw), &usage); err != nil {
		return 0
	}
	if vendor, ok := usage["vendor"].(map[string]any); ok {
		if seconds := firstPositiveFloat(vendor,
			"duration", "output_seconds", "duration_seconds", "video_duration"); seconds > 0 {
			return seconds
		}
	}
	if costInput, ok := usage["cost_input"].(map[string]any); ok {
		if seconds := firstPositiveFloat(costInput, "output_seconds"); seconds > 0 {
			return seconds
		}
	}
	return 0
}

// formatMediaSeconds 把秒数写成 OpenAI 形状的十进制字符串（整秒不带小数点：8 → "8"）。
func formatMediaSeconds(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	if seconds == math.Trunc(seconds) {
		return strconv.FormatInt(int64(seconds), 10)
	}
	return strconv.FormatFloat(seconds, 'f', -1, 64)
}

// mediaResponseSize 取产物尺寸（OpenAI video 对象的 size，形如 "1280x720"）。
//
// 只认**请求参数里字面给出的 WxH**（`size`，其次 `resolution`）：视频创建接口本身没有
// size 字段，客户端可以用它表达精确尺寸（同时网关会用它推导 aspect_ratio）。
//
// 刻意**不做** "720p" → "1280x720" 的推导：短边 720 对应的另一边取决于宽高比、渠道
// 与 mode（Kling std/pro 的输出档位不同），推导值可能与真实产物不符——而 size 是用来
// 描述产物的，「推导错」比「不填」更糟（调用方会拿它去校验/展示）。取不到就省略。
func mediaResponseSize(params map[string]any) string {
	for _, key := range []string{"size", "resolution"} {
		width, height, ok := parseSizePair(mapString(params, key))
		if ok {
			return strconv.Itoa(width) + "x" + strconv.Itoa(height)
		}
	}
	return ""
}

// mapString 从「已 JSON 往返」的 submit_params 里安全取字符串。
func mapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	value, _ := m[key].(string)
	return strings.TrimSpace(value)
}

// mediaArtifactURL 取产物地址：优先 video_url 列（终态写入），
// 回退 response_body 里的 artifacts[0].url。
func mediaArtifactURL(task *infra.MediaTask) string {
	if task == nil {
		return ""
	}
	if u := strings.TrimSpace(task.VideoURL); u != "" {
		return u
	}
	if payload, ok := parseMediaStatusPayload(task.ResponseBody); ok && len(payload.Artifacts) > 0 {
		return strings.TrimSpace(payload.Artifacts[0].URL)
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// 记账（C8）
// ─────────────────────────────────────────────────────────────────────────────

// mediaCompletionPlan 是终态成功时的一次性写入内容。
type mediaCompletionPlan struct {
	VideoURL     string
	ResponseBody string
	// Usage 是 jsonb 的线上表示：必须是**裸 JSON 对象**（json.RawMessage），
	// 不能用 string —— 那会被 json.Marshal 包成双层引号的 JSON 字符串，控制面按 map 绑定会 400。
	Usage        json.RawMessage
	CostMicroUSD *int64 // nil = 无价（不写 billed_at，宁可留空也不记 0 元）
	BilledAt     string // RFC3339
	CostInput    core.MediaCostInput
}

// buildCompletionUpdate 计算终态成功的用量与成本（C8）。
//
// 口径（docs/media-api.md §7 / 设计文档 §8.1）：
//   - 用量按**实际产物数 / 实际时长**，不按请求参数；
//   - 成本走 A1 的 EstimateMediaCost（与准入闸门同源价表）；
//   - 无价 → 不写 cost/billed_at（绝不静默记 0 元），只记 usage 供对账。
func (h *MediaTaskHandler) buildCompletionUpdate(task *infra.MediaTask, st *core.MediaTaskStatus) mediaCompletionPlan {
	private, _ := parseMediaPrivateData(task.PrivateData.String())
	costInput := mediaCostInputFrom(task, private, st)
	costUSD, missing := h.estimateMediaCost(private, task, costInput)

	plan := mediaCompletionPlan{
		CostInput: costInput,
		VideoURL:  firstArtifactURL(st.Artifacts),
		ResponseBody: marshalMediaStatusPayload(mediaTaskStatusPayload{
			Status:    string(core.MediaStatusCompleted),
			Progress:  st.Progress,
			Artifacts: st.Artifacts,
			Usage:     st.Usage,
		}),
	}
	if usageJSON, err := json.Marshal(mediaTaskUsagePayload{
		Vendor:      st.Usage,
		CostInput:   toMediaCostInputJSON(costInput),
		CostUSD:     costUSD,
		CostMissing: missing,
	}); err == nil {
		plan.Usage = usageJSON
	}
	if missing {
		slog.Warn("media task: 终态成功但无可计价用量/单价，不写成本（不静默记 0 元）",
			"task_id", task.TaskID, "model", task.Model,
			"images", costInput.Images, "output_seconds", costInput.OutputSeconds,
			"image_tokens", costInput.OutputImageTokens)
		return plan
	}
	micro, err := budget.MicroUSDFromFloat64(costUSD)
	if err != nil {
		slog.Error("media task: 成本转微美元失败", "task_id", task.TaskID, "cost_usd", costUSD, "error", err)
		return plan
	}
	plan.CostMicroUSD = &micro
	plan.BilledAt = h.now().UTC().Format(time.RFC3339)
	return plan
}

// estimateMediaCost 用与准入闸门同源的价表估算成本；missing=true 表示「算不出钱」。
func (h *MediaTaskHandler) estimateMediaCost(private mediaTaskPrivateData, task *infra.MediaTask, in core.MediaCostInput) (float64, bool) {
	if h.cfg.Pricing == nil || !in.HasUsage() {
		return 0, true
	}
	modelKey := strings.TrimSpace(private.UpstreamModel)
	if modelKey == "" {
		modelKey = strings.TrimSpace(task.Model)
	}
	var p core.ModelPricing
	var ok bool
	if aware, isAware := h.cfg.Pricing.(gwobs.ProviderPricingResolver); isAware {
		p, ok = aware.ResolveForProvider(modelKey, task.ProviderType)
	} else {
		p, ok = h.cfg.Pricing.Resolve(modelKey)
	}
	if !ok {
		return 0, true
	}
	return gwobs.EstimateMediaCost(in, &p)
}

// mediaCostInputFrom 从「实际产物 + 上游用量 + 原请求参数」构造计价输入。
//
// 退化链路（文档 §7）：真实用量 → 请求参数。上游给的 credits（如 Kling
// final_unit_deduction）**不是美元**，没有换算率时不参与计价，改按秒退化。
func mediaCostInputFrom(task *infra.MediaTask, private mediaTaskPrivateData, st *core.MediaTaskStatus) core.MediaCostInput {
	in := core.MediaCostInput{}
	kind := mediaTaskEffectiveKind(task, private, st)
	if kind == core.MediaKindImage {
		inputTokens := firstPositiveInt(st.Usage, "input_image_tokens", "input_tokens")
		outputTokens := firstPositiveInt(st.Usage, "output_image_tokens", "output_tokens")
		if inputTokens > 0 || outputTokens > 0 {
			// 优先 image token 口径（gpt-image 系）。
			in.InputImageTokens = inputTokens
			in.OutputImageTokens = outputTokens
			return in
		}
		images := len(st.Artifacts)
		if images == 0 {
			images = len(mediaParamImages(private.SubmitParams))
		}
		in.Images = images
		return in
	}
	seconds := firstPositiveFloat(st.Usage, "output_seconds", "duration_seconds", "duration")
	if seconds <= 0 {
		seconds = mapFloat(private.SubmitParams, "duration")
	}
	in.OutputSeconds = seconds
	return in
}

func mediaTaskEffectiveKind(task *infra.MediaTask, private mediaTaskPrivateData, st *core.MediaTaskStatus) core.MediaKind {
	if kind := core.MediaKind(strings.TrimSpace(task.Kind)); kind.IsValid() {
		return kind
	}
	if kind := core.MediaKind(strings.TrimSpace(private.Kind)); kind.IsValid() {
		return kind
	}
	if st != nil && len(st.Artifacts) > 0 && st.Artifacts[0].Kind.IsValid() {
		return st.Artifacts[0].Kind
	}
	return core.MediaKindVideo
}

func firstArtifactURL(artifacts []core.MediaArtifact) string {
	for _, artifact := range artifacts {
		if u := strings.TrimSpace(artifact.URL); u != "" {
			return u
		}
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// JSON 载荷与解析工具
// ─────────────────────────────────────────────────────────────────────────────

// mediaTaskPrivateData 是 private_data（jsonb 文本）的内容：渠道细节，**禁止返回给客户端**。
//
// 为什么需要它：媒体任务表没有 upstream_model 列，而记账（C8）必须按
// 「上游模型 + 渠道」查价表、按「原始 duration」做退化口径 —— 这两个信息在 create 时
// 才知道，因此随 private_data 一起持久化。
type mediaTaskPrivateData struct {
	Kind          string         `json:"kind,omitempty"`
	Action        string         `json:"action,omitempty"`
	UpstreamModel string         `json:"upstream_model,omitempty"`
	ProviderName  string         `json:"provider_name,omitempty"`
	ProviderType  string         `json:"provider_type,omitempty"`
	SubmitParams  map[string]any `json:"submit_params,omitempty"`
}

func marshalMediaPrivateData(data mediaTaskPrivateData) (string, error) {
	out, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseMediaPrivateData(raw string) (mediaTaskPrivateData, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return mediaTaskPrivateData{}, false
	}
	var data mediaTaskPrivateData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return mediaTaskPrivateData{}, false
	}
	return data, true
}

// mediaTaskStatusPayload 是 response_body（jsonb 文本）的内容：归一状态快照。
type mediaTaskStatusPayload struct {
	Status     string               `json:"status"`
	Progress   string               `json:"progress,omitempty"`
	Artifacts  []core.MediaArtifact `json:"artifacts,omitempty"`
	FailReason string               `json:"fail_reason,omitempty"`
	Usage      map[string]any       `json:"usage,omitempty"`
}

func marshalMediaStatusPayload(payload mediaTaskStatusPayload) string {
	out, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(out)
}

func parseMediaStatusPayload(raw string) (mediaTaskStatusPayload, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return mediaTaskStatusPayload{}, false
	}
	var payload mediaTaskStatusPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return mediaTaskStatusPayload{}, false
	}
	return payload, true
}

// mediaTaskUsagePayload 是 usage 列（jsonb 文本）的内容：用量 + 成本口径快照。
//
// 注意 CostInput 用 mediaCostInputJSON 显式转写：core.MediaCostInput 没有 json tag
// （它是跨包的内部计价结构，本轮不改动），直接序列化会写出 "OutputSeconds" 这种
// Go 字段名，而 usage 是给人/分析读的 jsonb，统一 snake_case。
type mediaTaskUsagePayload struct {
	Vendor      map[string]any     `json:"vendor,omitempty"`
	CostInput   mediaCostInputJSON `json:"cost_input"`
	CostUSD     float64            `json:"cost_usd,omitempty"`
	CostMissing bool               `json:"cost_missing,omitempty"`
}

// mediaCostInputJSON 是 core.MediaCostInput 的线上表示（snake_case，全字段无 omitempty，
// 便于对账时按固定列读取）。
type mediaCostInputJSON struct {
	Images            int     `json:"images"`
	InputImages       int     `json:"input_images"`
	InputSeconds      float64 `json:"input_seconds"`
	OutputSeconds     float64 `json:"output_seconds"`
	Requests          int     `json:"requests"`
	InputImageTokens  int     `json:"input_image_tokens"`
	OutputImageTokens int     `json:"output_image_tokens"`
}

func toMediaCostInputJSON(in core.MediaCostInput) mediaCostInputJSON {
	return mediaCostInputJSON{
		Images:            in.Images,
		InputImages:       in.InputImages,
		InputSeconds:      in.InputSeconds,
		OutputSeconds:     in.OutputSeconds,
		Requests:          in.Requests,
		InputImageTokens:  in.InputImageTokens,
		OutputImageTokens: in.OutputImageTokens,
	}
}

// rawString 从原始 JSON 对象里取字符串字段（数字/布尔会被忽略）。
func rawString(raw map[string]json.RawMessage, key string) string {
	value, ok := raw[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// readMediaTaskJSON 读取并解析创建请求体（含大小上限），同时返回原始字节。
func readMediaTaskJSON(c *gin.Context) (map[string]json.RawMessage, []byte, bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxMediaTaskBodyBytes+1))
	if err != nil {
		writeMediaTaskError(c, http.StatusBadRequest, mediaErrInvalidRequest, "请求体读取失败", "")
		return nil, nil, false
	}
	if len(body) > maxMediaTaskBodyBytes {
		writeMediaTaskError(c, http.StatusBadRequest, mediaErrInvalidRequest, "请求体过大", "")
		return nil, nil, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		writeMediaTaskError(c, http.StatusBadRequest, mediaErrInvalidRequest, "请求体不是合法 JSON 对象", "")
		return nil, nil, false
	}
	return raw, body, true
}

// mediaSubmitParams 把对外请求归一成 core.MediaSubmitRequest.Params 的**冻结键集**。
//
// ⚠️ 键名与类型是跨队冻结契约（设计文档 §6.1.1，Batch 1 补充）：
//
//	prompt       string   —— 文本提示词
//	images       []string —— **归一后的图片列表**（URL 或 data URL）
//	duration     int      —— 秒（不是字符串；厂商要的 "5" 由适配器自己转）
//	resolution   string   —— 360p/540p/720p/1080p
//	mode         string   —— std/pro
//	aspect_ratio string   —— 1:1/16:9/9:16（缺省由 size 推导）
//	size         string   —— 如 1024x1024，仅用于推导 aspect_ratio
//
// 三个刻意的取舍：
//  1. **对外 image 可单值可数组，这里一律归一到 images []string**（Vidu 的
//     first_tail_to_video 需要 2 张、reference_to_video 需要 >2 张）；
//  2. **这 7 个键恒定存在**（缺省用零值），这样适配器可以直接类型断言而不会 panic，
//     也不需要区分「键不存在」与「值为零」；适配器需把零值当「未提供」处理；
//  3. **不透传厂商私有字段**（cfg_scale / movement_amplitude / metadata 等）：
//     适配器侧是 fail-closed 的（表外键直接 400），这些开关由适配器内部写死。
//     网关也刻意不维护渠道白名单——渠道参数矩阵校验收在适配器（D3）。
func mediaSubmitParams(raw map[string]json.RawMessage) map[string]any {
	images := mediaRequestImages(raw)
	prompt := rawString(raw, "prompt")
	return map[string]any{
		"prompt":       prompt,
		"images":       images,
		"duration":     mediaRequestDuration(raw),
		"resolution":   rawString(raw, "resolution"),
		"mode":         rawString(raw, "mode"),
		"aspect_ratio": mediaRequestAspectRatio(raw),
		"size":         rawString(raw, "size"),
	}
}

// mediaRequestImages 归一输入图片：接受 image / images 的单值或数组，
// 数组元素可以是字符串（URL / data URL）或 {url: ...} 对象。顺序语义化（首尾帧）——
// **保序**，不得排序或去重。
func mediaRequestImages(raw map[string]json.RawMessage) []string {
	out := make([]string, 0, 2)
	// images 优先（显式复数），其次 image（单值或数组）。
	for _, key := range []string{"images", "image"} {
		value, ok := raw[key]
		if !ok {
			continue
		}
		out = append(out, decodeImageList(value)...)
	}
	return out
}

// decodeImageList 解析单个字段的图片值（字符串 / 字符串数组 / 对象数组）。
func decodeImageList(value json.RawMessage) []string {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	switch trimmed[0] {
	case '"':
		var single string
		if err := json.Unmarshal(value, &single); err != nil {
			return nil
		}
		if s := strings.TrimSpace(single); s != "" {
			return []string{s}
		}
		return nil
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(value, &items); err != nil {
			return nil
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s := decodeImageItem(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case '{':
		// 单对象形式：{url: "..."} / {image_url: "..."}
		if s := decodeImageItem(value); s != "" {
			return []string{s}
		}
		return nil
	default:
		return nil
	}
}

func decodeImageItem(item json.RawMessage) string {
	trimmed := strings.TrimSpace(string(item))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(item, &s); err != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	if trimmed[0] == '{' {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			return ""
		}
		for _, key := range []string{"url", "image_url", "image"} {
			if s := rawString(obj, key); s != "" {
				return s
			}
		}
	}
	return ""
}

// mediaRequestDuration 归一 duration 为**秒（int）**：接受数字或数字字符串。
func mediaRequestDuration(raw map[string]json.RawMessage) int {
	value, ok := raw["duration"]
	if !ok {
		return 0
	}
	var asFloat float64
	if err := json.Unmarshal(value, &asFloat); err == nil {
		if asFloat <= 0 {
			return 0
		}
		return int(asFloat)
	}
	var asString string
	if err := json.Unmarshal(value, &asString); err == nil {
		seconds, err := strconv.Atoi(strings.TrimSpace(asString))
		if err != nil || seconds <= 0 {
			return 0
		}
		return seconds
	}
	return 0
}

// mediaKnownAspectRatios 是冻结契约允许的宽高比取值。
var mediaKnownAspectRatios = map[string]struct{}{
	"1:1": {}, "16:9": {}, "9:16": {},
}

// mediaRequestAspectRatio 取 aspect_ratio；缺省时由 size（如 1280x720）推导。
// 推导结果不在允许集合内时不填（宁可让适配器 fail-closed，也不喂非法值）。
func mediaRequestAspectRatio(raw map[string]json.RawMessage) string {
	if declared := rawString(raw, "aspect_ratio"); declared != "" {
		return declared
	}
	size := rawString(raw, "size")
	w, h, ok := parseSizePair(size)
	if !ok {
		return ""
	}
	ratio := simplifyRatio(w, h)
	if _, allowed := mediaKnownAspectRatios[ratio]; !allowed {
		return ""
	}
	return ratio
}

func parseSizePair(size string) (int, int, bool) {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(size)), "x", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

func simplifyRatio(w, h int) string {
	divisor := gcd(w, h)
	if divisor <= 0 {
		return ""
	}
	return strconv.Itoa(w/divisor) + ":" + strconv.Itoa(h/divisor)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// mediaTaskKind 判定任务类别。POST /v1/videos 恒定是 video；
// 若请求体显式声明 kind=image（异步图像渠道），以显式声明为准。
func mediaTaskKind(raw map[string]json.RawMessage) core.MediaKind {
	if declared := core.MediaKind(rawString(raw, "kind")); declared.IsValid() {
		return declared
	}
	return core.MediaKindVideo
}

// mediaTaskAction 归一动作：**由输入图片数量决定**（冻结规则，设计文档 §6.1.1）：
//
//	0 张 → text_to_video；1 张 → image_to_video；2 张 → first_tail_to_video；>2 张 → reference_to_video。
//
// 为什么以数量为准而不是以请求里的 action 为准：数量是唯一能同时对上
// Vidu 四个上游端点与 Kling 两个端点的客观事实；请求里的 action 只做**一致性校验**
// （不一致时 400 invalid_request_error，见 Create——对外只有这一个 400 code，
// 本仓库不存在 `validation_error` 的代码路径），避免用户参数被静默吃掉。
func mediaTaskAction(images []string) core.MediaAction {
	switch len(images) {
	case 0:
		return core.MediaActionTextToVideo
	case 1:
		return core.MediaActionImageToVideo
	case 2:
		return core.MediaActionFirstTailToVideo
	default:
		return core.MediaActionReferenceToVideo
	}
}

func firstPositiveInt(m map[string]any, keys ...string) int {
	for _, key := range keys {
		if v := mapInt(m, key); v > 0 {
			return v
		}
	}
	return 0
}

func mapInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

// mediaParamImages 从「已 JSON 往返」的 submit_params 里取图片列表（[]any → []string）。
// 记账口径的退化链路用它（产物数为 0 时按输入图数量兜底）。
func mediaParamImages(params map[string]any) []string {
	if params == nil {
		return nil
	}
	switch v := params["images"].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func firstPositiveFloat(m map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if v := mapFloat(m, key); v > 0 {
			return v
		}
	}
	return 0
}

func mapFloat(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	default:
		return 0
	}
}

// taskIDOf 安全取任务 id（日志用；task 可能为 nil）。
func taskIDOf(task *infra.MediaTask) string {
	if task == nil {
		return ""
	}
	return task.TaskID
}
