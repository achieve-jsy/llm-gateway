package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	gatewayerrors "github.com/example/llm-gateway/internal/errors"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/gin-gonic/gin"
)

// MessagesHandler handles Anthropic-compatible POST /v1/messages requests.
// It parses the Anthropic Messages API request, converts it to the internal
// ChatRequest (OpenAI format), dispatches through the ChatOrchestrator,
// then converts the response back to Anthropic Messages API format.
type MessagesHandler struct {
	orchestrator  ChatOrchestratorInterface
	groupDispatch *orchestrator.GroupAwareDispatcher
	budgetDeps    *middleware.BudgetDeps
	quotaGate     *quota.ModelQuotaGate // v0.15: 模型层配额检查
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

// SetModelQuotaGate 注入模型配额检查器（router setup 时调用一次）
func (h *MessagesHandler) SetModelQuotaGate(gate *quota.ModelQuotaGate) {
	h.quotaGate = gate
}

// Handle processes POST /v1/messages requests.
func (h *MessagesHandler) HandleMessages(c *gin.Context) {
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

	// v0.9: 预算检查（事后记账模式）
	if h.budgetDeps != nil {
		if _, err := middleware.Reserve(c, h.budgetDeps); err != nil {
			if be, ok := err.(*middleware.BudgetExceededError); ok {
				c.AbortWithStatusJSON(http.StatusPaymentRequired, be.ToJSON())
				return
			}
			slog.Warn("messages: budget check failed", "model", anthReq.Model, "error", err)
		}
	}

	// Convert Anthropic request to internal ChatRequest
	req := core.AnthropicToChatRequest(&anthReq)

	originalModel := anthReq.Model

	// 记录路由选定的模型 ID，用于模型层配额检查。
	routedModelID := anthReq.Model

	// Group dispatch (model group resolution)
	if h.groupDispatch != nil {
		decision, err := h.groupDispatch.Dispatch(c, anthReq.Model, req)
		if err != nil {
			h.respondAnthropicGroupError(c, err)
			return
		}
		if decision != nil {
			req.Model = decision.ModelName
			routedModelID = decision.ModelID
			// 传递 provider hint，确保 Router 在同名模型多渠道场景下
			// 选择 scheduler 指定的渠道，避免走裸 model ID「先注册者赢」。
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
				middleware.TrackProviderRequest(c, decision.ProviderName)
			}
		}
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

		// v0.15: failover 模型也需要经过模型层配额检查
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
			)
			HandleStreamResponse(c, anthStream)
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

		// v0.15: failover 模型也需要经过模型层配额检查
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

// HandleCountTokens handles a token counting request.
// Anthropic's /v1/messages/count_tokens endpoint returns approximate token count.
func (h *MessagesHandler) HandleCountTokens(c *gin.Context) {
	var anthReq core.AnthropicMessagesRequest
	if err := c.ShouldBindJSON(&anthReq); err != nil {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "invalid_request_body", err.Error(), nil)
		return
	}

	// Convert to ChatRequest and count tokens approximately
	req := core.AnthropicToChatRequest(&anthReq)

	totalChars := 0
	for _, msg := range req.Messages {
		switch content := msg.Content.(type) {
		case string:
			totalChars += len(content)
		case []core.ContentPart:
			for _, part := range content {
				totalChars += len(part.Text)
			}
		}
	}

	// Rough estimate: ~4 chars per token for English text
	estimatedTokens := totalChars / 4
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
		writePassthroughError(c, httpStatus, errType, code, message)
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