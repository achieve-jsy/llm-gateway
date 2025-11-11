package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/auth/service"
	"github.com/gin-gonic/gin"
)

type InternalHandler struct {
	keyAuth *service.KeyAuthService
}

func NewInternalHandler(keyAuth *service.KeyAuthService) *InternalHandler {
	return &InternalHandler{keyAuth: keyAuth}
}

func (h *InternalHandler) VerifyKey(c *gin.Context) {
	var req struct{ Key string `json:"key" binding:"required"` }
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "missing key", "code": "invalid_request"}})
		return
	}
	ctx := c.Request.Context()
	result, err := h.keyAuth.ResolveKey(ctx, req.Key)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "service_unavailable", "message": "verification failed", "code": "service_unavailable"}})
		return
	}
	c.JSON(http.StatusOK, result)
}
