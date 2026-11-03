package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/example/llm-gateway/internal/core"
	gatewayerrors "github.com/example/llm-gateway/internal/errors"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/example/llm-gateway/internal/gateway/redact"
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
	budgetGate    *budget.Gate
	requestQuota  *quota.QuotaMiddleware
	quotaGate     *quota.ModelQuotaGate
	// redactor 是进程内共享的脱敏引擎（并发安全）。nil 或 off 时所有方法 no-op。
	// 每个请求在 Handle 内 NewSession()，defer Close()，绝不跨请求复用。
	redactor *redact.Engine
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

func (h *RerankHandler) SetBudgetGate(gate *budget.Gate) {
	h.budgetGate = gate
}

func (h *RerankHandler) SetRequestQuotaGate(gate *quota.QuotaMiddleware) {
	h.requestQuota = gate
}

// SetModelQuotaGate injects the model quota gate (called once during router setup).
func (h *RerankHandler) SetModelQuotaGate(gate *quota.ModelQuotaGate) {
	h.quotaGate = gate
}

// SetRedactor 注入脱敏引擎（router setup 时调用一次；nil 表示不脱敏）。
func (h *RerankHandler) SetRedactor(engine *redact.Engine) {
	h.redactor = engine
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

	// 兼容整形：image_url 别名、双 key 收敛、单元素 query 数组解包
	if err := normalizeRerankRequest(&req); err != nil {
		gatewayerrors.WriteError(c, "request_body_invalid", gatewayerrors.WithMessage(err.Error()))
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

	routedModelID := req.Model
	originalModel := req.Model

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
			req.ModelType = decision.ModelType
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
			}
			routedModelID = decision.ModelUUID
		}
		return nil
	}, h.budgetGate, h.requestQuota)
	if err != nil {
		if c.IsAborted() {
			return
		}
		if !budget.WriteHTTPError(c, err) {
			respondGroupError(c, err)
		}
		return
	}
	if releaseQuota != nil {
		defer releaseQuota()
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

	// 隐私脱敏：请求侧只处理纯文本形态，放在转发上游前的最后一步。
	// 没有 failover 循环，天然只脱敏一次。
	// engine 为 nil 或 off 时不创建 session（所有方法本就 no-op）。
	var session *redact.Session
	if h.redactor != nil && h.redactor.Mode() != redact.ModeOff {
		session = h.redactor.NewSession()
		defer session.Close() // 每个请求一个 session，绝不复用
		protectTraceBody(c, session)

		if perr := protectRerankInput(c.Request.Context(), session, &req); perr != nil {
			// fail-closed：保护失败绝不能把原文发给上游。只记错误种类，不记正文。
			slog.Error("privacy: protect rerank request failed, request aborted (fail-closed)",
				"request_id", c.GetString("request_id"),
				"model", req.Model,
				"error", perr.Error(),
			)
			respondError(c, &model.UnifiedError{
				Type:       model.ErrorTypeInternal,
				Code:       "redaction_failed",
				Message:    "privacy protection failed; request not forwarded",
				HTTPStatus: http.StatusInternalServerError,
			})
			return
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

	// 响应可能回显原文档（return_documents=true）：上游拿到的是占位符，
	// 回显文本里也是占位符，客户端看到的必须是原文。
	restoreRerankResponse(session, resp)

	c.Set("usage", core.Usage{
		TotalTokens: resp.Usage.TotalTokens,
	})
	c.Set("search_units", resp.Usage.SearchUnits)
	c.Set("stream", false)
	c.Set("resp_object", resp)

	c.JSON(http.StatusOK, resp)
}

// protectRerankInput 就地脱敏 query 与 documents 的文本形态。
// 调用点在 normalize + validate 之后，此时 query 只可能是 string|{text|image}，
// document 只可能是 string|{text|image|video}；未知形态原样保留。
func protectRerankInput(ctx context.Context, session *redact.Session, req *core.RerankRequest) error {
	if req == nil {
		return nil
	}
	q, err := protectRerankPart(ctx, session, req.Query)
	if err != nil {
		return err
	}
	req.Query = q
	for i := range req.Documents {
		d, err := protectRerankPart(ctx, session, req.Documents[i])
		if err != nil {
			return err
		}
		req.Documents[i] = d
	}
	return nil
}

// protectRerankPart 脱敏单个 query/document：string 与 {text} 走 ProtectText，
// {image} / {video} 是 URL 或 base64，一律不动（视觉模态没有纯文本语义）。
func protectRerankPart(ctx context.Context, session *redact.Session, v any) (any, error) {
	switch x := v.(type) {
	case string:
		return session.ProtectText(ctx, x)
	case map[string]any:
		text, ok := x["text"].(string)
		if !ok {
			return x, nil // image / video：原样返回
		}
		out, err := session.ProtectText(ctx, text)
		if err != nil {
			return nil, err
		}
		x["text"] = out
		return x, nil
	default:
		return v, nil
	}
}

// restoreRerankResponse 还原回显文档里的占位符（detect 模式无映射，等同于 no-op）。
func restoreRerankResponse(session *redact.Session, resp *core.RerankResponse) {
	if session == nil || resp == nil {
		return
	}
	for i := range resp.Results {
		if d := resp.Results[i].Document; d != nil && d.Text != "" {
			d.Text = session.RestoreText(d.Text)
		}
	}
}

// normalizeRerankRequest 在校验前做兼容整形，尽量对齐硅基流动常见写法。
func normalizeRerankRequest(req *core.RerankRequest) error {
	q, err := normalizeRerankPart(req.Query, false)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	req.Query = q

	for i, doc := range req.Documents {
		d, err := normalizeRerankPart(doc, true)
		if err != nil {
			return fmt.Errorf("documents[%d]: %w", i, err)
		}
		req.Documents[i] = d
	}
	return nil
}

// normalizeRerankPart：
// - 单元素数组解包为元素本身
// - image_url → image
// - 多模态双/多 key 时按优先级收敛为单 key：image > video > text
func normalizeRerankPart(v any, allowVideo bool) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		return x, nil
	case []any:
		if len(x) == 0 {
			return nil, fmt.Errorf("empty array is not allowed")
		}
		if len(x) > 1 {
			return nil, fmt.Errorf("array with multiple items is not supported; use a string or single-modality object")
		}
		return normalizeRerankPart(x[0], allowVideo)
	case map[string]any:
		return normalizeModalityObject(x, allowVideo)
	default:
		// json 数字等保持原样，交给后续校验报错
		return v, nil
	}
}

func normalizeModalityObject(m map[string]any, allowVideo bool) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for k, raw := range m {
		key := k
		if key == "image_url" {
			key = "image"
		}
		if existing, ok := out[key]; ok {
			// 同名冲突时保留已有非空值
			if s, ok := existing.(string); ok && strings.TrimSpace(s) != "" {
				continue
			}
		}
		out[key] = raw
	}

	priority := []string{"image", "video", "text"}
	if !allowVideo {
		priority = []string{"image", "text"}
	}

	var chosenKey string
	var chosenVal string
	for _, k := range priority {
		raw, ok := out[k]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		chosenKey, chosenVal = k, s
		break
	}
	if chosenKey == "" {
		// 没有可识别模态时原样返回，由校验给出明确错误
		return out, nil
	}
	return map[string]any{chosenKey: chosenVal}, nil
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
