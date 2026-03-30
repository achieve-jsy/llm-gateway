package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

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
//	@Description	对文档列表进行重排序，兼容 JinaAI /v1/rerank 接口；支持硅基流动式图文扩展
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

	if req.Model == "" {
		gatewayerrors.WriteError(c, "request_missing_model")
		return
	}

	if err := validateRerankQuery(req.Query); err != nil {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage(err.Error()))
		return
	}
	if err := validateRerankDocuments(req.Documents); err != nil {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage(err.Error()))
		return
	}
	if len(req.Documents) > maxDocuments {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage("documents count exceeds maximum of 1000"))
		return
	}
	if req.TopN != nil && *req.TopN <= 0 {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage("top_n must be greater than 0"))
		return
	}

	c.Set("model", req.Model)

	if h.budgetDeps != nil {
		if _, err := middleware.Reserve(c, h.budgetDeps); err != nil {
			if be, ok := err.(*middleware.BudgetExceededError); ok {
				be.WriteHTTPResponse(c)
				return
			}
			slog.Warn("rerank: budget check failed", "model", req.Model, "error", err)
		}
	}

	routedModelID := req.Model

	if h.groupDispatch != nil {
		decision, err := h.groupDispatch.Dispatch(c, req.Model, nil)
		if err != nil {
			respondGroupError(c, err)
			return
		}
		if decision != nil {
			req.Model = decision.ModelName
			req.ModelType = decision.ModelType
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
			}
			routedModelID = decision.ModelID
		}
	}

	if isMultimodalRerankInput(req.Query, req.Documents) && req.ModelType != "rerank_vl" {
		gatewayerrors.WriteError(c, "model_capability_mismatch",
			gatewayerrors.WithMessage("多模态 rerank input 需要使用 rerank_vl 模型或模型组"))
		return
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

	resp, err := h.orchestrator.Rerank(orchestrator.WithGinContext(c.Request.Context(), c), &req)
	if err != nil {
		if ue, ok := err.(*model.UnifiedError); ok {
			respondError(c, ue)
			return
		}
		respondError(c, model.ErrInternal)
		return
	}

	c.Set("usage", core.Usage{
		TotalTokens: resp.Usage.TotalTokens,
	})
	c.Set("search_units", resp.Usage.SearchUnits)
	c.Set("stream", false)
	c.Set("resp_object", resp)

	c.JSON(http.StatusOK, resp)
}

// validateRerankQuery 校验 query：string 非空，或单键 {text|image}。
func validateRerankQuery(query any) error {
	if query == nil {
		return fmt.Errorf("query field is required")
	}
	switch v := query.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("query field is required")
		}
		return nil
	case map[string]any:
		return validateMultimodalPart(v, false)
	default:
		return fmt.Errorf("query must be a string or a single-modality object")
	}
}

// validateRerankDocuments 校验 documents 非空且每项合法。
func validateRerankDocuments(docs []any) error {
	if len(docs) == 0 {
		return fmt.Errorf("documents field is required and must not be empty")
	}
	for i, doc := range docs {
		switch v := doc.(type) {
		case string:
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("documents[%d] must not be empty", i)
			}
		case map[string]any:
			if err := validateMultimodalPart(v, true); err != nil {
				return fmt.Errorf("documents[%d]: %w", i, err)
			}
		default:
			return fmt.Errorf("documents[%d] must be a string or a single-modality object", i)
		}
	}
	return nil
}

// validateMultimodalPart 要求恰好一个非空模态键。
// allowVideo：documents 允许 video；query 不允许。
func validateMultimodalPart(m map[string]any, allowVideo bool) error {
	allowed := map[string]bool{"text": true, "image": true}
	if allowVideo {
		allowed["video"] = true
	}
	found := ""
	for k, raw := range m {
		if !allowed[k] {
			return fmt.Errorf("unsupported modality key %q", k)
		}
		s, ok := raw.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return fmt.Errorf("modality %q must be a non-empty string", k)
		}
		if found != "" {
			return fmt.Errorf("multimodal part must have exactly one modality key")
		}
		found = k
	}
	if found == "" {
		return fmt.Errorf("multimodal part must have exactly one modality key")
	}
	if !allowVideo && found == "video" {
		return fmt.Errorf("query does not support video modality")
	}
	return nil
}

// isMultimodalRerankInput 含 image/video 才算多模态（纯 {text} 仍视为文本）。
func isMultimodalRerankInput(query any, docs []any) bool {
	if partHasVisualModality(query) {
		return true
	}
	for _, d := range docs {
		if partHasVisualModality(d) {
			return true
		}
	}
	return false
}

func partHasVisualModality(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if _, has := m["image"]; has {
		return true
	}
	if _, has := m["video"]; has {
		return true
	}
	return false
}
