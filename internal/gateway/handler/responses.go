package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/gin-gonic/gin"
)

// ResponsesHandler 处理 OpenAI Responses API 请求。
type ResponsesHandler struct {
	orchestrator     ResponsesOrchestratorInterface
	chatOrchestrator ChatOrchestratorInterface // relaykit 路径用的 Chat 编排器
	groupDispatch    *orchestrator.GroupAwareDispatcher
	budgetDeps       *middleware.BudgetDeps
	budgetGate       *budget.Gate
	requestQuota     *quota.QuotaMiddleware
	quotaGate        *quota.ModelQuotaGate // v0.15: 模型层配额检查
	protocolPolicy   *protocol.Policy      // relaykit 协议策略
}

// ResponsesOrchestratorInterface 编排器抽象，handler 仅依赖必要方法。
type ResponsesOrchestratorInterface interface {
	Responses(ctx context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error)
	ResponsesStream(ctx context.Context, req *core.ResponsesRequest) (io.ReadCloser, error)
}

// NewResponsesHandler 创建 ResponsesHandler 实例
func NewResponsesHandler(orch ResponsesOrchestratorInterface) *ResponsesHandler {
	return &ResponsesHandler{orchestrator: orch}
}

// NewResponsesHandlerWithGroup 创建支持模型组路由的 ResponsesHandler 实例
func NewResponsesHandlerWithGroup(orch ResponsesOrchestratorInterface, gd *orchestrator.GroupAwareDispatcher) *ResponsesHandler {
	return &ResponsesHandler{orchestrator: orch, groupDispatch: gd}
}

// SetChatOrchestrator 注入 Chat 编排器（relaykit 路径用）。
func (h *ResponsesHandler) SetChatOrchestrator(och ChatOrchestratorInterface) {
	h.chatOrchestrator = och
}

// SetProtocolPolicy 注入 relaykit 协议策略。
func (h *ResponsesHandler) SetProtocolPolicy(p *protocol.Policy) {
	h.protocolPolicy = p
}

// SetBudgetDeps 注入预算依赖（router setup 时调用一次）
func (h *ResponsesHandler) SetBudgetDeps(deps *middleware.BudgetDeps) {
	h.budgetDeps = deps
}

func (h *ResponsesHandler) SetBudgetGate(gate *budget.Gate) {
	h.budgetGate = gate
}

func (h *ResponsesHandler) SetRequestQuotaGate(gate *quota.QuotaMiddleware) {
	h.requestQuota = gate
}

// SetModelQuotaGate 注入模型配额检查器（router setup 时调用一次）
func (h *ResponsesHandler) SetModelQuotaGate(gate *quota.ModelQuotaGate) {
	h.quotaGate = gate
}

// Handle 处理 POST /v1/responses 请求
func (h *ResponsesHandler) Handle(c *gin.Context) {
	// 读取原始请求体，供 relaykit 路径使用
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "failed to read request body",
				"code":    "invalid_request_body",
			},
		})
		return
	}

	// 检查 relaykit 策略
	keyID, _ := c.Get("key_id")
	keyIDStr, _ := keyID.(string)
	mode := protocol.ModeLegacy
	if h.protocolPolicy != nil {
		mode = h.protocolPolicy.ModeFor(keyIDStr, "responses")
	}

	if mode == protocol.ModeActive && h.chatOrchestrator != nil {
		h.handleRelaykitPath(c, rawBody)
		return
	}

	// ── legacy 路径（默认）──
	var req core.ResponsesRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": err.Error(),
				"code":    "invalid_request_body",
			},
		})
		return
	}

	c.Set("model", req.Model)
	originalModel := req.Model
	// routedModelUUID 保存路由选定模型的 UUID，用于模型层配额键匹配
	//（dispatch 在下方闭包中执行，需把 decision 带出闭包作用域）。
	routedModelUUID := ""
	c.Set("stream", req.Stream)

	releaseQuota, err := PrepareAuthorizedGates(c, originalModel, func() error {
		if h.groupDispatch == nil {
			return nil
		}
		decision, dispatchErr := h.groupDispatch.Dispatch(c, originalModel, nil)
		if dispatchErr != nil {
			return dispatchErr
		}
		if decision != nil {
			req.Model = decision.ModelName
			req.Provider = decision.ChannelType
			routedModelUUID = decision.ModelUUID
		}
		return nil
	}, h.budgetGate, h.requestQuota)
	if err != nil {
		if c.IsAborted() {
			return
		}
		if !budget.WriteHTTPError(c, err) {
			if ue, ok := err.(*model.UnifiedError); ok {
				respondError(c, ue)
			} else {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": gin.H{"type": "unavailable", "message": "no available model in group", "code": "no_available_model"},
				})
			}
		}
		return
	}
	if releaseQuota != nil {
		defer releaseQuota()
	}

	// v0.15: 模型层配额检查（路由选定具体 model 之后、转发上游之前）
	if h.quotaGate != nil {
		subjectType, _ := c.Get("quota_subject_type")
		subjectID, _ := c.Get("quota_subject_id")
		if subjectType != nil && subjectID != nil {
			if err := h.quotaGate.BeforeUpstream(c, subjectType.(string), subjectID.(string), routedModelUUID); err != nil {
				return // 已在 middleware 中写入 429 响应
			}
		}
	}

	// shadow mode: 同时跑 relaykit 路径做对比（仅日志）
	if mode == protocol.ModeShadow && h.chatOrchestrator != nil && len(rawBody) > 0 {
		go func() {
			h.runRelaykitPath(logContext(c), rawBody)
		}()
	}

	baseCtx := orchestrator.WithGinContext(c.Request.Context(), c)
	nonStreamTimeout := h.groupTimeout(originalModel, false)
	streamTimeout := h.groupTimeout(originalModel, true)

	if req.Stream {
		h.handleStream(c, baseCtx, &req, originalModel, streamTimeout)
		return
	}
	h.handleNonStream(c, baseCtx, &req, originalModel, nonStreamTimeout)
}

// logContext 创建一个独立于请求超时的日志上下文。
func logContext(c *gin.Context) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	// runRelaykitPath 完成后其内部调用 cancel，或 30s 超时自动触发。
	go func() { <-ctx.Done(); cancel() }()
	return ctx
}

// handleRelaykitPath 处理 relaykit active 模式的 responses 请求。
// 流程：DecodeResponsesRequest → ConvertRequest → BridgeToChatRequest →
// Chat/ChatStream → BridgeChatResponseToSource / NewStreamAdapter
func (h *ResponsesHandler) handleRelaykitPath(c *gin.Context, rawBody []byte) {
	// 1. 解码为 relaykit DTO
	respReq, err := protocol.DecodeResponsesRequest(rawBody)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": err.Error(),
				"code":    "invalid_request_body",
			},
		})
		return
	}

	originalModel := respReq.Model
	c.Set("model", originalModel)
	isStream := respReq.Stream != nil && *respReq.Stream
	c.Set("stream", isStream)

	// 2. 创建 relaykit 会话
	opts := protocol.DefaultConvOptions()
	session := protocol.NewSession(
		types.RelayFormatOpenAIResponses,
		types.RelayFormatOpenAI,
		opts,
		originalModel,
		originalModel, // upstreamModel will be set after dispatch
	)

	// 3. 转换请求
	rkResult, err := protocol.ConvertRequest(session, respReq)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "protocol conversion failed: " + err.Error(),
				"code":    "conversion_error",
			},
		})
		return
	}

	// 4. 桥接为内部 ChatRequest
	chatReq, err := protocol.BridgeToChatRequest(session, rkResult)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "bridge to chat request failed: " + err.Error(),
				"code":    "bridge_error",
			},
		})
		return
	}

	// 5. 模型组调度（沿用与 chat 一致的门控）
	routedModelUUID := ""
	releaseQuota, err := PrepareAuthorizedGates(c, originalModel, func() error {
		if h.groupDispatch == nil {
			return nil
		}
		decision, dispatchErr := h.groupDispatch.Dispatch(c, originalModel, chatReq)
		if dispatchErr != nil {
			return dispatchErr
		}
		if decision != nil {
			chatReq.Model = decision.ModelName
			routedModelUUID = decision.ModelUUID
			if decision.ProviderName != "" {
				chatReq.Provider = decision.ProviderName
				middleware.TrackProviderRequest(c, decision.ProviderName)
			}
		}
		return nil
	}, h.budgetGate, h.requestQuota)
	if err != nil {
		if c.IsAborted() {
			return
		}
		if !budget.WriteHTTPError(c, err) {
			if ue, ok := err.(*model.UnifiedError); ok {
				respondError(c, ue)
			} else {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": gin.H{"type": "unavailable", "message": "no available model in group", "code": "no_available_model"},
				})
			}
		}
		return
	}
	if releaseQuota != nil {
		defer releaseQuota()
	}

	// 更新 session 上游模型名
	session.Values.UpstreamModelName = chatReq.Model

	// 6. 模型层配额检查
	if h.quotaGate != nil {
		subjectType, _ := c.Get("quota_subject_type")
		subjectID, _ := c.Get("quota_subject_id")
		if subjectType != nil && subjectID != nil {
			if err := h.quotaGate.BeforeUpstream(c, subjectType.(string), subjectID.(string), routedModelUUID); err != nil {
				return
			}
		}
	}

	baseCtx := orchestrator.WithGinContext(c.Request.Context(), c)
	nonStreamTimeout := h.groupTimeout(originalModel, false)
	streamTimeout := h.groupTimeout(originalModel, true)

	// 原生旁路：Responses → Responses 零转换。
	//
	// 位置刻意放在 PrepareAuthorizedGates 与模型配额之后：原生路径不能绕过
	// 调度/预算/配额，否则这条入口就是计费后门。
	// 只有 provider 声明支持 NativeRelayOpenAIResponses 才会命中；其余渠道
	// 返回哨兵后落到下面的 relaykit 转换（Responses→Chat→Responses）。
	if native, ok := h.chatOrchestrator.(NativeChatOrchestratorInterface); ok {
		if isStream {
			stream, nativeErr := native.ChatNativeStream(baseCtx, chatReq, rawBody, core.NativeRelayOpenAIResponses)
			if nativeErr == nil {
				c.Set("upstream_protocol", "native_responses")
				forwardNativeStream(c, stream)
				return
			}
			if !errors.Is(nativeErr, core.ErrNativeNotSupported) {
				respondNativeResponsesError(c, nativeErr)
				return
			}
		} else {
			out, nativeErr := native.ChatNative(baseCtx, chatReq, rawBody, core.NativeRelayOpenAIResponses)
			if nativeErr == nil {
				c.Set("upstream_protocol", "native_responses")
				c.Data(http.StatusOK, "application/json", out)
				return
			}
			if !errors.Is(nativeErr, core.ErrNativeNotSupported) {
				respondNativeResponsesError(c, nativeErr)
				return
			}
		}
	}

	if isStream {
		h.handleRelaykitStream(c, baseCtx, chatReq, session, originalModel, streamTimeout)
		return
	}
	h.handleRelaykitNonStream(c, baseCtx, chatReq, session, originalModel, nonStreamTimeout)
}

// respondNativeResponsesError 处理原生旁路的真实错误。
// 不降级：已经打过上游了，再走一次转换会产生第二次调用与重复计费。
func respondNativeResponsesError(c *gin.Context, err error) {
	if ue, ok := err.(*model.UnifiedError); ok {
		respondError(c, ue)
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"type":    "internal_error",
			"message": err.Error(),
			"code":    "upstream_error",
		},
	})
}

// handleRelaykitNonStream 处理 relaykit 路径的非流式响应。
func (h *ResponsesHandler) handleRelaykitNonStream(c *gin.Context, baseCtx context.Context, chatReq *core.ChatRequest, session *protocol.Session, originalModel string, timeout time.Duration) {
	// failover 循环：与 /v1/messages 的 relaykit 路径（handleMessagesRKNonStream）保持一致。
	// 缺了这段，配 policy=active 的请求反而比 legacy 更脆弱 —— 原生失败降级到 relaykit 后
	// 一次上游错误就直接返回，组内其他渠道不会被尝试。
	var lastErr error

	for {
		tctx, cancel := context.WithTimeout(baseCtx, timeout)
		chatResp, err := h.chatOrchestrator.Chat(tctx, chatReq)
		cancel()

		if err == nil {
			if lastErr != nil {
				c.Set("error_message", "")
			}
			// 桥接回源格式（OpenAI Responses）
			rkResp, rkErr := protocol.BridgeChatResponseToSource(session, chatResp)
			if rkErr == nil && rkResp != nil && rkResp.Value != nil {
				c.JSON(http.StatusOK, rkResp.Value)
				return
			}
			// 桥接失败则回退到直接返回 ChatResponse（保持可用性）
			slog.Warn("responses relaykit: bridge to source failed, falling back to chat response",
				"request_id", c.GetString("request_id"),
				"error", rkErr,
			)
			c.JSON(http.StatusOK, chatResp)
			return
		}
		lastErr = err

		if h.groupDispatch == nil || !c.GetBool("routed_via_group") || !shouldTriggerFailover(err) {
			break
		}

		failedID := chatReq.Model
		if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
			failedID = dc.RoutedModelID
		}
		slog.Warn("responses failover: attempt (rk)",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"failed_model", chatReq.Model,
			"failed_model_id", failedID,
			"error", err.Error(),
		)

		h.groupDispatch.NotifyFailure(failedID)
		if is429Error(err) {
			h.groupDispatch.SetCooldown(failedID, defaultCooldownDuration)
		}

		decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID, chatReq)
		if fErr != nil {
			slog.Error("responses failover: no available target (rk)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"error", fErr.Error(),
			)
			break
		}
		if decision != nil {
			chatReq.Model = decision.ModelName
			chatReq.Provider = decision.ChannelType
			c.Set("failover_used", true)
			slog.Info("responses failover: switching (rk)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"target", decision.ModelName,
			)
		}
	}

	if ue, ok := lastErr.(*model.UnifiedError); ok {
		respondError(c, ue)
		return
	}
	respondError(c, model.ErrInternal)
}

// handleRelaykitStream 处理 relaykit 路径的流式响应。
func (h *ResponsesHandler) handleRelaykitStream(c *gin.Context, baseCtx context.Context, chatReq *core.ChatRequest, session *protocol.Session, originalModel string, timeout time.Duration) {
	// 与 handleRelaykitNonStream 同构：建立流失败时可以换渠道重试；
	// 一旦流已建立就不再做 failover（客户端已经收到首字节，切换等于拼两个响应）。
	openStream := func(r *core.ChatRequest) (io.ReadCloser, context.CancelFunc, error) {
		streamCtx, cancel := context.WithTimeout(baseCtx, timeout)
		s, e := h.chatOrchestrator.ChatStream(streamCtx, r)
		if e != nil {
			cancel()
			return nil, nil, e
		}
		return s, cancel, nil
	}

	var lastErr error

	for {
		stream, cancel, err := openStream(chatReq)

		if err == nil {
			if lastErr != nil {
				c.Set("error_message", "")
			}
			// 使用 relaykit StreamAdapter 转换流
			responseID := c.GetString("request_id")
			adapted := protocol.NewStreamAdapter(
				&cancelOnClose{ReadCloser: stream, cancel: cancel},
				session,
				chatReq.Model,
				estimatePromptTokens(chatReq.Messages),
				responseID,
			)
			HandleStreamResponse(c, adapted)
			return
		}
		lastErr = err

		if h.groupDispatch == nil || !c.GetBool("routed_via_group") || !shouldTriggerFailover(err) {
			break
		}

		failedID := chatReq.Model
		if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
			failedID = dc.RoutedModelID
		}
		slog.Warn("responses failover: attempt (rk stream)",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"failed_model", chatReq.Model,
			"failed_model_id", failedID,
			"error", err.Error(),
		)

		h.groupDispatch.NotifyFailure(failedID)
		if is429Error(err) {
			h.groupDispatch.SetCooldown(failedID, defaultCooldownDuration)
		}

		decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID, chatReq)
		if fErr != nil {
			slog.Error("responses failover: no available target (rk stream)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"error", fErr.Error(),
			)
			break
		}
		if decision != nil {
			chatReq.Model = decision.ModelName
			chatReq.Provider = decision.ChannelType
			c.Set("failover_used", true)
			slog.Info("responses failover: switching (rk stream)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"target", decision.ModelName,
			)
		}
	}

	if ue, ok := lastErr.(*model.UnifiedError); ok {
		respondError(c, ue)
		return
	}
	respondError(c, model.ErrInternal)
}

// runRelaykitPath 在 shadow 模式下异步运行 relaykit 路径并记录结果（仅日志）。
func (h *ResponsesHandler) runRelaykitPath(ctx context.Context, rawBody []byte) {
	respReq, err := protocol.DecodeResponsesRequest(rawBody)
	if err != nil {
		slog.Warn("responses shadow: decode failed", "error", err)
		return
	}

	opts := protocol.DefaultConvOptions()
	session := protocol.NewSession(
		types.RelayFormatOpenAIResponses,
		types.RelayFormatOpenAI,
		opts,
		respReq.Model,
		respReq.Model,
	)

	rkResult, err := protocol.ConvertRequest(session, respReq)
	if err != nil {
		slog.Warn("responses shadow: convert failed", "error", err)
		return
	}

	chatReq, err := protocol.BridgeToChatRequest(session, rkResult)
	if err != nil {
		slog.Warn("responses shadow: bridge failed", "error", err)
		return
	}

	// 不对 shadow 路径做实际的调度和上游调用，仅验证转换链路能走通。
	_ = chatReq
	if len(session.Diagnostics) > 0 {
		diags := protocol.JoinDiagnostics(session, rkResult, session.Diagnostics)
		slog.Info("responses shadow: conversion diagnostics", "diagnostics", diags)
	}
}

// estimateResponsesInputChars 估算 Responses API 的 input 字符数。
// ResponsesRequest.Input 是 any，可能是 string 或结构化数据。
func estimateResponsesInputChars(req *core.ResponsesRequest) int {
	if req == nil || req.Input == nil {
		return 0
	}
	switch v := req.Input.(type) {
	case string:
		return len(v)
	default:
		b, _ := json.Marshal(v)
		return len(b)
	}
}

func (h *ResponsesHandler) handleNonStream(c *gin.Context, baseCtx context.Context, req *core.ResponsesRequest, originalModel string, timeout time.Duration) {
	tctx, cancel := context.WithTimeout(baseCtx, timeout)
	resp, err := h.orchestrator.Responses(tctx, req)
	cancel()

	if err == nil {
		c.JSON(http.StatusOK, resp)
		return
	}

	// failover（仅在通过 group routing 时尝试）
	if h.groupDispatch != nil && c.GetBool("routed_via_group") {
		failedID := req.Model
		if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
			failedID = dc.RoutedModelID
		}
		// /v1/responses 没有 core.ChatRequest，视频约束必须显式传入：
		// 否则 failover 会换到不支持视频的成员，视频被静默丢弃。
		if decision, fErr := h.groupDispatch.HandleFailoverVideo(c, originalModel, failedID, core.HasVideoInput(req.Input)); fErr == nil {
			req.Model = decision.ModelName
			req.Provider = decision.ChannelType
			c.Set("failover_used", true)
			slog.Info("responses: failover switching",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"target", decision.ModelName,
			)
			ftctx, fcancel := context.WithTimeout(baseCtx, timeout)
			fResp, fErr2 := h.orchestrator.Responses(ftctx, req)
			fcancel()
			if fErr2 == nil {
				c.Set("error_message", "")
				c.JSON(http.StatusOK, fResp)
				return
			}
		}
	}

	if ue, ok := err.(*model.UnifiedError); ok {
		respondError(c, ue)
		return
	}
	respondError(c, model.ErrInternal)
}

func (h *ResponsesHandler) handleStream(c *gin.Context, baseCtx context.Context, req *core.ResponsesRequest, originalModel string, timeout time.Duration) {
	open := func(r *core.ResponsesRequest) (io.ReadCloser, context.CancelFunc, error) {
		tctx, cancel := context.WithTimeout(baseCtx, timeout)
		s, e := h.orchestrator.ResponsesStream(tctx, r)
		if e != nil {
			cancel()
			return nil, nil, e
		}
		return s, cancel, nil
	}

	stream, cancel, err := open(req)

	if err != nil {
		// failover（仅在通过 group routing 时尝试）
		if h.groupDispatch != nil && c.GetBool("routed_via_group") {
			failedID := req.Model
			if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
				failedID = dc.RoutedModelID
			}
			if decision, fErr := h.groupDispatch.HandleFailoverVideo(c, originalModel, failedID, core.HasVideoInput(req.Input)); fErr == nil {
				req.Model = decision.ModelName
				req.Provider = decision.ChannelType
				c.Set("failover_used", true)
				slog.Info("responses: stream failover switching",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"target", decision.ModelName,
				)
				fStream, fCancel, fErr2 := open(req)
				if fErr2 == nil {
					c.Set("error_message", "")
					HandleStreamResponse(c, &cancelOnClose{ReadCloser: fStream, cancel: fCancel})
					return
				}
			}
		}
		if ue, ok := err.(*model.UnifiedError); ok {
			respondError(c, ue)
			return
		}
		respondError(c, model.ErrInternal)
		return
	}

	HandleStreamResponse(c, &cancelOnClose{ReadCloser: stream, cancel: cancel})
}

// groupTimeout 获取组的超时配置，未配置时使用默认值。
func (h *ResponsesHandler) groupTimeout(modelName string, stream bool) time.Duration {
	def := defaultNonStreamTimeout
	if stream {
		def = defaultStreamTimeout
	}
	if h.groupDispatch != nil {
		return h.groupDispatch.GetGroupTimeout(modelName, def)
	}
	return def
}
