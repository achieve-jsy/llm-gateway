package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/gin-gonic/gin"
)

// ResponsesHandler 处理 OpenAI Responses API 请求。
type ResponsesHandler struct {
	orchestrator  ResponsesOrchestratorInterface
	groupDispatch *orchestrator.GroupAwareDispatcher
	budgetDeps    *middleware.BudgetDeps
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

// SetBudgetDeps 注入预算依赖（router setup 时调用一次）
func (h *ResponsesHandler) SetBudgetDeps(deps *middleware.BudgetDeps) {
	h.budgetDeps = deps
}

// Handle 处理 POST /v1/responses 请求
func (h *ResponsesHandler) Handle(c *gin.Context) {
	var req core.ResponsesRequest
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

	c.Set("model", req.Model)
	originalModel := req.Model
	c.Set("stream", req.Stream)

	// v0.9: 预算检查（事后记账模式）
	if h.budgetDeps != nil {
		if _, err := middleware.Reserve(c, h.budgetDeps); err != nil {
			if be, ok := err.(*middleware.BudgetExceededError); ok {
				be.WriteHTTPResponse(c)
				return
			}
			slog.Warn("responses: budget check failed", "model", req.Model, "error", err)
		}
	}

	// 若请求的是模型组名，通过 group dispatcher 解析到实际 provider 模型
	if h.groupDispatch != nil {
		decision, err := h.groupDispatch.Dispatch(c, req.Model, nil)
		if err != nil {
			if ue, ok := err.(*model.UnifiedError); ok {
				respondError(c, ue)
			} else {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": gin.H{"type": "unavailable", "message": "no available model in group", "code": "no_available_model"},
				})
			}
			return
		}
		if decision != nil {
			req.Model = decision.ModelName
			req.Provider = decision.ChannelType
		}
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
	if h.groupDispatch != nil && req.Model != originalModel {
		failedID := req.Model
		if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
			failedID = dc.RoutedModelID
		}
		if decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID, nil); fErr == nil {
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
		if h.groupDispatch != nil && req.Model != originalModel {
			failedID := req.Model
			if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
				failedID = dc.RoutedModelID
			}
			if decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID, nil); fErr == nil {
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
