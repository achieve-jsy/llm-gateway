package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/gin-gonic/gin"
)

// MessagesHandler handles Anthropic-compatible POST /v1/messages requests.
// It parses the Anthropic Messages API request, converts it to the internal
// ChatRequest (OpenAI format), dispatches through the ChatOrchestrator,
// then converts the response back to Anthropic Messages API format.
type MessagesHandler struct {
	orchestrator  ChatOrchestratorInterface
	groupDispatch *orchestrator.GroupAwareDispatcher
}

// NewMessagesHandler creates a MessagesHandler.
func NewMessagesHandler(orch ChatOrchestratorInterface) *MessagesHandler {
	return &MessagesHandler{orchestrator: orch}
}

// NewMessagesHandlerWithGroup creates a MessagesHandler with group dispatch support.
func NewMessagesHandlerWithGroup(orch ChatOrchestratorInterface, gd *orchestrator.GroupAwareDispatcher) *MessagesHandler {
	return &MessagesHandler{orchestrator: orch, groupDispatch: gd}
}

// Handle processes POST /v1/messages requests.
func (h *MessagesHandler) HandleMessages(c *gin.Context) {
	var anthReq core.AnthropicMessagesRequest
	if err := c.ShouldBindJSON(&anthReq); err != nil {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if anthReq.Model == "" {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}

	if len(anthReq.Messages) == 0 {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", "messages is required and must not be empty")
		return
	}

	c.Set("model", anthReq.Model)

	// Convert Anthropic request to internal ChatRequest
	req := core.AnthropicToChatRequest(&anthReq)

	originalModel := anthReq.Model

	// Group dispatch (model group resolution)
	if h.groupDispatch != nil {
		decision, err := h.groupDispatch.Dispatch(c, anthReq.Model)
		if err != nil {
			h.respondAnthropicGroupError(c, err)
			return
		}
		if decision != nil {
			req.Model = decision.ModelName
		}
	}

	// Inject Gin context
	baseCtx := orchestrator.WithGinContext(c.Request.Context(), c)

	if anthReq.Stream {
		h.handleStream(c, baseCtx, req, originalModel)
		return
	}

	h.handleNonStream(c, baseCtx, req, originalModel)
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
	primaryCtx, primaryCancel := context.WithTimeout(baseCtx, timeout)
	resp, err := h.orchestrator.Chat(primaryCtx, req)
	primaryCancel()

	if err != nil {
		if h.groupDispatch != nil && req.Model != originalModel {
			// Try failover
			failedID := req.Model
			if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
				failedID = dc.RoutedModelID
			}
			slog.Warn("messages failover: primary failed",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"failed_model", req.Model,
				"failed_model_id", failedID,
				"error", err.Error(),
			)
			if decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID); fErr == nil {
				req.Model = decision.ModelName
				req.Provider = decision.ProviderName
				c.Set("failover_used", true)
				slog.Info("messages failover: switching",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"target_provider", decision.ProviderName,
					"target_model", decision.ModelName,
					"target_model_id", decision.ModelID,
				)
				failoverCtx, failoverCancel := context.WithTimeout(baseCtx, timeout)
				fResp, fErr2 := h.orchestrator.Chat(failoverCtx, req)
				failoverCancel()
				if fErr2 == nil {
					c.Set("error_message", "")
					slog.Info("messages failover: success",
						"request_id", c.GetString("request_id"),
						"group", originalModel,
						"final_provider", decision.ProviderName,
					)
					anthResp := core.ChatResponseToAnthropic(fResp, originalModel)
					c.JSON(http.StatusOK, anthResp)
					return
				}
				slog.Error("messages failover: target also failed",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"target_provider", decision.ProviderName,
					"error", fErr2.Error(),
				)
			} else {
				slog.Error("messages failover: no available target",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"error", fErr.Error(),
				)
			}
		}
		h.respondAnthropicUnifiedError(c, err)
		return
	}

	anthResp := core.ChatResponseToAnthropic(resp, originalModel)
	// Ensure we always have a proper ID and model
	if anthResp.ID == "" {
		anthResp.ID = generateAnthropicID(c)
	}
	c.JSON(http.StatusOK, anthResp)
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

	// Primary attempt
	rawStream, cancel, err := openStream(req)
	if err != nil {
		if h.groupDispatch != nil && req.Model != originalModel {
			failedID := req.Model
			if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
				failedID = dc.RoutedModelID
			}
			slog.Warn("messages failover: primary failed (stream)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"failed_model", req.Model,
				"failed_model_id", failedID,
				"error", err.Error(),
			)
			if decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID); fErr == nil {
				req.Model = decision.ModelName
				req.Provider = decision.ProviderName
				c.Set("failover_used", true)
				fStream, fCancel, fErr2 := openStream(req)
				if fErr2 == nil {
					c.Set("error_message", "")
					anthStream := newAnthropicStreamAdapter(
						&cancelOnClose{ReadCloser: fStream, cancel: fCancel},
						c.GetString("request_id"),
						originalModel,
					)
					HandleStreamResponse(c, anthStream)
					return
				}
				slog.Error("messages failover: target also failed (stream)",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"target_provider", decision.ProviderName,
					"error", fErr2.Error(),
				)
			} else {
				slog.Error("messages failover: no available target (stream)",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"error", fErr.Error(),
				)
			}
		}
		h.respondAnthropicUnifiedError(c, err)
		return
	}

	// Wrap the OpenAI SSE stream with our Anthropic SSE converter
	anthStream := newAnthropicStreamAdapter(
		&cancelOnClose{ReadCloser: rawStream, cancel: cancel},
		c.GetString("request_id"),
		originalModel,
	)
	HandleStreamResponse(c, anthStream)
}

// ──────────────────────────────────────────────
// Token counting endpoint
// ──────────────────────────────────────────────

// HandleCountTokens handles a token counting request.
// Anthropic's /v1/messages/count_tokens endpoint returns approximate token count.
func (h *MessagesHandler) HandleCountTokens(c *gin.Context) {
	var anthReq core.AnthropicMessagesRequest
	if err := c.ShouldBindJSON(&anthReq); err != nil {
		h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request", err.Error())
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

	c.JSON(http.StatusOK, gin.H{
		"input_tokens": estimatedTokens,
	})
}

// ──────────────────────────────────────────────
// Error helpers (Anthropic error format)
// ──────────────────────────────────────────────

// respondAnthropicError sends an error in Anthropic format.
func (h *MessagesHandler) respondAnthropicError(c *gin.Context, httpStatus int, errType string, message string) {
	c.JSON(httpStatus, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// respondAnthropicGroupError handles group dispatch errors.
func (h *MessagesHandler) respondAnthropicGroupError(c *gin.Context, err error) {
	if err == model.ErrGroupNotFound {
		h.respondAnthropicError(c, http.StatusNotFound, "not_found_error", err.Error())
		return
	}
	if err == model.ErrNoAvailableModel {
		h.respondAnthropicError(c, http.StatusServiceUnavailable, "overloaded_error", "No available model for this group")
		return
	}
	if err == model.ErrNoOtherModelAvailable {
		h.respondAnthropicError(c, http.StatusServiceUnavailable, "overloaded_error", "No other model available for failover")
		return
	}
	h.respondAnthropicError(c, http.StatusInternalServerError, "internal_error", err.Error())
}

// respondAnthropicUnifiedError converts a UnifiedError to Anthropic format.
func (h *MessagesHandler) respondAnthropicUnifiedError(c *gin.Context, err error) {
	if ue, ok := err.(*model.UnifiedError); ok {
		anthType := mapAnthropicErrorType(ue.Type)
		h.respondAnthropicError(c, ue.HTTPStatus, anthType, ue.Message)
		return
	}
	h.respondAnthropicError(c, http.StatusInternalServerError, "internal_error", err.Error())
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