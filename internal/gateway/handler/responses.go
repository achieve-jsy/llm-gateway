package handler

import (
	"context"
	"io"
	"net/http"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/gin-gonic/gin"
)

// ResponsesHandler 处理 OpenAI Responses API 请求。
type ResponsesHandler struct {
	orchestrator  ResponsesOrchestratorInterface
	groupDispatch *orchestrator.GroupAwareDispatcher
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

	// 若请求的是模型组名，通过 group dispatcher 解析到实际 provider 模型
	if h.groupDispatch != nil {
		decision, err := h.groupDispatch.Dispatch(c, req.Model)
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

	ctx := orchestrator.WithGinContext(c.Request.Context(), c)

	if req.Stream {
		h.handleStream(c, ctx, &req)
		return
	}

	h.handleNonStream(c, ctx, &req)
}

func (h *ResponsesHandler) handleNonStream(c *gin.Context, ctx context.Context, req *core.ResponsesRequest) {
	resp, err := h.orchestrator.Responses(ctx, req)
	if err != nil {
		if ue, ok := err.(*model.UnifiedError); ok {
			respondError(c, ue)
			return
		}
		respondError(c, model.ErrInternal)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *ResponsesHandler) handleStream(c *gin.Context, ctx context.Context, req *core.ResponsesRequest) {
	stream, err := h.orchestrator.ResponsesStream(ctx, req)
	if err != nil {
		if ue, ok := err.(*model.UnifiedError); ok {
			respondError(c, ue)
			return
		}
		respondError(c, model.ErrInternal)
		return
	}

	HandleStreamResponse(c, stream)
}