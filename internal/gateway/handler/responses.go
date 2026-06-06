package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/gin-gonic/gin"
)

// ResponsesHandler 处理 OpenAI Responses API 请求。
type ResponsesHandler struct {
	orchestrator  ResponsesOrchestratorInterface
	groupDispatch *orchestrator.GroupAwareDispatcher
	budgetDeps    *middleware.BudgetDeps
	budgetGate    *budget.Gate
	requestQuota  *quota.QuotaMiddleware
	quotaGate     *quota.ModelQuotaGate // v0.15: 模型层配额检查
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

func (h *ResponsesHandler) SetBudgetGate(gate *budget.Gate) {
	h.budgetGate = gate
}

func (h *ResponsesHandler) SetRequestQuotaGate(gate *quota.QuotaMiddleware) {
	h.requestQuota = gate
}

// SetModelQuotaGate 注入模型配额检查器（router setup 时调用一次）
func (h *ResponsesHandler) SetModelQuotaGate(gate *quota.ModelQuotaGate) {
	h.quotaGate = gate
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
	// routedModelUUID 保存路由选定模型的 UUID，用于模型层配额键匹配
	//（dispatch 在下方闭包中执行，需把 decision 带出闭包作用域）。
	routedModelUUID := ""
	c.Set("stream", req.Stream)

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
			req.Provider = decision.ChannelType
			routedModelUUID = decision.ModelUUID
		}
		return nil
	}, h.budgetGate, h.requestQuota)
	if err != nil {
		if c.IsAborted() {
			return
		}
		if !budget.WriteHTTPError(c, err) {
			if ue, ok := err.(*model.UnifiedError); ok {
				respondError(c, ue)
			} else {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": gin.H{"type": "unavailable", "message": "no available model in group", "code": "no_available_model"},
				})
			}
		}
		return
	}
	if releaseQuota != nil {
		defer releaseQuota()
	}

	// v0.15: 模型层配额检查（路由选定具体 model 之后、转发上游之前）
	if h.quotaGate != nil {
		subjectType, _ := c.Get("quota_subject_type")
		subjectID, _ := c.Get("quota_subject_id")
		if subjectType != nil && subjectID != nil {
			if err := h.quotaGate.BeforeUpstream(c, subjectType.(string), subjectID.(string), routedModelUUID); err != nil {
				return // 已在 middleware 中写入 429 响应
			}
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
	if h.groupDispatch != nil && c.GetBool("routed_via_group") {
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
		if h.groupDispatch != nil && c.GetBool("routed_via_group") {
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
