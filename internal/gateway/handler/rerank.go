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

// RerankOrchestratorInterface abstracts the rerank call for testing.
type RerankOrchestratorInterface interface {
	Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error)
}

// RerankHandler handles POST /v1/rerank
type RerankHandler struct {
	orchestrator  RerankOrchestratorInterface
	groupDispatch *orchestrator.GroupAwareDispatcher
	budgetDeps    *middleware.BudgetDeps
	quotaGate     *quota.ModelQuotaGate
}

// NewRerankHandler creates a new RerankHandler.
func NewRerankHandler(orch RerankOrchestratorInterface) *RerankHandler {
	return &RerankHandler{orchestrator: orch}
}

// NewRerankHandlerWithGroup creates a new RerankHandler with group dispatch.
func NewRerankHandlerWithGroup(orch RerankOrchestratorInterface, gd *orchestrator.GroupAwareDispatcher) *RerankHandler {
	return &RerankHandler{orchestrator: orch, groupDispatch: gd}
}

// SetBudgetDeps injects budget dependencies (called once during router setup).
func (h *RerankHandler) SetBudgetDeps(deps *middleware.BudgetDeps) {
	h.budgetDeps = deps
}

// SetModelQuotaGate injects the model quota gate (called once during router setup).
func (h *RerankHandler) SetModelQuotaGate(gate *quota.ModelQuotaGate) {
	h.quotaGate = gate
}

const (
	// maxDocuments is the maximum number of documents allowed in a rerank request.
	maxDocuments = 1000
)

// Handle processes POST /v1/rerank requests.
//
//	@Summary		Rerank
//	@Description	对文档列表进行重排序，兼容 JinaAI /v1/rerank 接口
//	@Tags			Rerank
//	@Accept			json
//	@Produce		json
//	@Param			request	body		core.RerankRequest	true	"重排序请求"
//	@Success		200		{object}	core.RerankResponse	"重排序响应"
//	@Failure		400		{object}	map[string]interface{}	"请求参数错误"
//	@Failure		401		{object}	map[string]interface{}	"认证失败"
//	@Failure		402		{object}	map[string]interface{}	"预算不足"
//	@Failure		404		{object}	map[string]interface{}	"模型未注册"
//	@Failure		429		{object}	map[string]interface{}	"请求频率超限"
//	@Failure		500		{object}	map[string]interface{}	"内部错误"
//	@Security		BearerAuth
//	@Router			/v1/rerank [post]
func (h *RerankHandler) Handle(c *gin.Context) {
	var req core.RerankRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage(err.Error()))
		return
	}

	// 校验 model 非空
	if req.Model == "" {
		gatewayerrors.WriteError(c, "request_missing_model")
		return
	}

	// 校验 documents 非空
	if len(req.Documents) == 0 {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage("documents field is required and must not be empty"))
		return
	}

	// 校验 documents 数量不超过上限
	if len(req.Documents) > maxDocuments {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage("documents count exceeds maximum of 1000"))
		return
	}

	// 校验 top_n > 0（如果提供了 top_n）
	if req.TopN != nil && *req.TopN <= 0 {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage("top_n must be greater than 0"))
		return
	}

	c.Set("model", req.Model)

	// 预算检查
	if h.budgetDeps != nil {
		if _, err := middleware.Reserve(c, h.budgetDeps); err != nil {
			if be, ok := err.(*middleware.BudgetExceededError); ok {
				be.WriteHTTPResponse(c)
				return
			}
			slog.Warn("rerank: budget check failed", "model", req.Model, "error", err)
		}
	}

	// 记录路由选定的模型 ID，用于模型层配额检查。
	routedModelID := req.Model

	// 组调度
	if h.groupDispatch != nil {
		decision, err := h.groupDispatch.Dispatch(c, req.Model, nil)
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
			routedModelID = decision.ModelID
		}
	}

	// 模型层配额检查（路由选定具体 model 之后、转发上游之前）
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
	resp, err := h.orchestrator.Rerank(orchestrator.WithGinContext(c.Request.Context(), c), &req)
	if err != nil {
		if ue, ok := err.(*model.UnifiedError); ok {
			respondError(c, ue)
			return
		}
		respondError(c, model.ErrInternal)
		return
	}

	// 回填 usage 到 gin context，使 usage_collector 能正确核销预算。
	c.Set("usage", core.Usage{
		TotalTokens: resp.Usage.TotalTokens,
	})
	c.Set("search_units", resp.Usage.SearchUnits)
	c.Set("stream", false)
	c.Set("resp_object", resp)

	c.JSON(http.StatusOK, resp)
}