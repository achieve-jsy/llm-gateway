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
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
)

const (
	// defaultNonStreamTimeout 非流式请求每轮上游超时（含 failover 每轮），对齐 LiteLLM 默认值。
	// 推理类模型（DeepSeek R1、o1 等）单次响应可能超过 30s，保守值过小会导致误 failover。
	defaultNonStreamTimeout = 600 * time.Second
	// defaultStreamTimeout 流式请求总超时，需与 Nginx proxy_read_timeout 保持一致。
	defaultStreamTimeout = 600 * time.Second
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
	quotaGate     *quota.ModelQuotaGate  // v0.15: 模型层配额检查（nil 表示跳过）
}

// NewChatHandler 创建 ChatHandler 实例
func NewChatHandler(orch ChatOrchestratorInterface) *ChatHandler {
	return &ChatHandler{orchestrator: orch}
}

// NewChatHandlerWithGroup 创建带模型组调度的 ChatHandler 实例
func NewChatHandlerWithGroup(orch ChatOrchestratorInterface, gd *orchestrator.GroupAwareDispatcher) *ChatHandler {
	return &ChatHandler{orchestrator: orch, groupDispatch: gd}
}

// SetBudgetDeps 注入预算依赖（router setup 时调用一次）
func (h *ChatHandler) SetBudgetDeps(deps *middleware.BudgetDeps) {
	h.budgetDeps = deps
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
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": err.Error(),
				"code":    "invalid_request_body",
			},
		})
		return
	}

	originalModel := req.Model
	c.Set("model", req.Model)

	// v0.9: 预算检查（事后记账模式，只检查已累计用量，不估算本次成本）
	if h.budgetDeps != nil {
		if _, err := middleware.Reserve(c, h.budgetDeps); err != nil {
			if be, ok := err.(*middleware.BudgetExceededError); ok {
				be.WriteHTTPResponse(c)
				return
			}
			slog.Warn("chat: budget check failed", "model", req.Model, "error", err)
		}
	}

	if h.groupDispatch != nil {
		decision, err := h.groupDispatch.Dispatch(c, req.Model, &req)
		if err != nil {
			h.respondGroupError(c, err)
			return
		}
		if decision != nil {
			req.Model = decision.ModelName
			// 传递 provider hint，确保 Router 在同名模型多 provider 场景下
			// 选择 scheduler 指定的 provider，避免路由歧义。
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
			}
			if decision.Strategy == scheduler.StrategyAuto && decision.ComplexityTier != "" {
				c.Header("X-Auto-Tier", decision.ComplexityTier)
				c.Header("X-Auto-Score", fmt.Sprintf("%.2f", decision.ComplexityScore))
			}
			
			// v0.15: 模型层配额检查（在路由选定具体 model 之后、转发上游之前）
			if h.quotaGate != nil {
				subjectType, _ := c.Get("quota_subject_type")
				subjectID, _ := c.Get("quota_subject_id")
				if st, ok := subjectType.(string); ok {
					if si, ok := subjectID.(string); ok {
						if err := h.quotaGate.BeforeUpstream(c, st, si, decision.ModelID); err != nil {
							return // BeforeUpstream 已写入 429 响应
						}
					}
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
// 轮次不受前一轮超时消耗的影响。多轮 failover 由 HandleFailover 内部的
// DispatchContext 控制上限（最多 maxFailoverRounds 轮）。
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
		if h.groupDispatch == nil || req.Model == originalModel || !shouldTriggerFailover(err) {
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
		c.Set("failover_used", true)
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

		if h.groupDispatch == nil || req.Model == originalModel || !shouldTriggerFailover(err) {
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
		c.Set("failover_used", true)
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

func (h *ChatHandler) respondGroupError(c *gin.Context, err error) {
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

// respondError 返回统一错误响应 JSON
func respondError(c *gin.Context, ue *model.UnifiedError) {
	body := gin.H{
		"error": gin.H{
			"type":    ue.Type,
			"code":    ue.Code,
			"message": ue.Message,
		},
	}
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
