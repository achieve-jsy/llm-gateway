package handler

import (
	"io"
	"net/http"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/gin-gonic/gin"
)

// MessagesHandler 处理 Anthropic 原生 /v1/messages 协议请求。
//
// Claude Code 和 Anthropic SDK 默认调用 POST /v1/messages，本 handler
// 将请求原样透传给 anthropic provider，保留所有 beta headers、thinking
// blocks、原生 SSE 格式，不做 OpenAI 协议转换。
type MessagesHandler struct {
	router *providers.Router
}

// NewMessagesHandler 创建 MessagesHandler 实例
func NewMessagesHandler(router *providers.Router) *MessagesHandler {
	return &MessagesHandler{router: router}
}

// Handle 处理 POST /v1/messages
func (h *MessagesHandler) Handle(c *gin.Context) {
	if h.router == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"message": "router not ready", "code": "service_unavailable"},
		})
		return
	}

	passthroughReq := &core.PassthroughRequest{
		Method:   c.Request.Method,
		Endpoint: "/messages",
		Body:     c.Request.Body,
		Headers:  c.Request.Header.Clone(),
	}

	resp, err := h.router.Passthrough(c.Request.Context(), "anthropic", passthroughReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{"message": err.Error(), "code": "upstream_error"},
		})
		return
	}
	if resp == nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{"message": "empty response", "code": "upstream_error"},
		})
		return
	}

	for key, values := range resp.Headers {
		for _, v := range values {
			c.Header(key, v)
		}
	}
	c.Status(resp.StatusCode)

	if resp.Body != nil {
		defer resp.Body.Close()
		_, _ = io.Copy(c.Writer, resp.Body)
	}
}

// HandleCountTokens 处理 POST /v1/messages/count_tokens
func (h *MessagesHandler) HandleCountTokens(c *gin.Context) {
	if h.router == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"message": "router not ready", "code": "service_unavailable"},
		})
		return
	}

	passthroughReq := &core.PassthroughRequest{
		Method:   c.Request.Method,
		Endpoint: "/messages/count_tokens",
		Body:     c.Request.Body,
		Headers:  c.Request.Header.Clone(),
	}

	resp, err := h.router.Passthrough(c.Request.Context(), "anthropic", passthroughReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{"message": err.Error(), "code": "upstream_error"},
		})
		return
	}
	if resp == nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{"message": "empty response", "code": "upstream_error"},
		})
		return
	}

	for key, values := range resp.Headers {
		for _, v := range values {
			c.Header(key, v)
		}
	}
	c.Status(resp.StatusCode)

	if resp.Body != nil {
		defer resp.Body.Close()
		_, _ = io.Copy(c.Writer, resp.Body)
	}
}