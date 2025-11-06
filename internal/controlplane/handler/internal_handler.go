package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// InternalServiceInterface 内部服务抽象
// 实现者：internal/controlplane/service.InternalService
type InternalServiceInterface interface {
	ListAvailableModels() ([]service.AvailableModel, error)
}

// InternalHandler 内部接口 Handler
type InternalHandler struct {
	svc InternalServiceInterface
}

// NewInternalHandler 创建 InternalHandler 实例
func NewInternalHandler(svc InternalServiceInterface) *InternalHandler {
	return &InternalHandler{svc: svc}
}

// ListAvailableModels GET /internal/v1/models/available（T046）
func (h *InternalHandler) ListAvailableModels(c *gin.Context) {
	models, err := h.svc.ListAvailableModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to list available models",
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": models})
}
