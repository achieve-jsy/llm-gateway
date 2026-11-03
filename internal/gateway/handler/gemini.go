package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/gin-gonic/gin"
)

// Gemini 原生入站支持的 URL 动作（:modelAction 段的冒号后半部分）。
const (
	geminiActionGenerateContent       = "generateContent"
	geminiActionStreamGenerateContent = "streamGenerateContent"
	geminiActionCountTokens           = "countTokens"
)

// GeminiHandler 处理 Gemini 原生入站请求：
//
//	POST /v1beta/models/{model}:generateContent
//	POST /v1beta/models/{model}:streamGenerateContent[?alt=sse]
//
// 与 /v1/messages 一样是两段式：
//  1. 原生旁路：orchestrator 支持 NativeChatOrchestratorInterface 时，把客户端原始
//     Gemini body 零转换直发上游渠道，响应字节原样回传；
//  2. 降级：只有 core.ErrNativeNotSupported 才用 relaykit 把 Gemini → Chat 转换，
//     走既有调度/预算/配额/Chat 管线，再把 Chat 响应转回 Gemini 格式。
type GeminiHandler struct {
	orchestrator   ChatOrchestratorInterface
	groupDispatch  *orchestrator.GroupAwareDispatcher
	budgetGate     *budget.Gate
	requestQuota   *quota.QuotaMiddleware
	quotaGate      *quota.ModelQuotaGate // v0.15: 模型层配额检查
	protocolPolicy *protocol.Policy      // relaykit 协议策略
}

// NewGeminiHandler 创建 GeminiHandler。
func NewGeminiHandler(orch ChatOrchestratorInterface) *GeminiHandler {
	return &GeminiHandler{orchestrator: orch}
}

// NewGeminiHandlerWithGroup 创建支持模型组调度的 GeminiHandler。
func NewGeminiHandlerWithGroup(orch ChatOrchestratorInterface, gd *orchestrator.GroupAwareDispatcher) *GeminiHandler {
	return &GeminiHandler{orchestrator: orch, groupDispatch: gd}
}

func (h *GeminiHandler) SetBudgetGate(gate *budget.Gate) {
	h.budgetGate = gate
}

func (h *GeminiHandler) SetRequestQuotaGate(gate *quota.QuotaMiddleware) {
	h.requestQuota = gate
}

// SetModelQuotaGate 注入模型配额检查器（router setup 时调用一次）
func (h *GeminiHandler) SetModelQuotaGate(gate *quota.ModelQuotaGate) {
	h.quotaGate = gate
}

// SetProtocolPolicy 注入 relaykit 协议策略。
//
// Gemini 入站只有 relaykit 一条转换实现（没有 legacy 兜底），所以策略不参与
// 分支选择，仅把命中的模式记录到 context 供埋点/排查使用。
// geminiNativeEnabled 判定本次请求是否允许走原生直发。
//
// Gemini 特殊之处：路由是新增的，没有 legacy 实现可对照，所以 shadow 与 active 等价
// （没有影子对照物）。只有显式 legacy 才强制走转换路径 —— 这个开关的价值在于让
// 「关掉某个 Key 的原生直发」可以按策略操作，而不必去改渠道配置。
// 未配置策略时返回 true，保持历史行为，否则升级会静默改变既有客户端的行为。
func (h *GeminiHandler) geminiNativeEnabled(c *gin.Context) bool {
	if h.protocolPolicy == nil {
		return true
	}
	return h.protocolPolicy.ModeFor(c.GetString("key_id"), "gemini") != protocol.ModeLegacy
}

func (h *GeminiHandler) SetProtocolPolicy(p *protocol.Policy) {
	h.protocolPolicy = p
}

// Handle 处理 Gemini 原生 generateContent / streamGenerateContent 请求。
func (h *GeminiHandler) Handle(c *gin.Context) {
	// 1. 先读原始 body：原生旁路必须拿到客户端原字节，不能走 ShouldBindJSON。
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		respondGeminiError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "failed to read request body: "+err.Error())
		return
	}

	// 2. 解析 model + action。
	modelName, action := parseGeminiModelAction(c.Param("modelAction"))
	if modelName == "" {
		respondGeminiError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "model is required in path")
		return
	}
	switch action {
	case geminiActionGenerateContent, geminiActionStreamGenerateContent, geminiActionCountTokens:
	default:
		respondGeminiError(c, http.StatusBadRequest, "INVALID_ARGUMENT",
			"unsupported action: "+action+" (expect generateContent, streamGenerateContent or countTokens)")
		return
	}
	// countTokens 是免费查询：官方语义不产生计费，也不该占用预算/配额，
	// 所以它不从 PrepareAuthorizedGates 走。与 /v1/messages/count_tokens 一致，
	// 用本地校准估算（CJK≈0.66/字 + 每条消息开销），不打上游。
	// 这条路径的价值在于：google-genai SDK 会主动调用它预估成本，早前未支持时
	// 直接 400，等于把 SDK 的 token 预估整段打断。
	if action == geminiActionCountTokens {
		chatReq, _ := buildGeminiChatRequest(rawBody, modelName, false)
		c.Set("model", modelName)
		c.Set("stream", false)
		c.JSON(http.StatusOK, gin.H{"totalTokens": estimatePromptTokens(chatReq.Messages)})
		return
	}

	// ?alt=sse 是 google 客户端对流式的第二种表达，与 streamGenerateContent 等价。
	stream := action == geminiActionStreamGenerateContent || c.Query("alt") == "sse"

	originalModel := modelName
	c.Set("model", originalModel)
	c.Set("stream", stream)
	if h.protocolPolicy != nil {
		c.Set("protocol_mode", string(h.protocolPolicy.ModeFor(c.GetString("key_id"), "gemini")))
	}

	// 3. 构造 core.ChatRequest（Gemini → OpenAI Chat），失败时退化为最小请求，
	// 保证调度/预算/配额照常工作。
	chatReq, session := buildGeminiChatRequest(rawBody, originalModel, stream)

	// 4. 调度/预算/配额（与 /v1/messages 同一套门控，不能绕过计费）。
	routedModelID := originalModel

	releaseQuota, err := PrepareAuthorizedGates(c, originalModel, func() error {
		if h.groupDispatch == nil {
			return nil
		}
		decision, dispatchErr := h.groupDispatch.Dispatch(c, originalModel, chatReq)
		if dispatchErr != nil {
			return dispatchErr
		}
		if decision != nil {
			chatReq.Model = decision.ModelName
			routedModelID = decision.ModelUUID
			// 传递 provider hint，避免同名模型多渠道时走裸 model ID「先注册者赢」。
			if decision.ProviderName != "" {
				chatReq.Provider = decision.ProviderName
				middleware.TrackProviderRequest(c, decision.ProviderName)
			}
		}
		return nil
	}, h.budgetGate, h.requestQuota)
	if err != nil {
		if c.IsAborted() {
			return
		}
		if !budget.WriteHTTPError(c, err) {
			respondGeminiUnifiedError(c, err)
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
		if st, ok := subjectType.(string); ok {
			if si, ok := subjectID.(string); ok {
				if err := h.quotaGate.BeforeUpstream(c, st, si, routedModelID); err != nil {
					return // 已在 middleware 中写入 429 响应
				}
			}
		}
	}

	baseCtx := orchestrator.WithGinContext(c.Request.Context(), c)

	// 5. 原生旁路：必须在门控之后，否则会绕过调度/预算/配额。
	if native, ok := h.orchestrator.(NativeChatOrchestratorInterface); ok && h.geminiNativeEnabled(c) {
		nativeBody := withGeminiBodyModel(rawBody, originalModel)
		if stream {
			nativeStream, nativeErr := native.ChatNativeStream(baseCtx, chatReq, nativeBody, core.NativeRelayGemini)
			if nativeErr == nil {
				c.Set("upstream_protocol", "native_gemini")
				forwardNativeStream(c, nativeStream)
				return
			}
			// 真实错误（非“不支持原生”）：按 Gemini 错误格式返回，不静默降级二次打上游。
			if !errors.Is(nativeErr, core.ErrNativeNotSupported) {
				respondGeminiUnifiedError(c, nativeErr)
				return
			}
		} else {
			out, nativeErr := native.ChatNative(baseCtx, chatReq, nativeBody, core.NativeRelayGemini)
			if nativeErr == nil {
				// 原生响应本身就是干净的 Gemini 格式，不做任何清洗/改写。
				c.Set("upstream_protocol", "native_gemini")
				c.Data(http.StatusOK, "application/json", out)
				return
			}
			if !errors.Is(nativeErr, core.ErrNativeNotSupported) {
				respondGeminiUnifiedError(c, nativeErr)
				return
			}
		}
	}

	// 6. 降级：Chat 管线 + relaykit 回程转换。
	if stream {
		h.handleGeminiStream(c, baseCtx, chatReq, session, originalModel)
		return
	}
	h.handleGeminiNonStream(c, baseCtx, chatReq, session, originalModel)
}

// buildGeminiChatRequest 用 relaykit 把 Gemini 原生请求转成内部 ChatRequest。
//
// 转换失败不返回错误：调度只需要 model + stream，配额/预算/埋点也都只依赖这两项，
// 与其在入口拒绝，不如让请求进入既有管线，由上游给出真实错误。
func buildGeminiChatRequest(rawBody []byte, modelName string, stream bool) (*core.ChatRequest, *protocol.Session) {
	session := protocol.NewSession(
		types.RelayFormatGemini,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		modelName,
		modelName,
	)

	minimal := &core.ChatRequest{Model: modelName, Stream: stream}

	var geminiReq dto.GeminiChatRequest
	if err := json.Unmarshal(rawBody, &geminiReq); err != nil {
		return minimal, session
	}
	result, err := protocol.ConvertRequest(session, &geminiReq)
	if err != nil {
		return minimal, session
	}
	chatReq, err := protocol.BridgeToChatRequest(session, result)
	if err != nil || chatReq == nil {
		return minimal, session
	}
	// Gemini 的流式由 URL action 表达，转换器读不到，必须由这里定稿。
	chatReq.Stream = stream
	if chatReq.Model == "" {
		chatReq.Model = modelName
	}
	return chatReq, session
}

// handleGeminiNonStream 非流式降级：Chat → relaykit → Gemini 响应。
//
// failover 循环与 messages/responses 的转换路径同构：只有「建立请求就失败」时换渠道重试，
// 转换/编码阶段的错误属于本地问题，重试没有意义。
func (h *GeminiHandler) handleGeminiNonStream(c *gin.Context, baseCtx context.Context, req *core.ChatRequest, session *protocol.Session, originalModel string) {
	c.Set("stream", false)
	timeout := h.requestTimeout(originalModel, false)

	var lastErr error

	for {
		roundCtx, cancel := context.WithTimeout(baseCtx, timeout)
		resp, err := h.orchestrator.Chat(roundCtx, req)
		cancel()

		if err == nil {
			if lastErr != nil {
				c.Set("error_message", "")
			}
			h.finishGeminiNonStream(c, session, resp)
			return
		}
		lastErr = err

		if h.groupDispatch == nil || !c.GetBool("routed_via_group") || !shouldTriggerFailover(err) {
			break
		}

		failedID := req.Model
		if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
			failedID = dc.RoutedModelID
		}
		slog.Warn("gemini failover: attempt",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"failed_model", req.Model,
			"failed_model_id", failedID,
			"error", err.Error(),
		)

		h.groupDispatch.NotifyFailure(failedID)
		if is429Error(err) {
			h.groupDispatch.SetCooldown(failedID, defaultCooldownDuration)
		}

		decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID, req)
		if fErr != nil {
			slog.Error("gemini failover: no available target",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"error", fErr.Error(),
			)
			break
		}
		if decision != nil {
			req.Model = decision.ModelName
			req.Provider = decision.ChannelType
			c.Set("failover_used", true)
			slog.Info("gemini failover: switching",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"target", decision.ModelName,
			)
		}
	}

	respondGeminiUnifiedError(c, lastErr)
}

// finishGeminiNonStream 把已拿到的 Chat 响应桥接成 Gemini 格式并写出。
func (h *GeminiHandler) finishGeminiNonStream(c *gin.Context, session *protocol.Session, resp *core.ChatResponse) {
	geminiResult, convErr := protocol.BridgeChatResponseToSource(session, resp)
	if convErr != nil || geminiResult == nil || geminiResult.Value == nil {
		message := "relaykit produced nil response value"
		if convErr != nil {
			message = convErr.Error()
		}
		respondGeminiError(c, http.StatusInternalServerError, "INTERNAL", "response_conversion_failed: "+message)
		return
	}

	payload, marshalErr := protocol.MarshalForClient(geminiResult.Value)
	if marshalErr != nil {
		respondGeminiError(c, http.StatusInternalServerError, "INTERNAL", "response_encode_failed: "+marshalErr.Error())
		return
	}
	c.Set("resp_object", geminiResult.Value)
	c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
}

// handleGeminiStream 流式降级：ChatStream → relaykit SSE → Gemini SSE。
//
// 建立流失败可以换渠道重试；流一旦建立就不再切换（客户端已经可能收到首帧）。
func (h *GeminiHandler) handleGeminiStream(c *gin.Context, baseCtx context.Context, req *core.ChatRequest, session *protocol.Session, originalModel string) {
	c.Set("stream", true)
	timeout := h.requestTimeout(originalModel, true)

	openStream := func(r *core.ChatRequest) (io.ReadCloser, context.CancelFunc, error) {
		streamCtx, cancel := context.WithTimeout(baseCtx, timeout)
		s, e := h.orchestrator.ChatStream(streamCtx, r)
		if e != nil {
			cancel()
			return nil, nil, e
		}
		return s, cancel, nil
	}

	var lastErr error

	for {
		upstream, cancel, err := openStream(req)

		if err == nil {
			if lastErr != nil {
				c.Set("error_message", "")
			}
			geminiStream := protocol.NewStreamAdapter(
				&cancelOnClose{ReadCloser: upstream, cancel: cancel},
				session,
				req.Model,
				estimatePromptTokens(req.Messages),
				"",
			)
			// Gemini SSE 用 data: {...} 帧，出错时用 OpenAI 方言写 data: {"error":...}。
			HandleStreamResponseAs(c, geminiStream, DialectOpenAI)
			return
		}
		lastErr = err

		if h.groupDispatch == nil || !c.GetBool("routed_via_group") || !shouldTriggerFailover(err) {
			break
		}

		failedID := req.Model
		if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
			failedID = dc.RoutedModelID
		}
		slog.Warn("gemini failover: attempt (stream)",
			"request_id", c.GetString("request_id"),
			"group", originalModel,
			"failed_model", req.Model,
			"failed_model_id", failedID,
			"error", err.Error(),
		)

		h.groupDispatch.NotifyFailure(failedID)
		if is429Error(err) {
			h.groupDispatch.SetCooldown(failedID, defaultCooldownDuration)
		}

		decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID, req)
		if fErr != nil {
			slog.Error("gemini failover: no available target (stream)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"error", fErr.Error(),
			)
			break
		}
		if decision != nil {
			req.Model = decision.ModelName
			req.Provider = decision.ChannelType
			c.Set("failover_used", true)
			slog.Info("gemini failover: switching (stream)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"target", decision.ModelName,
			)
		}
	}

	respondGeminiUnifiedError(c, lastErr)
}

// requestTimeout 返回当前请求适用的上游超时：优先模型组配置，否则系统默认值。
func (h *GeminiHandler) requestTimeout(originalModel string, stream bool) time.Duration {
	def := defaultNonStreamTimeout
	if stream {
		def = defaultStreamTimeout
	}
	if h.groupDispatch != nil {
		return h.groupDispatch.GetGroupTimeout(originalModel, def)
	}
	return def
}

// parseGeminiModelAction 拆开 :modelAction 段（形如 "gemini-2.5-pro:generateContent"）。
// 用 LastIndex 而不是 Split：模型名里不会出现 ':'，但路径前缀可能带（如 tunedModels/…）。
func parseGeminiModelAction(modelAction string) (string, string) {
	modelAction = strings.TrimSpace(modelAction)
	if idx := strings.LastIndex(modelAction, ":"); idx >= 0 {
		return strings.TrimSpace(modelAction[:idx]), strings.TrimSpace(modelAction[idx+1:])
	}
	return modelAction, ""
}

// withGeminiBodyModel 把 model 写进原始 Gemini body。
//
// Router 需要 body 里的 model 做改写与路由（改写成 resolveProvider 解析出的实际
// 模型名），Gemini provider 在发往上游前会把这个字段摘掉（模型名只存在于 URL 路径）。
func withGeminiBodyModel(rawBody []byte, modelName string) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &fields); err != nil || fields == nil {
		return rawBody
	}
	modelJSON, err := json.Marshal(modelName)
	if err != nil {
		return rawBody
	}
	fields["model"] = modelJSON
	out, err := json.Marshal(fields)
	if err != nil {
		return rawBody
	}
	return out
}

// respondGeminiError 按 Gemini 错误格式返回：{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}。
func respondGeminiError(c *gin.Context, httpStatus int, status, message string) {
	body := gin.H{
		"error": gin.H{
			"code":    httpStatus,
			"message": message,
			"status":  status,
		},
	}
	middleware.SetTraceResponseObject(c, body)
	c.JSON(httpStatus, body)
}

// respondGeminiUnifiedError 把内部错误（UnifiedError / 组调度错误 / 普通 error）
// 渲染为 Gemini 错误体，状态码与 message 尽量沿用上游语义。
func respondGeminiUnifiedError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	geminiStatus := "INTERNAL"
	message := err.Error()

	switch {
	case errors.Is(err, model.ErrGroupNotFound):
		status, geminiStatus = http.StatusNotFound, "NOT_FOUND"
	case errors.Is(err, model.ErrNoAvailableModel), errors.Is(err, model.ErrNoOtherModelAvailable):
		status, geminiStatus = http.StatusServiceUnavailable, "UNAVAILABLE"
	case errors.Is(err, core.ErrNativeNotSupported):
		// 走到这里说明只允许降级的错误被误当真实错误处理，保持 4xx 提示而不是 500。
		status, geminiStatus = http.StatusBadRequest, "INVALID_ARGUMENT"
	}

	var ue *model.UnifiedError
	if errors.As(err, &ue) {
		if ue.HTTPStatus > 0 {
			status = ue.HTTPStatus
		}
		if ue.Message != "" {
			message = ue.Message
		}
		geminiStatus = mapGeminiErrorStatus(ue.Type)
	}

	respondGeminiError(c, status, geminiStatus, message)
}

// mapGeminiErrorStatus 把网关错误类型映射为 Google API 的 status 字符串。
func mapGeminiErrorStatus(errType model.ErrorType) string {
	switch errType {
	case model.ErrorTypeInvalidRequest:
		return "INVALID_ARGUMENT"
	case model.ErrorTypeModelNotFound:
		return "NOT_FOUND"
	case model.ErrorTypeModelUnavail, model.ErrorTypeTempFailure:
		return "UNAVAILABLE"
	case model.ErrorTypeAuthError:
		return "UNAUTHENTICATED"
	default:
		return "INTERNAL"
	}
}
