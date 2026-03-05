package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/example/llm-gateway/internal/core"
	gatewayerrors "github.com/example/llm-gateway/internal/errors"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
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
	budgetDeps    *middleware.BudgetDeps
	quotaGate     *quota.ModelQuotaGate // v0.15: 模型层配额检查
}

// NewEmbeddingsHandler creates a new EmbeddingsHandler.
func NewEmbeddingsHandler(orch EmbeddingsOrchestratorInterface) *EmbeddingsHandler {
	return &EmbeddingsHandler{orchestrator: orch}
}

// SetBudgetDeps 注入预算依赖（router setup 时调用一次）
func (h *EmbeddingsHandler) SetBudgetDeps(deps *middleware.BudgetDeps) {
	h.budgetDeps = deps
}

// SetModelQuotaGate 注入模型配额检查器（router setup 时调用一次）
func (h *EmbeddingsHandler) SetModelQuotaGate(gate *quota.ModelQuotaGate) {
	h.quotaGate = gate
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
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage(err.Error()))
		return
	}

	// 校验 model 非空
	if req.Model == "" {
		gatewayerrors.WriteError(c, "request_missing_model")
		return
	}

	// 校验 input 非空
	if req.Input == nil {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage("input field is required and must not be empty"))
		return
	}

	// 校验 input 不能是空字符串
	if s, ok := req.Input.(string); ok && s == "" {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage("input must not be an empty string"))
		return
	}

	// 校验 input 类型：必须是 string 或 []string
	if _, ok := req.Input.(string); !ok {
		if arr, ok2 := req.Input.([]interface{}); ok2 {
			if len(arr) == 0 {
				gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage("input array must not be empty"))
				return
			}
		} else {
			gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage("input must be a string or array of strings"))
			return
		}
	}

	c.Set("model", req.Model)

	// v0.9: 预算检查（事后记账模式）
	if h.budgetDeps != nil {
		if _, err := middleware.Reserve(c, h.budgetDeps); err != nil {
			if be, ok := err.(*middleware.BudgetExceededError); ok {
				be.WriteHTTPResponse(c)
				return
			}
			slog.Warn("embeddings: budget check failed", "model", req.Model, "error", err)
		}
	}

	// 记录路由选定的模型 ID，用于模型层配额检查。
	routedModelID := req.Model

	if h.groupDispatch != nil {
		decision, err := h.groupDispatch.Dispatch(c, req.Model, nil)
		if err != nil {
			respondGroupError(c, err)
			return
		}
		if decision != nil {
			req.Model = decision.ModelName
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
			}
			routedModelID = decision.ModelID
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

	// 调用上游（不做 failover，失败直接返回错误）
	resp, err := h.orchestrator.Embeddings(orchestrator.WithGinContext(c.Request.Context(), c), &req)
	if err != nil {
		if ue, ok := err.(*model.UnifiedError); ok {
			respondError(c, ue)
			return
		}
		respondError(c, model.ErrInternal)
		return
	}

	// v0.7 FIX(A): 回填 embeddings usage 到 gin context，
	// 使 usage_collector 能正确核销预算（原来 est_cost 恒为 0）。
	c.Set("usage", core.Usage{
		PromptTokens: resp.Usage.PromptTokens,
		TotalTokens:  resp.Usage.TotalTokens,
	})
	c.Set("stream", false)
	c.Set("resp_object", resp)

	c.JSON(http.StatusOK, resp)
}