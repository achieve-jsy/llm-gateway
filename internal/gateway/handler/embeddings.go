package handler

import (
	"context"
	"net/http"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/gin-gonic/gin"
)

// EmbeddingsOrchestratorInterface abstracts the embeddings call for testing.
type EmbeddingsOrchestratorInterface interface {
	Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error)
}

// EmbeddingsHandler handles POST /v1/embeddings
type EmbeddingsHandler struct {
	orchestrator  EmbeddingsOrchestratorInterface
	groupDispatch *orchestrator.GroupAwareDispatcher
}

// NewEmbeddingsHandler creates a new EmbeddingsHandler.
func NewEmbeddingsHandler(orch EmbeddingsOrchestratorInterface) *EmbeddingsHandler {
	return &EmbeddingsHandler{orchestrator: orch}
}

// NewEmbeddingsHandlerWithGroup creates a new EmbeddingsHandler with group dispatch.
func NewEmbeddingsHandlerWithGroup(orch EmbeddingsOrchestratorInterface, gd *orchestrator.GroupAwareDispatcher) *EmbeddingsHandler {
	return &EmbeddingsHandler{orchestrator: orch, groupDispatch: gd}
}

// Handle processes POST /v1/embeddings requests.
//
//	@Summary		Embeddings
//	@Description	创建文本嵌入向量，兼容 OpenAI /v1/embeddings 接口
//	@Tags			Embeddings
//	@Accept			json
//	@Produce		json
//	@Param			request	body		core.EmbeddingRequest	true	"嵌入请求"
//	@Success		200		{object}	core.EmbeddingResponse	"嵌入响应"
//	@Failure		400		{object}	map[string]interface{}	"请求参数错误"
//	@Failure		401		{object}	map[string]interface{}	"认证失败"
//	@Failure		500		{object}	map[string]interface{}	"内部错误"
//	@Security		BearerAuth
//	@Router			/v1/embeddings [post]
func (h *EmbeddingsHandler) Handle(c *gin.Context) {
	var req core.EmbeddingRequest
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

	if h.groupDispatch != nil {
		decision, err := h.groupDispatch.Dispatch(c, req.Model)
		if err != nil {
			if ue, ok := err.(*model.UnifiedError); ok {
				respondError(c, ue)
				return
			}
			respondError(c, model.ErrInternal)
			return
		}
		if decision != nil {
			req.Model = decision.ModelName
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
			}
		}
	}

	resp, err := h.orchestrator.Embeddings(c.Request.Context(), &req)
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
