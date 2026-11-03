package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/example/llm-gateway/internal/core"
	gatewayerrors "github.com/example/llm-gateway/internal/errors"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/gin-gonic/gin"
)

// MessagesHandler handles Anthropic-compatible POST /v1/messages requests.
// It parses the Anthropic Messages API request, converts it to the internal
// ChatRequest (OpenAI format), dispatches through the ChatOrchestrator,
// then converts the response back to Anthropic Messages API format.
type MessagesHandler struct {
	orchestrator   ChatOrchestratorInterface
	groupDispatch  *orchestrator.GroupAwareDispatcher
	budgetDeps     *middleware.BudgetDeps
	budgetGate     *budget.Gate
	requestQuota   *quota.QuotaMiddleware
	quotaGate      *quota.ModelQuotaGate // v0.15: 模型层配额检查
	protocolPolicy *protocol.Policy      // relaykit 协议转换策略
}

// NewMessagesHandler creates a MessagesHandler.
func NewMessagesHandler(orch ChatOrchestratorInterface) *MessagesHandler {
	return &MessagesHandler{orchestrator: orch}
}

// NewMessagesHandlerWithGroup creates a MessagesHandler with group dispatch support.
func NewMessagesHandlerWithGroup(orch ChatOrchestratorInterface, gd *orchestrator.GroupAwareDispatcher) *MessagesHandler {
	return &MessagesHandler{orchestrator: orch, groupDispatch: gd}
}

// SetBudgetDeps 注入预算依赖（router setup 时调用一次）
func (h *MessagesHandler) SetBudgetDeps(deps *middleware.BudgetDeps) {
	h.budgetDeps = deps
}

func (h *MessagesHandler) SetBudgetGate(gate *budget.Gate) {
	h.budgetGate = gate
}

func (h *MessagesHandler) SetRequestQuotaGate(gate *quota.QuotaMiddleware) {
	h.requestQuota = gate
}

// SetModelQuotaGate 注入模型配额检查器（router setup 时调用一次）
func (h *MessagesHandler) SetModelQuotaGate(gate *quota.ModelQuotaGate) {
	h.quotaGate = gate
}

// SetProtocolPolicy 注入 relaykit 协议策略。
func (h *MessagesHandler) SetProtocolPolicy(p *protocol.Policy) {
	h.protocolPolicy = p
}

// Handle processes POST /v1/messages requests.
func (h *MessagesHandler) HandleMessages(c *gin.Context) {
	// relaykit 协议路径分流：active/shadow 走新转换器，legacy 保持原有实现。
	// 必须在 ShouldBindJSON 之前取原始 body，否则新路径拿不到被 core 结构裁剪掉的字段。
	if h.protocolPolicy != nil {
		keyID := c.GetString("key_id")
		mode := h.protocolPolicy.ModeFor(keyID, "messages")
		if mode == protocol.ModeActive || mode == protocol.ModeShadow {
			rawBody, readErr := io.ReadAll(c.Request.Body)
			if readErr != nil {
				h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "invalid_request_body", readErr.Error(), nil)
				return
			}
			if mode == protocol.ModeActive {
				h.handleMessagesRK(c, rawBody)
				return
			}
			// shadow：只离线比对转换结果，客户端仍拿 legacy 输出。
			h.handleMessagesRKShadow(c, rawBody)
			c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))
		}
	}

	var anthReq core.AnthropicMessagesRequest
	if err := c.ShouldBindJSON(&anthReq); err != nil {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "invalid_request_body", err.Error(), nil)
		return
	}

	if anthReq.Model == "" {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "model_required", "model is required", nil)
		return
	}

	if len(anthReq.Messages) == 0 {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "messages_required", "messages is required and must not be empty", nil)
		return
	}

	c.Set("model", anthReq.Model)

	// Convert Anthropic request to internal ChatRequest
	req, convErr := core.AnthropicToChatRequest(&anthReq)
	if convErr != nil {
		// 协议不支持的内容（如视频片段）在这里就拒绝，不要带着被丢弃的内容继续转发。
		h.respondAnthropicConversionError(c, convErr)
		return
	}

	originalModel := anthReq.Model

	// 记录路由选定的模型 ID，用于模型层配额检查。
	routedModelID := anthReq.Model

	releaseQuota, err := PrepareAuthorizedGates(c, originalModel, func() error {
		if h.groupDispatch == nil {
			return nil
		}
		decision, dispatchErr := h.groupDispatch.Dispatch(c, originalModel, req)
		if dispatchErr != nil {
			return dispatchErr
		}
		if decision != nil {
			req.Model = decision.ModelName
			routedModelID = decision.ModelUUID
			// 传递 provider hint，确保 Router 在同名模型多渠道场景下
			// 选择 scheduler 指定的渠道，避免走裸 model ID「先注册者赢」。
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
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
			h.respondAnthropicGroupError(c, err)
		}
		return
	}
	if releaseQuota != nil {
		defer releaseQuota()
	}

	// v0.15: 模型层配额检查（路由选定具体 model 之后、转发上游之前）
	// 使用 routed_model_id（UUID）而非 model name，确保配额配置与路由表一致。
	if h.quotaGate != nil {
		subjectType, _ := c.Get("quota_subject_type")
		subjectID, _ := c.Get("quota_subject_id")
		if st, ok := subjectType.(string); ok {
			if si, ok := subjectID.(string); ok {
				if err := h.quotaGate.BeforeUpstream(c, st, si, routedModelID); err != nil {
					return // 已在 middleware 中写入 429 响应
				}
			}
		}
	}

	// Inject Gin context
	baseCtx := orchestrator.WithGinContext(c.Request.Context(), c)

	if anthReq.Stream {
		c.Set("stream", true)
		h.handleStream(c, baseCtx, req, originalModel)
		return
	}

	c.Set("stream", false)
	h.handleNonStream(c, baseCtx, req, originalModel)
}

// estimateAnthropicPromptChars 估算 Anthropic 请求的 prompt 字符数（system + messages）。
func estimateAnthropicPromptChars(req *core.AnthropicMessagesRequest) int {
	if req == nil {
		return 0
	}
	total := 0
	// system 字段是 any，可能是 string 或 []ContentBlock
	if req.System != nil {
		switch v := req.System.(type) {
		case string:
			total += len(v)
		default:
			b, _ := json.Marshal(v)
			total += len(b)
		}
	}
	// messages 累加每条
	for _, m := range req.Messages {
		b, err := json.Marshal(m.Content)
		if err == nil {
			total += len(b)
		}
	}
	return total
}

// nonStreamTimeout returns the timeout for non-streaming requests.
func (h *MessagesHandler) nonStreamTimeout(originalModel string) time.Duration {
	if h.groupDispatch != nil {
		return h.groupDispatch.GetGroupTimeout(originalModel, defaultNonStreamTimeout)
	}
	return defaultNonStreamTimeout
}

// streamTimeout returns the timeout for streaming requests.
func (h *MessagesHandler) streamTimeout(originalModel string) time.Duration {
	if h.groupDispatch != nil {
		return h.groupDispatch.GetGroupTimeout(originalModel, defaultStreamTimeout)
	}
	return defaultStreamTimeout
}

// handleNonStream processes a non-streaming request.
func (h *MessagesHandler) handleNonStream(c *gin.Context, baseCtx context.Context, req *core.ChatRequest, originalModel string) {
	timeout := h.nonStreamTimeout(originalModel)
	var lastErr error

	for {
		roundCtx, roundCancel := context.WithTimeout(baseCtx, timeout)
		resp, err := h.orchestrator.Chat(roundCtx, req)
		roundCancel()

		if err == nil {
			if lastErr != nil {
				c.Set("error_message", "")
			}
			anthResp := core.ChatResponseToAnthropic(resp, originalModel)
			if anthResp.ID == "" {
				anthResp.ID = generateAnthropicID(c)
			}
			c.Set("stream", false)
			c.Set("resp_object", anthResp)
			c.JSON(http.StatusOK, anthResp)
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
		slog.Warn("messages failover: attempt",
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
			slog.Error("messages failover: no available target",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"error", fErr.Error(),
			)
			break
		}

		req.Model = decision.ModelName
		req.Provider = decision.ProviderName
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

		slog.Info("messages failover: switching",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"target_provider", decision.ProviderName,
			"target_model", decision.ModelName,
			"target_model_id", decision.ModelID,
		)
	}

	h.respondAnthropicUnifiedError(c, lastErr)
}

// perMessageOverhead 每条消息的结构 token 开销（role 标记、分隔符、tool_use
// JSON 包装、词边界等）。旧估算完全忽略结构 token，导致短/结构化 prompt（尤其
// tool_use）被低算，故按消息逐条补计。
const perMessageOverhead = 3

// estimatePromptTokens 估算请求消息的 input_tokens（复用 scheduler 的 CJK 感知
// tokenizer，并额外按消息逐条计入结构 overhead）。当流式上游不返回真实 usage 时，
// 作为 message_start 的非零兜底值。
func estimatePromptTokens(msgs []core.Message) int {
	if len(msgs) == 0 {
		return 0
	}
	total := 0
	for _, m := range msgs {
		total += scheduler.EstimateTokensCalibrated(core.ExtractTextContent(m.Content))
		total += perMessageOverhead
	}
	if total < 1 {
		return 1
	}
	return total
}

// handleStream processes a streaming request.
func (h *MessagesHandler) handleStream(c *gin.Context, baseCtx context.Context, req *core.ChatRequest, originalModel string) {
	timeout := h.streamTimeout(originalModel)

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
		rawStream, cancel, err := openStream(req)

		if err == nil {
			if lastErr != nil {
				c.Set("error_message", "")
			}
			anthStream := newAnthropicStreamAdapter(
				&cancelOnClose{ReadCloser: rawStream, cancel: cancel},
				c.GetString("request_id"),
				originalModel,
				estimatePromptTokens(req.Messages),
			)
			HandleStreamResponseAs(c, anthStream, DialectAnthropic)
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
		slog.Warn("messages failover: attempt (stream)",
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
			slog.Error("messages failover: no available target (stream)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"error", fErr.Error(),
			)
			break
		}

		req.Model = decision.ModelName
		req.Provider = decision.ProviderName
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

		slog.Info("messages failover: switching (stream)",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"target_provider", decision.ProviderName,
			"target_model", decision.ModelName,
			"target_model_id", decision.ModelID,
		)
	}

	h.respondAnthropicUnifiedError(c, lastErr)
}

// ──────────────────────────────────────────────
// Token counting endpoint
// ──────────────────────────────────────────────

// ──────────────────────────────────────────────
// Relaykit 协议转换路径（active / shadow）
// ──────────────────────────────────────────────

// handleMessagesRK 使用 relaykit 进行 Claude → OpenAI → Claude 的双向协议转换。
// 流程：解码 Claude 请求 → relaykit 转换为 OpenAI → 内部 ChatRequest →
// 走既有调度/预算/配额管线 → 上游调用 → relaykit 回程转换为 Claude 响应。
func (h *MessagesHandler) handleMessagesRK(c *gin.Context, rawBody []byte) {
	// 解析基本字段用于验证和路由。
	var header struct {
		Model     string `json:"model"`
		Stream    bool   `json:"stream"`
		MaxTokens int    `json:"max_tokens"`
	}
	if err := json.Unmarshal(rawBody, &header); err != nil {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "invalid_request_body", err.Error(), nil)
		return
	}

	originalModel := header.Model
	if originalModel == "" {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "model_required", "model is required", nil)
		return
	}

	c.Set("model", originalModel)

	// 1. 解码 Claude 请求。
	claudeReq, err := protocol.DecodeClaudeRequest(rawBody)
	if err != nil {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "invalid_request_body", err.Error(), nil)
		return
	}

	// 2. 创建 relaykit 转换会话。
	session := protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		originalModel,
		originalModel,
	)

	// 3. relaykit 转换 Claude → OpenAI。
	result, err := protocol.ConvertRequest(session, claudeReq)
	if err != nil {
		h.respondAnthropicError(c, http.StatusInternalServerError, "internal_error", "conversion_failed", err.Error(), nil)
		return
	}

	// 4. 收集诊断信息。
	bridgeDiags := protocol.HasUnsupportedBlocks(claudeReq)
	protocol.JoinDiagnostics(session, result, bridgeDiags)

	// 5. 按损失策略判定是否拒绝。
	if err := protocol.ApplyLossPolicy(types.ConversionLossPolicyAllow, session.Diagnostics); err != nil {
		var convErr *protocol.ConversionError
		if errors.As(err, &convErr) {
			h.respondAnthropicError(c, convErr.HTTPStatus, "invalid_request", "conversion_rejected", convErr.Error(), nil)
			return
		}
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "conversion_rejected", err.Error(), nil)
		return
	}

	// 6. 桥接为内部 ChatRequest。
	req, err := protocol.BridgeToChatRequest(session, result)
	if err != nil {
		h.respondAnthropicError(c, http.StatusInternalServerError, "internal_error", "bridge_failed", err.Error(), nil)
		return
	}

	// 7. 调度/预算/配额（复用 legacy 管线）。
	routedModelID := originalModel

	releaseQuota, err := PrepareAuthorizedGates(c, originalModel, func() error {
		if h.groupDispatch == nil {
			return nil
		}
		decision, dispatchErr := h.groupDispatch.Dispatch(c, originalModel, req)
		if dispatchErr != nil {
			return dispatchErr
		}
		if decision != nil {
			req.Model = decision.ModelName
			routedModelID = decision.ModelUUID
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
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
			h.respondAnthropicGroupError(c, err)
		}
		return
	}
	if releaseQuota != nil {
		defer releaseQuota()
	}

	// v0.15: 模型层配额检查。
	if h.quotaGate != nil {
		subjectType, _ := c.Get("quota_subject_type")
		subjectID, _ := c.Get("quota_subject_id")
		if st, ok := subjectType.(string); ok {
			if si, ok := subjectID.(string); ok {
				if err := h.quotaGate.BeforeUpstream(c, st, si, routedModelID); err != nil {
					return
				}
			}
		}
	}

	baseCtx := orchestrator.WithGinContext(c.Request.Context(), c)

	// 原生旁路：目标渠道能直接吃 Anthropic 原生 body 时零转换直发，
	// 响应原样回传；只有 ErrNativeNotSupported 才降级到下方 relaykit 转换路径。
	// 位置必须在 PrepareAuthorizedGates 之后，否则会绕过调度/预算/配额。
	if native, ok := h.orchestrator.(NativeChatOrchestratorInterface); ok {
		if header.Stream {
			c.Set("stream", true)
			stream, nativeErr := native.ChatNativeStream(baseCtx, req, rawBody, core.NativeRelayClaude)
			if nativeErr == nil {
				c.Set("upstream_protocol", "native_claude")
				forwardNativeStream(c, stream)
				return
			}
			// 真实错误（非“不支持原生”）：按 Anthropic 错误格式返回。
			// 不能静默降级，否则会对同一请求二次打上游。
			if !errors.Is(nativeErr, core.ErrNativeNotSupported) {
				h.respondAnthropicUnifiedError(c, nativeErr)
				return
			}
		} else {
			c.Set("stream", false)
			out, nativeErr := native.ChatNative(baseCtx, req, rawBody, core.NativeRelayClaude)
			if nativeErr == nil {
				// 原生响应本身就是干净的 Anthropic 格式，不做任何清洗/改写。
				c.Set("upstream_protocol", "native_claude")
				c.Data(http.StatusOK, "application/json", out)
				return
			}
			if !errors.Is(nativeErr, core.ErrNativeNotSupported) {
				h.respondAnthropicUnifiedError(c, nativeErr)
				return
			}
		}
	}

	if header.Stream {
		c.Set("stream", true)
		h.handleMessagesRKStream(c, baseCtx, req, originalModel, session)
		return
	}

	c.Set("stream", false)
	h.handleMessagesRKNonStream(c, baseCtx, req, originalModel, session)
}

// forwardNativeStream 把上游原生 Anthropic SSE 原样转发给客户端：
// 只设置 SSE 响应头并逐块 flush，不对字节做任何解析或改写。
func forwardNativeStream(c *gin.Context, stream io.ReadCloser) {
	defer stream.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	// 包一层 flushWriter：每写完一个读块立刻 flush，避免 SSE 帧被缓冲合并。
	_, _ = io.Copy(flushWriter{c.Writer}, stream)
}

// flushWriter 每次 Write 后立刻 Flush，保持 SSE 帧的实时性。
type flushWriter struct {
	w gin.ResponseWriter
}

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	f.w.Flush()
	return n, err
}

// handleMessagesRKShadow 在 shadow 模式下运行双路径：
//   - 主路径：legacy 转换，正常返回客户端。
//   - 影子路径：relaykit 转换，仅记录诊断日志，不上游。
func (h *MessagesHandler) handleMessagesRKShadow(c *gin.Context, rawBody []byte) {
	// 影子 goroutine：仅做 relaykit 解码+转换，记录诊断差异。
	go func(body []byte) {
		claudeReq, err := protocol.DecodeClaudeRequest(body)
		if err != nil {
			slog.Debug("protocol shadow: decode claude request failed", "error", err)
			return
		}

		var model string
		if claudeReq != nil {
			model = claudeReq.Model
		}

		session := protocol.NewSession(
			types.RelayFormatClaude,
			types.RelayFormatOpenAI,
			protocol.DefaultConvOptions(),
			model,
			model,
		)

		result, err := protocol.ConvertRequest(session, claudeReq)
		if err != nil {
			slog.Debug("protocol shadow: convert request failed", "error", err)
			return
		}

		bridgeDiags := protocol.HasUnsupportedBlocks(claudeReq)
		protocol.JoinDiagnostics(session, result, bridgeDiags)

		if len(session.Diagnostics) > 0 {
			slog.Info("protocol shadow: conversion diagnostics",
				"count", len(session.Diagnostics),
				"codes", protocol.FormatLossHeader(session.Diagnostics),
			)
		}

		_, err = protocol.BridgeToChatRequest(session, result)
		if err != nil {
			slog.Debug("protocol shadow: bridge to chat request failed", "error", err)
			return
		}

		if len(session.Diagnostics) == 0 {
			slog.Debug("protocol shadow: conversion successful, no diagnostics")
		}
	}(rawBody)

	// 主路径：恢复请求体，走 legacy 管线。
	c.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

	var anthReq core.AnthropicMessagesRequest
	if err := c.ShouldBindJSON(&anthReq); err != nil {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "invalid_request_body", err.Error(), nil)
		return
	}

	if anthReq.Model == "" {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "model_required", "model is required", nil)
		return
	}

	if len(anthReq.Messages) == 0 {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "messages_required", "messages is required and must not be empty", nil)
		return
	}

	c.Set("model", anthReq.Model)

	req, convErr := core.AnthropicToChatRequest(&anthReq)
	if convErr != nil {
		h.respondAnthropicConversionError(c, convErr)
		return
	}

	originalModel := anthReq.Model
	routedModelID := anthReq.Model

	releaseQuota, err := PrepareAuthorizedGates(c, originalModel, func() error {
		if h.groupDispatch == nil {
			return nil
		}
		decision, dispatchErr := h.groupDispatch.Dispatch(c, originalModel, req)
		if dispatchErr != nil {
			return dispatchErr
		}
		if decision != nil {
			req.Model = decision.ModelName
			routedModelID = decision.ModelUUID
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
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
			h.respondAnthropicGroupError(c, err)
		}
		return
	}
	if releaseQuota != nil {
		defer releaseQuota()
	}

	if h.quotaGate != nil {
		subjectType, _ := c.Get("quota_subject_type")
		subjectID, _ := c.Get("quota_subject_id")
		if st, ok := subjectType.(string); ok {
			if si, ok := subjectID.(string); ok {
				if err := h.quotaGate.BeforeUpstream(c, st, si, routedModelID); err != nil {
					return
				}
			}
		}
	}

	baseCtx := orchestrator.WithGinContext(c.Request.Context(), c)

	if anthReq.Stream {
		c.Set("stream", true)
		h.handleStream(c, baseCtx, req, originalModel)
		return
	}

	c.Set("stream", false)
	h.handleNonStream(c, baseCtx, req, originalModel)
}

// handleMessagesRKNonStream 处理 relaykit 路径的非流式请求。
// 与 legacy handleNonStream 结构相同，差异在于成功响应使用
// BridgeChatResponseToSource 转回 Claude 格式。
func (h *MessagesHandler) handleMessagesRKNonStream(c *gin.Context, baseCtx context.Context, req *core.ChatRequest, originalModel string, session *protocol.Session) {
	timeout := h.nonStreamTimeout(originalModel)
	var lastErr error

	for {
		roundCtx, roundCancel := context.WithTimeout(baseCtx, timeout)
		resp, err := h.orchestrator.Chat(roundCtx, req)
		roundCancel()

		if err == nil {
			if lastErr != nil {
				c.Set("error_message", "")
			}
			// relaykit 回程：ChatResponse → Claude 格式。
			claudeResult, convErr := protocol.BridgeChatResponseToSource(session, resp)
			if convErr != nil {
				h.respondAnthropicError(c, http.StatusInternalServerError, "internal_error", "response_conversion_failed", convErr.Error(), nil)
				return
			}
			if claudeResult.Value == nil {
				h.respondAnthropicError(c, http.StatusInternalServerError, "internal_error", "empty_response", "relaykit produced nil response value", nil)
				return
			}
			c.Set("stream", false)
			c.Set("resp_object", claudeResult.Value)
			// 经 protocol 清洗后再输出，避免 relaykit 内部计费字段泄漏到 Anthropic 响应。
			payload, marshalErr := protocol.MarshalForClient(claudeResult.Value)
			if marshalErr != nil {
				h.respondAnthropicError(c, http.StatusInternalServerError, "internal_error", "response_encode_failed", marshalErr.Error(), nil)
				return
			}
			c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
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
		slog.Warn("messages failover: attempt (rk)",
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
			slog.Error("messages failover: no available target (rk)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"error", fErr.Error(),
			)
			break
		}

		req.Model = decision.ModelName
		req.Provider = decision.ProviderName
		c.Set("failover_used", true)

		dc := model.GetDispatchContext(c)
		if dc != nil && dc.SessionKey != "" {
			h.groupDispatch.UpdateSticky(c.Request.Context(), originalModel, dc.SessionKey, decision.ModelName, decision.ProviderName)
		}

		if h.quotaGate != nil {
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

		slog.Info("messages failover: switching (rk)",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"target_provider", decision.ProviderName,
			"target_model", decision.ModelName,
			"target_model_id", decision.ModelID,
		)
	}

	h.respondAnthropicUnifiedError(c, lastErr)
}

// handleMessagesRKStream 处理 relaykit 路径的流式请求。
// 与 legacy handleStream 结构相同，差异在于使用
// protocol.NewStreamAdapter 替代 newAnthropicStreamAdapter。
func (h *MessagesHandler) handleMessagesRKStream(c *gin.Context, baseCtx context.Context, req *core.ChatRequest, originalModel string, session *protocol.Session) {
	timeout := h.streamTimeout(originalModel)

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
		rawStream, cancel, err := openStream(req)

		if err == nil {
			if lastErr != nil {
				c.Set("error_message", "")
			}
			streamAdapter := protocol.NewStreamAdapter(
				&cancelOnClose{ReadCloser: rawStream, cancel: cancel},
				session,
				req.Model,
				estimatePromptTokens(req.Messages),
				generateAnthropicID(c),
			)
			HandleStreamResponseAs(c, streamAdapter, DialectAnthropic)
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
		slog.Warn("messages failover: attempt (rk stream)",
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
			slog.Error("messages failover: no available target (rk stream)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"error", fErr.Error(),
			)
			break
		}

		req.Model = decision.ModelName
		req.Provider = decision.ProviderName
		c.Set("failover_used", true)

		dc := model.GetDispatchContext(c)
		if dc != nil && dc.SessionKey != "" {
			h.groupDispatch.UpdateSticky(c.Request.Context(), originalModel, dc.SessionKey, decision.ModelName, decision.ProviderName)
		}

		if h.quotaGate != nil {
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

		slog.Info("messages failover: switching (rk stream)",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"target_provider", decision.ProviderName,
			"target_model", decision.ModelName,
			"target_model_id", decision.ModelID,
		)
	}

	h.respondAnthropicUnifiedError(c, lastErr)
}

// HandleCountTokens handles a token counting request.
// Anthropic's /v1/messages/count_tokens endpoint returns approximate token count.
func (h *MessagesHandler) HandleCountTokens(c *gin.Context) {
	var anthReq core.AnthropicMessagesRequest
	if err := c.ShouldBindJSON(&anthReq); err != nil {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "invalid_request_body", err.Error(), nil)
		return
	}

	// Convert to ChatRequest and count tokens approximately.
	// 改用与流式 message_start 一致的校准估算（CJK≈0.66/字 + 每条消息结构开销），
	// 替代旧的 totalChars/4——后者会严重低估中文（中文按 1 字近 1 token，而非 4 字 1 token）。
	// 视频/图片等不支持转换的片段由 AnthropicToChatRequest 返回错误，这里显式回 400，
	// 不再静默丢弃（静默丢弃会让用户以为模型看到了视频）。
	req, convErr := core.AnthropicToChatRequest(&anthReq)
	if convErr != nil {
		h.respondAnthropicConversionError(c, convErr)
		return
	}

	estimatedTokens := estimatePromptTokens(req.Messages)
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}

	respBody := gin.H{"input_tokens": estimatedTokens}
	c.Set("stream", false)
	c.Set("resp_object", respBody)
	c.JSON(http.StatusOK, respBody)
}

// ──────────────────────────────────────────────
// Error helpers (Anthropic error format)
// ──────────────────────────────────────────────

// respondAnthropicError sends an error in Anthropic format.
func (h *MessagesHandler) respondAnthropicError(c *gin.Context, httpStatus int, errType string, code string, message string, upstream *gatewayerrors.UpstreamInfo) {
	if IsPassthroughMode(c) {
		writePassthroughError(c, &model.UnifiedError{
			Type:       model.ErrorType(errType),
			Code:       code,
			Message:    message,
			HTTPStatus: httpStatus,
			Upstream:   upstream,
		})
		return
	}
	body := gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	}
	if code != "" {
		body["error"].(gin.H)["code"] = code
	}
	if upstream != nil {
		body["error"].(gin.H)["upstream"] = upstream
	}
	middleware.SetTraceResponseObject(c, body)
	c.JSON(httpStatus, body)
}

// respondAnthropicConversionError 把 core 转换层错误（协议不支持的内容片段，如视频）
// 渲染为 Anthropic 形状的错误体：/v1/messages 的客户端只认 Anthropic 错误格式。
func (h *MessagesHandler) respondAnthropicConversionError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	message := err.Error()
	var gwErr *core.GatewayError
	if errors.As(err, &gwErr) {
		if gwErr.StatusCode > 0 {
			status = gwErr.StatusCode
		}
		if gwErr.Message != "" {
			message = gwErr.Message
		}
	}
	h.respondAnthropicError(c, status, "invalid_request_error", "unsupported_content_part", message, nil)
}

// respondAnthropicGroupError handles group dispatch errors.
func (h *MessagesHandler) respondAnthropicGroupError(c *gin.Context, err error) {
	if err == model.ErrGroupNotFound {
		h.respondAnthropicError(c, http.StatusNotFound, "not_found_error", "model_group_not_found", err.Error(), nil)
		return
	}
	if err == model.ErrNoAvailableModel {
		h.respondAnthropicError(c, http.StatusServiceUnavailable, "overloaded_error", "model_unavailable", "No available model for this group", nil)
		return
	}
	if err == model.ErrNoOtherModelAvailable {
		h.respondAnthropicError(c, http.StatusServiceUnavailable, "overloaded_error", "model_unavailable", "No other model available for failover", nil)
		return
	}
	if ue, ok := err.(*model.UnifiedError); ok {
		anthType := mapAnthropicErrorType(ue.Type)
		h.respondAnthropicError(c, ue.HTTPStatus, anthType, ue.Code, ue.Message, ue.Upstream)
		return
	}
	h.respondAnthropicError(c, http.StatusInternalServerError, "internal_error", "internal_error", err.Error(), nil)
}

// respondAnthropicUnifiedError converts a UnifiedError to Anthropic format.
func (h *MessagesHandler) respondAnthropicUnifiedError(c *gin.Context, err error) {
	if ue, ok := err.(*model.UnifiedError); ok {
		anthType := mapAnthropicErrorType(ue.Type)
		h.respondAnthropicError(c, ue.HTTPStatus, anthType, ue.Code, ue.Message, ue.Upstream)
		return
	}
	h.respondAnthropicError(c, http.StatusInternalServerError, "internal_error", "internal_error", err.Error(), nil)
}

// mapAnthropicErrorType maps gateway error types to Anthropic error types.
func mapAnthropicErrorType(errType model.ErrorType) string {
	switch errType {
	case model.ErrorTypeInvalidRequest:
		return "invalid_request_error"
	case model.ErrorTypeModelNotFound:
		return "not_found_error"
	case model.ErrorTypeModelUnavail:
		return "overloaded_error"
	case model.ErrorTypeTempFailure:
		return "overloaded_error"
	case model.ErrorTypeAuthError:
		return "authentication_error"
	case model.ErrorTypeInternal:
		return "api_error"
	default:
		return "api_error"
	}
}

// generateAnthropicID generates a unique Anthropic-style message ID.
func generateAnthropicID(c *gin.Context) string {
	if requestID := c.GetString("request_id"); requestID != "" {
		return "msg_" + requestID
	}
	// Fallback: generate a random ID
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("msg_%x", b)
}
