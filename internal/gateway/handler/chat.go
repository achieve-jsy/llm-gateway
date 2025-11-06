package handler

import (
	"context"
	"net/http"

	"github.com/example/llm-gateway/internal/gateway/model"
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
	orchestrator ChatOrchestratorInterface
}

// NewChatHandler 创建 ChatHandler 实例
func NewChatHandler(orch ChatOrchestratorInterface) *ChatHandler {
	return &ChatHandler{orchestrator: orch}
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

	// 记录模型名到 context（供日志中间件使用）
	c.Set("model", req.Model)

	// 根据 stream 字段分发
	if req.Stream {
		h.handleStream(c, &req)
		return
	}

	h.handleNonStream(c, &req)
}

// handleNonStream 处理普通（非流式）请求
func (h *ChatHandler) handleNonStream(c *gin.Context, req *model.ChatCompletionRequest) {
	resp, err := h.orchestrator.Chat(c.Request.Context(), req)
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

// handleStream 处理流式请求（委托给 stream handler）
func (h *ChatHandler) handleStream(c *gin.Context, req *model.ChatCompletionRequest) {
	ch, err := h.orchestrator.ChatStream(c.Request.Context(), req)
	if err != nil {
		if ue, ok := err.(*model.UnifiedError); ok {
			respondError(c, ue)
			return
		}
		respondError(c, model.ErrInternal)
		return
	}

	HandleStreamResponse(c, ch)
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
