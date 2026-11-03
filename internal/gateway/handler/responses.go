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
	"github.com/example/llm-gateway/internal/gateway/privacy"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/example/llm-gateway/internal/gateway/redact"
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

	// redactor 是进程内共享的脱敏引擎（并发安全）。nil 或 off 时所有方法 no-op。
	redactor *redact.Engine
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

// SetRedactor 注入脱敏引擎（router setup 时调用一次；nil 表示不脱敏）。
func (h *ResponsesHandler) SetRedactor(engine *redact.Engine) {
	h.redactor = engine
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

	// 隐私脱敏：整个请求只做一次。必须放在 failover 循环之外——failover 复用
	// 同一个 req，放进循环会把占位符再套一层。放在组调度之后，是为了让复杂度
	// 评分/粘性路由仍然看到原文。
	// 只碰纯文本字段（input 文本 / instructions）；工具定义、tool_call id、
	// 图片/音频/视频 part 一律不动（见 redact.ProtectResponsesInput）。
	var session *redact.Session
	if h.redactor != nil && h.redactor.Mode() != redact.ModeOff {
		session = h.redactor.NewSession()
		defer session.Close() // 每个请求一个 session，绝不复用

		if perr := h.protectRequest(c, &req, session); perr != nil {
			// fail-closed：保护失败绝不能把原文发给上游，也不能 failover——
			// 换渠道只是把同一份原文重发一遍。直接终止请求（500 + redaction_failed）。
			// 只记错误种类，不记正文：失败路径上的报文可能含敏感片段。
			slog.Error("privacy: protect responses request failed, request aborted (fail-closed)",
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

	baseCtx := orchestrator.WithGinContext(c.Request.Context(), c)
	nonStreamTimeout := h.groupTimeout(originalModel, false)
	streamTimeout := h.groupTimeout(originalModel, true)

	if req.Stream {
		h.handleStream(c, baseCtx, &req, originalModel, streamTimeout, session)
		return
	}
	h.handleNonStream(c, baseCtx, &req, originalModel, nonStreamTimeout, session)
}

// protectRequest 就地脱敏 Responses 请求的纯文本字段：input 与 instructions。
func (h *ResponsesHandler) protectRequest(c *gin.Context, req *core.ResponsesRequest, session *redact.Session) error {
	ctx := c.Request.Context()
	input, err := session.ProtectResponsesInput(ctx, req.Input)
	if err != nil {
		return err
	}
	req.Input = input
	instructions, err := session.ProtectText(ctx, req.Instructions)
	if err != nil {
		return err
	}
	req.Instructions = instructions
	return nil
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

func (h *ResponsesHandler) handleNonStream(c *gin.Context, baseCtx context.Context, req *core.ResponsesRequest, originalModel string, timeout time.Duration, session *redact.Session) {
	tctx, cancel := context.WithTimeout(baseCtx, timeout)
	resp, err := h.orchestrator.Responses(tctx, req)
	cancel()

	if err == nil {
		// 写响应前先还原占位符（failover 不会重发请求，只在这一处发生一次）。
		session.RestoreResponses(resp)
		c.JSON(http.StatusOK, resp)
		return
	}

	// failover（仅在通过 group routing 时尝试）
	if h.groupDispatch != nil && c.GetBool("routed_via_group") {
		failedID := req.Model
		if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
			failedID = dc.RoutedModelID
		}
		// /v1/responses 没有 core.ChatRequest，视频约束必须显式传入：
		// 否则 failover 会换到不支持视频的成员，视频被静默丢弃。
		if decision, fErr := h.groupDispatch.HandleFailoverVideo(c, originalModel, failedID, core.HasVideoInput(req.Input)); fErr == nil {
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
				session.RestoreResponses(fResp)
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

func (h *ResponsesHandler) handleStream(c *gin.Context, baseCtx context.Context, req *core.ResponsesRequest, originalModel string, timeout time.Duration, session *redact.Session) {
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
			if decision, fErr := h.groupDispatch.HandleFailoverVideo(c, originalModel, failedID, core.HasVideoInput(req.Input)); fErr == nil {
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
					h.writeStream(c, fStream, fCancel, session)
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

	h.writeStream(c, stream, cancel, session)
}

// writeStream 写出 SSE 流。只有 mask 模式才包还原写入器：detect 模式没有任何
// 映射要还原，包上只会白重写一遍 event 字节；session 为 nil 时原样透传。
func (h *ResponsesHandler) writeStream(c *gin.Context, stream io.ReadCloser, cancel context.CancelFunc, session *redact.Session) {
	var rw *privacy.RestoringWriter
	if session != nil && h.redactor.Enabled() {
		rw = session.RestoringWriter(c.Writer)
	}
	HandleStreamResponseAsRestoring(c, &cancelOnClose{ReadCloser: stream, cancel: cancel}, DialectOpenAI, rw)
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
