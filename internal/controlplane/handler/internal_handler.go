package handler

import (
	"errors"
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// InternalServiceInterface 内部服务抽象
// 实现者：internal/controlplane/service.InternalService
type InternalServiceInterface interface {
	ListAvailableModels() ([]service.AvailableModel, error)
	GetModelHealth(names []string) (map[string]service.ModelHealthInfo, error)
}

// InternalHandler 内部接口 Handler
type InternalHandler struct {
	svc    InternalServiceInterface
	keySvc KeyServiceInterface
}

func NewInternalHandler(svc InternalServiceInterface, keySvc KeyServiceInterface) *InternalHandler {
	return &InternalHandler{svc: svc, keySvc: keySvc}
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

// GetModelHealth GET /internal/v1/models/health?names=a,b,c
func (h *InternalHandler) GetModelHealth(c *gin.Context) {
	names := c.QueryArray("names")
	if len(names) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": map[string]service.ModelHealthInfo{}})
		return
	}
	result, err := h.svc.GetModelHealth(names)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to get model health",
				"code":    CodeInternalError,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// VerifyKey POST /internal/v1/keys/verify
func (h *InternalHandler) VerifyKey(c *gin.Context) {
	var input struct {
		Key string `json:"key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": err.Error(),
				"code":    CodeBadRequest,
			},
		})
		return
	}

	result, err := h.keySvc.VerifyKey(c.Request.Context(), input.Key)
	if err != nil {
		if errors.Is(err, service.ErrKeyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found",
					"message": "key not found",
					"code":    CodeKeyNotFound,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to verify key",
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}
