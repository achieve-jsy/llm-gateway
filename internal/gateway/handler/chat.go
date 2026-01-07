package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/gin-gonic/gin"
)

const (
	// defaultNonStreamTimeout 非流式请求每轮上游超时（含 failover 每轮），对齐 LiteLLM 默认值。
	// 推理类模型（DeepSeek R1、o1 等）单次响应可能超过 30s，保守值过小会导致误 failover。
	defaultNonStreamTimeout = 600 * time.Second
	// defaultStreamTimeout 流式请求总超时，需与 Nginx proxy_read_timeout 保持一致。
	defaultStreamTimeout = 600 * time.Second
)

// ChatOrchestratorInterface 编排器抽象：handler 仅依赖必要方法，便于测试替换
// 实现者：internal/gateway/orchestrator.ChatOrchestrator
type ChatOrchestratorInterface interface {
	Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error)
	ChatStream(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error)
}

// ChatHandler 聊天请求处理器
type ChatHandler struct {
	orchestrator  ChatOrchestratorInterface
	groupDispatch *orchestrator.GroupAwareDispatcher
}

// NewChatHandler 创建 ChatHandler 实例
func NewChatHandler(orch ChatOrchestratorInterface) *ChatHandler {
	return &ChatHandler{orchestrator: orch}
}

// NewChatHandlerWithGroup 创建带模型组调度的 ChatHandler 实例
func NewChatHandlerWithGroup(orch ChatOrchestratorInterface, gd *orchestrator.GroupAwareDispatcher) *ChatHandler {
	return &ChatHandler{orchestrator: orch, groupDispatch: gd}
}

// Handle 处理 POST /v1/chat/completions 请求
// 解析请求 → 判断普通/流式 → 分发处理
//
//	@Summary		Chat Completions
//	@Description	创建聊天补全，兼容 OpenAI /v1/chat/completions 接口
//	@Tags			Chat
//	@Accept			json
//	@Produce		json
//	@Param			request	body		core.ChatRequest	true	"聊天补全请求"
//	@Success		200		{object}	core.ChatResponse	"成功响应"
//	@Failure		400		{object}	map[string]interface{}	"请求参数错误"
//	@Failure		401		{object}	map[string]interface{}	"认证失败"
//	@Failure		404		{object}	map[string]interface{}	"模型不存在"
//	@Failure		500		{object}	map[string]interface{}	"内部错误"
//	@Security		BearerAuth
//	@Router			/v1/chat/completions [post]
func (h *ChatHandler) Handle(c *gin.Context) {
	var req core.ChatRequest
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

	originalModel := req.Model
	c.Set("model", req.Model)

	if h.groupDispatch != nil {
		decision, err := h.groupDispatch.Dispatch(c, req.Model)
		if err != nil {
			h.respondGroupError(c, err)
			return
		}
		if decision != nil {
			req.Model = decision.ModelName
			// 传递 provider hint，确保 Router 在同名模型多 provider 场景下
			// 选择 scheduler 指定的 provider，避免路由歧义。
			if decision.ProviderName != "" {
				req.Provider = decision.ProviderName
			}
		}
	}

	// 将 Gin context 注入请求 context，供 orchestrator 写回 provider/token 信息。
	// baseCtx 不设超时，由各 handle 函数按轮次独立创建超时子 context。
	baseCtx := orchestrator.WithGinContext(c.Request.Context(), c)

	if req.Stream {
		h.handleStream(c, baseCtx, &req, originalModel)
		return
	}

	h.handleNonStream(c, baseCtx, &req, originalModel)
}

// nonStreamTimeout 返回当前请求适用的非流式超时：优先使用组级配置，否则使用系统默认值。
func (h *ChatHandler) nonStreamTimeout(originalModel string) time.Duration {
	if h.groupDispatch != nil {
		return h.groupDispatch.GetGroupTimeout(originalModel, defaultNonStreamTimeout)
	}
	return defaultNonStreamTimeout
}

// streamTimeout 返回当前请求适用的流式超时：优先使用组级配置，否则使用系统默认值。
func (h *ChatHandler) streamTimeout(originalModel string) time.Duration {
	if h.groupDispatch != nil {
		return h.groupDispatch.GetGroupTimeout(originalModel, defaultStreamTimeout)
	}
	return defaultStreamTimeout
}

// handleNonStream 处理普通（非流式）请求
//
// 每次上游调用（primary + failover）都从 baseCtx 派生独立的超时 context，
// 确保 failover 轮次拥有完整的超时预算，不受前一次失败的影响。
func (h *ChatHandler) handleNonStream(c *gin.Context, baseCtx context.Context, req *core.ChatRequest, originalModel string) {
	timeout := h.nonStreamTimeout(originalModel)
	// 第一轮：primary
	primaryCtx, primaryCancel := context.WithTimeout(baseCtx, timeout)
	resp, err := h.orchestrator.Chat(primaryCtx, req)
	primaryCancel()

	if err != nil {
		if h.groupDispatch != nil && req.Model != originalModel {
			// 用 RoutedModelID（数字 ID）而非模型名做 Failover 排除，
			// 避免 failover.go 中字符串比对永远不匹配的 bug。
			failedID := req.Model
			if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
				failedID = dc.RoutedModelID
			}
			slog.Warn("failover: primary failed",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"failed_model", req.Model,
				"failed_model_id", failedID,
				"error", err.Error(),
			)
			if decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID); fErr == nil {
				req.Model = decision.ModelName
				req.Provider = decision.ProviderName // 指定 provider，避免同名模型路由回故障节点
				c.Set("failover_used", true)
				slog.Info("failover: switching",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"target_provider", decision.ProviderName,
					"target_model", decision.ModelName,
					"target_model_id", decision.ModelID,
				)
				// 第二轮：failover —— 用全新的超时 context，不受第一轮超时影响
				failoverCtx, failoverCancel := context.WithTimeout(baseCtx, timeout)
				fResp, fErr2 := h.orchestrator.Chat(failoverCtx, req)
				failoverCancel()
				if fErr2 == nil {
					// failover 成功，清除首次失败写入的 error_message
					c.Set("error_message", "")
					slog.Info("failover: success",
						"request_id", c.GetString("request_id"),
						"group", originalModel,
						"final_provider", decision.ProviderName,
					)
					c.JSON(http.StatusOK, fResp)
					return
				}
				slog.Error("failover: target also failed",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"target_provider", decision.ProviderName,
					"error", fErr2.Error(),
				)
			} else {
				slog.Error("failover: no available target",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"error", fErr.Error(),
				)
			}
		}
		if ue, ok := err.(*model.UnifiedError); ok {
			respondError(c, ue)
			return
		}
		respondError(c, model.ErrInternal)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// handleStream 处理流式请求
//
// 流式场景下，若 primary 建立连接失败才可 failover；一旦流已建立则不中途切换。
// 每次建流尝试从 baseCtx 派生独立超时 context，确保 failover 有完整预算。
func (h *ChatHandler) handleStream(c *gin.Context, baseCtx context.Context, req *core.ChatRequest, originalModel string) {
	timeout := h.streamTimeout(originalModel)
	// openStream 封装"建立一次流"的逻辑：派生超时 ctx，连接失败时立即 cancel。
	// 连接成功时返回 ctx cancel 与 stream，调用方需确保最终调用 cancel。
	openStream := func(r *core.ChatRequest) (io.ReadCloser, context.CancelFunc, error) {
		streamCtx, cancel := context.WithTimeout(baseCtx, timeout)
		s, e := h.orchestrator.ChatStream(streamCtx, r)
		if e != nil {
			cancel()
			return nil, nil, e
		}
		return s, cancel, nil
	}

	// 第一轮：primary
	stream, cancel, err := openStream(req)

	if err != nil {
		if h.groupDispatch != nil && req.Model != originalModel {
			// 用 RoutedModelID 做 Failover 排除
			failedID := req.Model
			if dc := model.GetDispatchContext(c); dc != nil && dc.RoutedModelID != "" {
				failedID = dc.RoutedModelID
			}
			slog.Warn("failover: primary failed (stream)",
				"request_id", c.GetString("request_id"),
				"group", originalModel,
				"failed_model", req.Model,
				"failed_model_id", failedID,
				"error", err.Error(),
			)
			if decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, failedID); fErr == nil {
				req.Model = decision.ModelName
				req.Provider = decision.ProviderName
				c.Set("failover_used", true)
				slog.Info("failover: switching (stream)",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"target_provider", decision.ProviderName,
					"target_model", decision.ModelName,
					"target_model_id", decision.ModelID,
				)
				// 第二轮：failover —— 全新超时 context
				fStream, fCancel, fErr2 := openStream(req)
				if fErr2 == nil {
					c.Set("error_message", "")
					slog.Info("failover: success (stream)",
						"request_id", c.GetString("request_id"),
						"group", originalModel,
						"final_provider", decision.ProviderName,
					)
					// 流关闭时释放 failover ctx
					HandleStreamResponse(c, &cancelOnClose{ReadCloser: fStream, cancel: fCancel})
					return
				}
				slog.Error("failover: target also failed (stream)",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"target_provider", decision.ProviderName,
					"error", fErr2.Error(),
				)
			} else {
				slog.Error("failover: no available target (stream)",
					"request_id", c.GetString("request_id"),
					"group", originalModel,
					"error", fErr.Error(),
				)
			}
		}
		if ue, ok := err.(*model.UnifiedError); ok {
			respondError(c, ue)
			return
		}
		respondError(c, model.ErrInternal)
		return
	}

	// 流关闭时释放 primary ctx
	HandleStreamResponse(c, &cancelOnClose{ReadCloser: stream, cancel: cancel})
}

// cancelOnClose 包装 io.ReadCloser，Close 时调用 cancel 释放超时 context
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (w *cancelOnClose) Close() error {
	w.cancel()
	return w.ReadCloser.Close()
}

func (h *ChatHandler) respondGroupError(c *gin.Context, err error) {
	if err == model.ErrGroupNotFound {
		respondError(c, &model.UnifiedError{
			Type:       model.ErrorTypeModelNotFound,
			Code:       "group_not_found",
			Message:    err.Error(),
			HTTPStatus: http.StatusNotFound,
		})
		return
	}
	if err == model.ErrNoAvailableModel {
		respondError(c, model.ErrModelUnavailable)
		return
	}
	if err == model.ErrNoOtherModelAvailable {
		respondError(c, model.ErrModelUnavailable)
		return
	}
	if ue, ok := err.(*model.UnifiedError); ok {
		respondError(c, ue)
		return
	}
	respondError(c, model.ErrInternal)
}

// respondError 返回统一错误响应 JSON
func respondError(c *gin.Context, ue *model.UnifiedError) {
	c.JSON(ue.HTTPStatus, gin.H{
		"error": gin.H{
			"type":    ue.Type,
			"code":    ue.Code,
			"message": ue.Message,
		},
	})
}

