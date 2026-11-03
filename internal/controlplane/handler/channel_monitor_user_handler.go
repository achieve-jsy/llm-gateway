package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// ChannelMonitorUserServiceInterface defines the service methods needed by the user-facing handler.
type ChannelMonitorUserServiceInterface interface {
	ListPublic(ctx context.Context) ([]service.UserMonitorView, error)
	GetPublicStatus(ctx context.Context, monitorID int64) (*service.UserMonitorDetail, error)
}

// ChannelMonitorUserHandler handles user-facing channel monitor endpoints.
type ChannelMonitorUserHandler struct {
	svc ChannelMonitorUserServiceInterface
}

// NewChannelMonitorUserHandler creates a new ChannelMonitorUserHandler.
func NewChannelMonitorUserHandler(svc ChannelMonitorUserServiceInterface) *ChannelMonitorUserHandler {
	return &ChannelMonitorUserHandler{svc: svc}
}

// ListPublic returns all enabled monitors with their latest status.
// GET /api/v1/channel-monitors
func (h *ChannelMonitorUserHandler) ListPublic(c *gin.Context) {
	views, err := h.svc.ListPublic(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取渠道状态失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": views})
}

// GetStatus returns detailed status for a single monitor.
// GET /api/v1/channel-monitors/:id/status
func (h *ChannelMonitorUserHandler) GetStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "监控 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	detail, err := h.svc.GetPublicStatus(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取渠道详情失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": detail})
}