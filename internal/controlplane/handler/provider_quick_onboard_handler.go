package handler

import (
	"errors"
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

type ProviderQuickOnboardHandler struct {
	svc *service.QuickOnboardService
}

func NewProviderQuickOnboardHandler(svc *service.QuickOnboardService) *ProviderQuickOnboardHandler {
	return &ProviderQuickOnboardHandler{svc: svc}
}

// Preview POST /api/v1/providers/quick-onboard/preview
func (h *ProviderQuickOnboardHandler) Preview(c *gin.Context) {
	var input service.QuickOnboardRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest}})
		return
	}

	preview, err := h.svc.Preview(input)
	if err != nil {
		handleQuickOnboardError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": preview})
}

// Create POST /api/v1/providers/quick-onboard
func (h *ProviderQuickOnboardHandler) Create(c *gin.Context) {
	var input service.QuickOnboardRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest}})
		return
	}

	result, err := h.svc.Create(c.Request.Context(), input)
	if err != nil {
		handleQuickOnboardError(c, err)
		return
	}
	status := http.StatusCreated
	if result.SucceededCount == 0 && result.FailedCount > 0 {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"data": result})
}

func handleQuickOnboardError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrQuickOnboardInvalidRequest) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest}})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError}})
}
