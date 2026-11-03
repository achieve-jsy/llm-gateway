package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

type OpsHandler struct {
	svc *service.OpsService
}

func NewOpsHandler(svc *service.OpsService) *OpsHandler {
	return &OpsHandler{svc: svc}
}

func (h *OpsHandler) GetStatus(c *gin.Context) {
	result, err := h.svc.GetStatus()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "获取系统状态失败"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
