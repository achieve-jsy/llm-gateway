package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// NotificationRepoInterface 供 handler 使用的最小接口。
type NotificationRepoInterface interface {
	GetUnread(userID uuid.UUID) ([]model.Notification, error)
	GetUnreadCount(userID uuid.UUID) (int64, error)
	MarkRead(userID, notificationID uuid.UUID) error
	MarkAllRead(userID uuid.UUID) error
	GetHistory(userID uuid.UUID) ([]model.Notification, error)
	ListAllPaged(channel, notifType, level, keyword, user string, startTime, endTime *time.Time, page, pageSize int) ([]model.AdminNotificationRecord, int64, error)
}

type NotificationHandler struct {
	repo NotificationRepoInterface
}

func NewNotificationHandler(repo NotificationRepoInterface) *NotificationHandler {
	return &NotificationHandler{repo: repo}
}

// GetUnread GET /api/v1/notifications/unread
func (h *NotificationHandler) GetUnread(c *gin.Context) {
	userID := getUserID(c)
	items, err := h.repo.GetUnread(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取未读通知失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// GetUnreadCount GET /api/v1/notifications/unread/count
func (h *NotificationHandler) GetUnreadCount(c *gin.Context) {
	userID := getUserID(c)
	count, err := h.repo.GetUnreadCount(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取未读数量失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": count})
}

// MarkRead POST /api/v1/notifications/:id/read
func (h *NotificationHandler) MarkRead(c *gin.Context) {
	userID := getUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "通知 ID 无效", "code": CodeBadRequest},
		})
		return
	}
	if err := h.repo.MarkRead(userID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "标记已读失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已标记为已读"})
}

// MarkAllRead POST /api/v1/notifications/read-all
func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	userID := getUserID(c)
	if err := h.repo.MarkAllRead(userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "全部标记已读失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "全部已标记为已读"})
}

// GetHistory GET /api/v1/notifications/history
func (h *NotificationHandler) GetHistory(c *gin.Context) {
	userID := getUserID(c)
	items, err := h.repo.GetHistory(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取通知历史失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

// GetAdminHistory GET /api/v1/notifications/admin/history
// 管理端全渠道通知历史（站内信 + email/webhook 投递），支持分页与筛选。
func (h *NotificationHandler) GetAdminHistory(c *gin.Context) {
	channel := c.Query("channel")
	notifType := c.Query("type")
	level := c.Query("level")
	keyword := c.Query("keyword")
	user := c.Query("user")

	var startTime, endTime *time.Time
	for _, f := range []struct {
		name string
		dst  **time.Time
	}{
		{"start_time", &startTime},
		{"end_time", &endTime},
	} {
		if v := c.Query(f.name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": gin.H{"type": "invalid_time", "message": "时间格式无效", "code": CodeBadRequest},
				})
				return
			}
			*f.dst = &t
		}
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	items, total, err := h.repo.ListAllPaged(channel, notifType, level, keyword, user, startTime, endTime, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取通知历史失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"items":     items,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}
