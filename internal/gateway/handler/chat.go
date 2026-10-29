package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	gatewayerrors "github.com/example/llm-gateway/internal/errors"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
)

const (
	// defaultNonStreamTimeout 非流式请求每轮上游超时（含 failover 每轮）。
	// 推理类模型（DeepSeek R1、o1 等）单次响应可能超过 30s，保守值过小会导致误 failover；
	// 但过大会让并发槽位被慢/挂起请求长期占用（详见 concurrency 泄漏修复），故收敛到 300s。
	defaultNonStreamTimeout = 300 * time.Second
	// defaultStreamTimeout 流式请求总超时，需与 Nginx proxy_read_timeout 保持一致。
	// 同样收敛到 300s，避免长流占住并发槽位达 10 分钟。
	defaultStreamTimeout = 300 * time.Second
	// defaultCooldownDuration 429 限流后默认冷却时长（无 Retry-After 时使用）。
	defaultCooldownDuration = 60 * time.Second
)

// ChatOrchestratorInterface 编排器抽象：handler 仅依赖必要方法，便于测试替换
// 实现者：internal/gateway/orchestrator.ChatOrchestrator
type ChatOrchestratorInterface interface {
	Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error)
	ChatStream(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error)
}

// ChatHandler 聊天请求处理器
type ChatHandler struct {
	orchestrator  ChatOrchestratorInterface
	groupDispatch *orchestrator.GroupAwareDispatcher
	budgetDeps    *middleware.BudgetDeps // v0.7: 预算预扣依赖（nil 表示跳过预算校验）
	budgetGate    *budget.Gate
	requestQuota  *quota.QuotaMiddleware
	quotaGate     *quota.ModelQuotaGate // v0.15: 模型层配额检查（nil 表示跳过）
	// maxInlineMediaBytes 含内联媒体（data: URL 视频）的请求体上限。
	// 内联 base64 视频会把请求体撑到几十 MB，既拖慢网关又要经过整条
	// 鉴权/配额链路，所以在 bind 之后立刻拦截，引导客户端改用 http(s) URL。
	maxInlineMediaBytes int64
}

// defaultMaxInlineMediaBytes 内联媒体请求体默认上限（10MiB）。
// 视频帧 base64 后膨胀约 1.37 倍，10MiB 大约够几秒钟的低分辨率片段；
// 更长的视频应当走模型厂商的文件/URL 接口。
const defaultMaxInlineMediaBytes int64 = 10 << 20

// NewChatHandler 创建 ChatHandler 实例
func NewChatHandler(orch ChatOrchestratorInterface) *ChatHandler {
	return &ChatHandler{orchestrator: orch, maxInlineMediaBytes: defaultMaxInlineMediaBytes}
}

// NewChatHandlerWithGroup 创建带模型组调度的 ChatHandler 实例
func NewChatHandlerWithGroup(orch ChatOrchestratorInterface, gd *orchestrator.GroupAwareDispatcher) *ChatHandler {
	return &ChatHandler{orchestrator: orch, groupDispatch: gd, maxInlineMediaBytes: defaultMaxInlineMediaBytes}
}

// SetMaxInlineMediaBytes 覆盖内联媒体请求体上限（<=0 表示不限制）。
func (h *ChatHandler) SetMaxInlineMediaBytes(n int64) {
	h.maxInlineMediaBytes = n
}

// SetBudgetDeps 注入预算依赖（router setup 时调用一次）
func (h *ChatHandler) SetBudgetDeps(deps *middleware.BudgetDeps) {
	h.budgetDeps = deps
}

func (h *ChatHandler) SetBudgetGate(gate *budget.Gate) {
	h.budgetGate = gate
}

func (h *ChatHandler) SetRequestQuotaGate(gate *quota.QuotaMiddleware) {
	h.requestQuota = gate
}

// SetModelQuotaGate 注入模型配额检查器（router setup 时调用一次）
func (h *ChatHandler) SetModelQuotaGate(gate *quota.ModelQuotaGate) {
	h.quotaGate = gate
}

// Handle 处理 POST /v1/chat/completions 请求
// 解析请求 → 判断普通/流式 → 分发处理
//
//	@Summary		Chat Completions
//	@Description	创建聊天补全，兼容 OpenAI /v1/chat/completions 接口
//	@Tags			Chat
//	@Accept			json
//	@Produce		json
//	@Param			request	body		core.ChatRequest	true	"聊天补全请求"
//	@Success		200		{object}	core.ChatResponse	"成功响应"
//	@Failure		400		{object}	map[string]interface{}	"请求参数错误"
//	@Failure		401		{object}	map[string]interface{}	"认证失败"
//	@Failure		404		{object}	map[string]interface{}	"模型不存在"
//	@Failure		500		{object}	map[string]interface{}	"内部错误"
//	@Security		BearerAuth
//	@Router			/v1/chat/completions [post]
func (h *ChatHandler) Handle(c *gin.Context) {
	var req core.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 内容片段错误单独成码：客户端能定位到 content[N] 的哪一段、哪个字段，
		// 而不是只得到笼统的 request_body_invalid。
		var partErr *core.ContentPartError
		if errors.As(err, &partErr) {
			// Index 未知（-1）时不拼 content[-1]，退化成不带下标的定位。
			param := "content.type"
			if partErr.Index >= 0 {
				param = fmt.Sprintf("content[%d].type", partErr.Index)
			}
			if partErr.Field != "" {
				if partErr.Index >= 0 {
					param = fmt.Sprintf("content[%d].%s", partErr.Index, partErr.Field)
				} else {
					param = fmt.Sprintf("content.%s", partErr.Field)
				}
			}
			gatewayerrors.WriteError(c, "unsupported_content_part",
				gatewayerrors.WithMessage(err.Error()),
				gatewayerrors.WithParam(param))
			return
		}
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage(err.Error()))
		return
	}

	// 内联媒体体积闸门：放在鉴权/配额/路由之前，超大 base64 视频不必走完整链路。
	// 只对含视频片段的请求生效——纯文本超长属于另一层限制（上游 context 校验）。
	if h.maxInlineMediaBytes > 0 &&
		c.Request.ContentLength > h.maxInlineMediaBytes &&
		core.HasVideoContent(req.Messages) {
		gatewayerrors.WriteError(c, "inline_media_too_large",
			gatewayerrors.WithMessage(fmt.Sprintf(
				"内联媒体体积 %d bytes 超过上限 %d bytes，请改用 http(s) 视频 URL（如 video_url.url 指向可公网访问的 mp4）",
				c.Request.ContentLength, h.maxInlineMediaBytes)))
		return
	}

	originalModel := req.Model
	c.Set("model", req.Model)

	var decision *scheduler.RouteDecision
	releaseQuota, err := PrepareAuthorizedGates(c, originalModel, func() error {
		if h.groupDispatch == nil {
			return nil
		}
		var dispatchErr error
		decision, dispatchErr = h.groupDispatch.Dispatch(c, originalModel, &req)
		if dispatchErr != nil {
			return dispatchErr
		}
		if decision != nil {
			req.Model = decision.ModelName
			// 传递 provider hint，确保 Router 在同名模型多 provider 场景下
			// 选择 scheduler 指定的 provider，避免路由歧义。
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
				// Track provider concurrency in Redis (WP8)
				middleware.TrackProviderRequest(c, decision.ProviderName)
			}
			if decision.Strategy == scheduler.StrategyAuto && decision.ComplexityTier != "" {
				c.Header("X-Auto-Tier", decision.ComplexityTier)
				c.Header("X-Auto-Score", fmt.Sprintf("%.2f", decision.ComplexityScore))
			}

		}
		return nil
	}, h.budgetGate, h.requestQuota)
	if err != nil {
		if c.IsAborted() {
			return
		}
		if !budget.WriteHTTPError(c, err) {
			respondGroupError(c, err)
		}
		return
	}
	if releaseQuota != nil {
		defer releaseQuota()
	}

	if decision != nil && h.quotaGate != nil {
		subjectType, _ := c.Get("quota_subject_type")
		subjectID, _ := c.Get("quota_subject_id")
		if st, ok := subjectType.(string); ok {
			if si, ok := subjectID.(string); ok {
				if err := h.quotaGate.BeforeUpstream(c, st, si, decision.ModelUUID); err != nil {
					return
				}
			}
		}
	}

	// 将 Gin context 注入请求 context，供 orchestrator 写回 provider/token 信息。
	// baseCtx 不设超时，由各 handle 函数按轮次独立创建超时子 context。
	baseCtx := orchestrator.WithGinContext(c.Request.Context(), c)

	if req.Stream {
		h.handleStream(c, baseCtx, &req, originalModel)
		return
	}

	h.handleNonStream(c, baseCtx, &req, originalModel)
}

// nonStreamTimeout 返回当前请求适用的非流式超时：优先使用组级配置，否则使用系统默认值。
func (h *ChatHandler) nonStreamTimeout(originalModel string) time.Duration {
	if h.groupDispatch != nil {
		return h.groupDispatch.GetGroupTimeout(originalModel, defaultNonStreamTimeout)
	}
	return defaultNonStreamTimeout
}

// streamTimeout 返回当前请求适用的流式超时：优先使用组级配置，否则使用系统默认值。
func (h *ChatHandler) streamTimeout(originalModel string) time.Duration {
	if h.groupDispatch != nil {
		return h.groupDispatch.GetGroupTimeout(originalModel, defaultStreamTimeout)
	}
	return defaultStreamTimeout
}

// handleNonStream 处理普通（非流式）请求。
//
// 每轮上游调用都从 baseCtx 派生独立超时 context（完整预算），确保 failover
// 轮次不受前一轮超时消耗的影响。failover 轮次由 triedIDs 自然收束：
// 每轮失败的模型加入排除列表，全部模型试过后 HandleFailover 返回错误。
func (h *ChatHandler) handleNonStream(c *gin.Context, baseCtx context.Context, req *core.ChatRequest, originalModel string) {
	timeout := h.nonStreamTimeout(originalModel)
	var lastErr error

	for {
		roundCtx, roundCancel := context.WithTimeout(baseCtx, timeout)
		resp, err := h.orchestrator.Chat(roundCtx, req)
		roundCancel()

		if err == nil {
			if lastErr != nil {
				c.Set("error_message", "") // failover 成功，清除前轮错误
			}
			c.JSON(http.StatusOK, resp)
			return
		}
		lastErr = err

		// 不触发 failover 的条件：无 groupDispatch、未路由到组、错误类型不可 failover
		if h.groupDispatch == nil || !c.GetBool("routed_via_group") || !shouldTriggerFailover(err) {
			break
		}

		failedID := req.Model
		if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
			failedID = dc.RoutedModelID
		}

		slog.Warn("failover: attempt",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"failed_model", req.Model,
			"failed_model_id", failedID,
			"error", err.Error(),
		)

		// Phase 3: 实时通知健康状态 + 429 冷却
		h.groupDispatch.NotifyFailure(failedID)
		if is429Error(err) {
			h.groupDispatch.SetCooldown(failedID, defaultCooldownDuration)
		}

		decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID, req)
		if fErr != nil {
			slog.Error("failover: no available target",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"error", fErr.Error(),
			)
			break
		}

		req.Model = decision.ModelName
		req.Provider = decision.ProviderName
		// Track provider concurrency for failover target (WP8)
		middleware.TrackProviderRequest(c, decision.ProviderName)
		c.Set("failover_used", true)

		// Update sticky mapping after successful failover
		dc := model.GetDispatchContext(c)
		if dc != nil && dc.SessionKey != "" {
			h.groupDispatch.UpdateSticky(c.Request.Context(), originalModel, dc.SessionKey, decision.ModelName, decision.ProviderName)
		}

		// v0.15: failover 模型也需要经过模型层配额检查
		if h.quotaGate != nil {
			subjectType, _ := c.Get("quota_subject_type")
			subjectID, _ := c.Get("quota_subject_id")
			if st, ok := subjectType.(string); ok {
				if si, ok := subjectID.(string); ok {
					if err := h.quotaGate.BeforeUpstream(c, st, si, decision.ModelUUID); err != nil {
						return // BeforeUpstream 已写入 429 响应
					}
				}
			}
		}

		slog.Info("failover: switching",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"target_provider", decision.ProviderName,
			"target_model", decision.ModelName,
			"target_model_id", decision.ModelID,
		)
	}

	// 所有轮次均失败
	if ue, ok := lastErr.(*model.UnifiedError); ok {
		respondError(c, ue)
		return
	}
	respondError(c, model.ErrInternal)
}

// handleStream 处理流式请求。
//
// 流式场景下，若 primary 建立连接失败才可 failover；一旦流已建立则不中途切换。
// 多轮 failover 逻辑与 handleNonStream 一致。
func (h *ChatHandler) handleStream(c *gin.Context, baseCtx context.Context, req *core.ChatRequest, originalModel string) {
	timeout := h.streamTimeout(originalModel)

	// openStream 封装"建立一次流"的逻辑：派生超时 ctx，连接失败时立即 cancel。
	// 连接成功时返回 cancel 与 stream，调用方需确保最终调用 cancel。
	openStream := func(r *core.ChatRequest) (io.ReadCloser, context.CancelFunc, error) {
		streamCtx, cancel := context.WithTimeout(baseCtx, timeout)
		s, e := h.orchestrator.ChatStream(streamCtx, r)
		if e != nil {
			cancel()
			return nil, nil, e
		}
		return s, cancel, nil
	}

	var lastErr error

	for {
		stream, cancel, err := openStream(req)

		if err == nil {
			if lastErr != nil {
				c.Set("error_message", "")
			}
			HandleStreamResponse(c, &cancelOnClose{ReadCloser: stream, cancel: cancel})
			return
		}
		lastErr = err

		if h.groupDispatch == nil || !c.GetBool("routed_via_group") || !shouldTriggerFailover(err) {
			break
		}

		failedID := req.Model
		if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
			failedID = dc.RoutedModelID
		}

		slog.Warn("failover: attempt (stream)",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"failed_model", req.Model,
			"failed_model_id", failedID,
			"error", err.Error(),
		)

		h.groupDispatch.NotifyFailure(failedID)
		if is429Error(err) {
			h.groupDispatch.SetCooldown(failedID, defaultCooldownDuration)
		}

		decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID, req)
		if fErr != nil {
			slog.Error("failover: no available target (stream)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"error", fErr.Error(),
			)
			break
		}

		req.Model = decision.ModelName
		req.Provider = decision.ProviderName
		// Track provider concurrency for failover target (WP8)
		middleware.TrackProviderRequest(c, decision.ProviderName)
		c.Set("failover_used", true)

		// Update sticky mapping after successful failover
		dc := model.GetDispatchContext(c)
		if dc != nil && dc.SessionKey != "" {
			h.groupDispatch.UpdateSticky(c.Request.Context(), originalModel, dc.SessionKey, decision.ModelName, decision.ProviderName)
		}

		// v0.15: failover 模型也需要经过模型层配额检查
		if h.quotaGate != nil {
			subjectType, _ := c.Get("quota_subject_type")
			subjectID, _ := c.Get("quota_subject_id")
			if st, ok := subjectType.(string); ok {
				if si, ok := subjectID.(string); ok {
					if err := h.quotaGate.BeforeUpstream(c, st, si, decision.ModelUUID); err != nil {
						return // BeforeUpstream 已写入 429 响应
					}
				}
			}
		}

		slog.Info("failover: switching (stream)",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"target_provider", decision.ProviderName,
			"target_model", decision.ModelName,
			"target_model_id", decision.ModelID,
		)
	}

	if ue, ok := lastErr.(*model.UnifiedError); ok {
		respondError(c, ue)
		return
	}
	respondError(c, model.ErrInternal)
}

// shouldTriggerFailover 判断是否应根据错误类型触发 failover。
// 上游 5xx/429/网络错误可 failover；参数错误（400）、鉴权（401/403）、
// 内容过长（413）、context 取消不触发 failover。
func shouldTriggerFailover(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var gwErr *core.GatewayError
	if !errors.As(err, &gwErr) {
		return true // 未知错误类型保守触发
	}
	switch gwErr.StatusCode {
	case http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusRequestEntityTooLarge:
		return false
	default:
		return true
	}
}

// is429Error 判断错误是否为上游 429 限流错误。
func is429Error(err error) bool {
	var gwErr *core.GatewayError
	return errors.As(err, &gwErr) && gwErr.StatusCode == http.StatusTooManyRequests
}

// cancelOnClose 包装 io.ReadCloser，Close 时调用 cancel 释放超时 context
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (w *cancelOnClose) Close() error {
	w.cancel()
	return w.ReadCloser.Close()
}

// respondGroupError 将组调度错误映射为合适的 HTTP 错误响应。
func respondGroupError(c *gin.Context, err error) {
	if err == model.ErrGroupNotFound {
		respondError(c, &model.UnifiedError{
			Type:       model.ErrorTypeModelNotFound,
			Code:       "group_not_found",
			Message:    err.Error(),
			HTTPStatus: http.StatusNotFound,
		})
		return
	}
	if err == model.ErrNoAvailableModel {
		respondError(c, model.ErrModelUnavailable)
		return
	}
	if err == model.ErrNoOtherModelAvailable {
		respondError(c, model.ErrModelUnavailable)
		return
	}
	if ue, ok := err.(*model.UnifiedError); ok {
		respondError(c, ue)
		return
	}
	respondError(c, model.ErrInternal)
}

// respondError 返回统一错误响应 JSON。
// 当 X-Error-Format: passthrough 请求头存在时，直接透传上游错误，不做归一化。
func respondError(c *gin.Context, ue *model.UnifiedError) {
	if IsPassthroughMode(c) {
		writePassthroughError(c, ue)
		return
	}
	errObj := gin.H{
		"type":    ue.Type,
		"code":    ue.Code,
		"message": ue.Message,
	}
	if ue.Upstream != nil {
		errObj["upstream"] = ue.Upstream
	}
	body := gin.H{"error": errObj}
	middleware.SetTraceResponseObject(c, body)
	c.JSON(ue.HTTPStatus, body)
}

// estimateChatPromptChars 粗略估算 ChatRequest 的 prompt 字符数，用于预算预扣。
// 算法：对每条 Message 的 Content JSON 序列化后取 byte 数累加。
// 粗略但够用于 tokens 估算（英文 ~1 token per 4 chars）。
func estimateChatPromptChars(req *core.ChatRequest) int {
	if req == nil || len(req.Messages) == 0 {
		return 0
	}
	total := 0
	for _, m := range req.Messages {
		// 序列化 Content 取字符数（Content 是 any，可能是 string 也可能是结构化）
		b, err := json.Marshal(m.Content)
		if err == nil {
			total += len(b)
		}
	}
	return total
}
