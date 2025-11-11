package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SyncServiceInterface 同步服务抽象
// 实现者：internal/controlplane/service.SyncService
type SyncServiceInterface interface {
	SyncModels(providerID uuid.UUID) (*service.SyncResult, error)
	ConfirmSync(providerID uuid.UUID, input service.ConfirmSyncInput) (*service.ConfirmSyncResult, error)
}

// SyncHandler 模型同步 Handler
type SyncHandler struct {
	svc SyncServiceInterface
}

// NewSyncHandler 创建 SyncHandler
func NewSyncHandler(svc SyncServiceInterface) *SyncHandler {
	return &SyncHandler{svc: svc}
}

// SyncModels POST /api/v1/providers/:id/sync-models
func (h *SyncHandler) SyncModels(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid provider id", "code": CodeBadRequest},
		})
		return
	}

	result, err := h.svc.SyncModels(id)
	if err != nil {
		msg := err.Error()
		errType := "internal_error"
		code := CodeInternalError
		status := http.StatusInternalServerError

		switch {
		case msg == "provider not found":
			status = http.StatusNotFound
			errType = "not_found"
			code = CodeProviderNotFound
		case msg == "provider is disabled":
			status = http.StatusBadRequest
			errType = "invalid_request"
			code = CodeProviderDisabled
		case len(msg) > 9 && msg[:9] == "upstream ":
			status = http.StatusBadGateway
			errType = CodeUpstreamError
			code = CodeUpstreamError
		}

		c.JSON(status, gin.H{
			"error": gin.H{"type": errType, "message": msg, "code": code},
		})
		return
	}

	if result.Unsupported {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "upstream adapter does not support model sync",
				"code":    CodeSyncNotSupported,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// ConfirmSync POST /api/v1/providers/:id/sync-models/confirm
func (h *SyncHandler) ConfirmSync(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid provider id", "code": CodeBadRequest},
		})
		return
	}

	var input service.ConfirmSyncInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	result, err := h.svc.ConfirmSync(id, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}
