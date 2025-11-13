package handler

import (
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// GroupCheckHandler 模型组健康检测 Handler
type GroupCheckHandler struct {
	svc *service.GroupCheckService
}

// NewGroupCheckHandler 创建 GroupCheckHandler 实例
func NewGroupCheckHandler(svc *service.GroupCheckService) *GroupCheckHandler {
	return &GroupCheckHandler{svc: svc}
}

// CheckOne POST /api/v1/groups/:id/check — 检测单个模型组
func (h *GroupCheckHandler) CheckOne(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid group id"}})
		return
	}

	result, err := h.svc.CheckOneByID(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// CheckAll POST /api/v1/groups/check-all — 批量检测所有启用模型组
func (h *GroupCheckHandler) CheckAll(c *gin.Context) {
	result := h.svc.CheckAll(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"data": result})
}
