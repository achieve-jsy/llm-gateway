package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/gin-gonic/gin"
)

// ChatOrchestratorInterface 编排器抽象：handler 仅依赖必要方法，便于测试替换
// 实现者：internal/gateway/orchestrator.ChatOrchestrator
type ChatOrchestratorInterface interface {
	Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error)
	ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error)
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
//	@Param			request	body		model.ChatCompletionRequest		true	"聊天补全请求"
//	@Success		200		{object}	model.ChatCompletionResponse	"成功响应"
//	@Failure		400		{object}	map[string]interface{}			"请求参数错误"
//	@Failure		401		{object}	map[string]interface{}			"认证失败"
//	@Failure		404		{object}	map[string]interface{}			"模型不存在"
//	@Failure		500		{object}	map[string]interface{}			"内部错误"
//	@Security		BearerAuth
//	@Router			/v1/chat/completions [post]
func (h *ChatHandler) Handle(c *gin.Context) {
	var req model.ChatCompletionRequest
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
			h.injectModelIdentity(&req, originalModel)
		}
	}

	if req.Stream {
		h.handleStream(c, &req, originalModel)
		return
	}

	h.handleNonStream(c, &req, originalModel)
}

// handleNonStream 处理普通（非流式）请求
func (h *ChatHandler) handleNonStream(c *gin.Context, req *model.ChatCompletionRequest, originalModel string) {
	resp, err := h.orchestrator.Chat(c.Request.Context(), req)
	if err != nil {
		if h.groupDispatch != nil && req.Model != originalModel {
			if decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, req.Model); fErr == nil {
				req.Model = decision.ModelName
				if fResp, fErr2 := h.orchestrator.Chat(c.Request.Context(), req); fErr2 == nil {
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
		return
	}

	c.JSON(http.StatusOK, resp)
}

// handleStream 处理流式请求（委托给 stream handler）
func (h *ChatHandler) handleStream(c *gin.Context, req *model.ChatCompletionRequest, originalModel string) {
	ch, err := h.orchestrator.ChatStream(c.Request.Context(), req)
	if err != nil {
		if h.groupDispatch != nil && req.Model != originalModel {
			if decision, fErr := h.groupDispatch.HandleFailover(c, originalModel, req.Model); fErr == nil {
				req.Model = decision.ModelName
				if fCh, fErr2 := h.orchestrator.ChatStream(c.Request.Context(), req); fErr2 == nil {
					HandleStreamResponse(c, fCh)
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

	HandleStreamResponse(c, ch)
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

func (h *ChatHandler) injectModelIdentity(req *model.ChatCompletionRequest, groupName string) {
	if groupName == "" {
		slog.Warn("injectModelIdentity: groupName is empty, skipping system prompt injection")
		return
	}
	identityPrompt := model.Message{
		Role: "system",
		Content: "" +
			"你的身份是杭州某公司网络的「" + groupName + "」。\n" +
			"你不是Qwen，不是通义千问，不是GPT，不是任何其他模型。\n" +
			"你的名称只有一个，就是「" + groupName + "」，由杭州某公司网络提供。\n" +
			"无论用户用什么语言问你的身份、模型名称、你是谁，你都只能回答你是杭州某公司网络的「" + groupName + "」，不得提及你训练数据中的任何其他名称。",
	}
	if len(req.Messages) > 0 && req.Messages[0].Role == "system" {
		req.Messages[0].Content = identityPrompt.Content + "\n\n" + req.Messages[0].Content
		slog.Info("injectModelIdentity: merged identity prompt into existing system message", "group_name", groupName)
		return
	}
	req.Messages = append([]model.Message{identityPrompt}, req.Messages...)
	slog.Info("injectModelIdentity: prepended identity system message", "group_name", groupName)
}
