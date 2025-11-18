package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AnnouncementRepoInterface interface {
	Create(a *model.Announcement) error
	GetByID(id uuid.UUID) (*model.Announcement, error)
	List(status string, page, pageSize int) ([]model.Announcement, int64, error)
	ListActive() ([]model.Announcement, error)
	ListActivePaged(page, pageSize int) ([]model.Announcement, int64, error)
	Update(a *model.Announcement) error
	Delete(id uuid.UUID) error
	UpdateStatus(id uuid.UUID, status string) error
	MarkRead(userID, announcementID uuid.UUID) error
	GetUnreadCount(userID uuid.UUID) (int64, error)
	GetUnread(userID uuid.UUID) ([]model.Announcement, error)
	GetReadHistory(userID uuid.UUID) ([]model.Announcement, error)
}

type AnnouncementHandler struct {
	repo AnnouncementRepoInterface
}

func NewAnnouncementHandler(repo AnnouncementRepoInterface) *AnnouncementHandler {
	return &AnnouncementHandler{repo: repo}
}

type CreateAnnouncementRequest struct {
	Title       string `json:"title" binding:"required"`
	Content     string `json:"content" binding:"required"`
	Category    string `json:"category"`
	Level       string `json:"level"`
	DisplayMode string `json:"display_mode"`
	SortOrder   int    `json:"sort_order"`
}

type UpdateAnnouncementRequest struct {
	Title       *string `json:"title"`
	Content     *string `json:"content"`
	Category    *string `json:"category"`
	Level       *string `json:"level"`
	DisplayMode *string `json:"display_mode"`
	Status      *string `json:"status"`
	SortOrder   *int    `json:"sort_order"`
}

func (h *AnnouncementHandler) Create(c *gin.Context) {
	var req CreateAnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "标题和内容不能为空", "code": CodeBadRequest},
		})
		return
	}

	level := req.Level
	if level == "" {
		level = "info"
	}
	if level != "info" && level != "warning" && level != "critical" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "公告级别无效", "code": CodeBadRequest},
		})
		return
	}

	category := req.Category
	if category == "" {
		category = "system"
	}
	if category != "system" && category != "feature" && category != "alert" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "公告分类无效", "code": CodeBadRequest},
		})
		return
	}

	displayMode := req.DisplayMode
	if displayMode == "" {
		displayMode = "banner"
	}
	if displayMode != "banner" && displayMode != "popup" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "展示方式无效", "code": CodeBadRequest},
		})
		return
	}

	a := &model.Announcement{
		Title:       req.Title,
		Content:     req.Content,
		Category:    category,
		Level:       level,
		DisplayMode: displayMode,
		Status:      "draft",
		SortOrder:   req.SortOrder,
		CreatedBy:   c.GetString("user_name"),
	}

	if err := h.repo.Create(a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "创建公告失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": a})
}

func (h *AnnouncementHandler) List(c *gin.Context) {
	status := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}

	announcements, total, err := h.repo.List(status, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取公告列表失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      announcements,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (h *AnnouncementHandler) ListActive(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 50 { pageSize = 10 }

	announcements, total, err := h.repo.ListActivePaged(page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取公告失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      announcements,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (h *AnnouncementHandler) GetPublicByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "公告 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	a, err := h.repo.GetByID(id)
	if err != nil || a.Status != "published" {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": "公告不存在", "code": CodeAnnouncementNotFound},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *AnnouncementHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "公告 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	a, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": "公告不存在", "code": CodeAnnouncementNotFound},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *AnnouncementHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "公告 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	a, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": "公告不存在", "code": CodeAnnouncementNotFound},
		})
		return
	}

	var req UpdateAnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "validation_error", "message": "请求参数有误", "code": CodeBadRequest},
		})
		return
	}

	if req.Title != nil {
		a.Title = *req.Title
	}
	if req.Content != nil {
		a.Content = *req.Content
	}
	if req.Category != nil {
		category := *req.Category
		if category != "system" && category != "feature" && category != "alert" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "validation_error", "message": "公告分类无效", "code": CodeBadRequest},
			})
			return
		}
		a.Category = category
	}
	if req.DisplayMode != nil {
		dm := *req.DisplayMode
		if dm != "banner" && dm != "popup" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "validation_error", "message": "展示方式无效", "code": CodeBadRequest},
			})
			return
		}
		a.DisplayMode = dm
	}
	if req.Level != nil {
		level := *req.Level
		if level != "info" && level != "warning" && level != "critical" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "validation_error", "message": "公告级别无效", "code": CodeBadRequest},
			})
			return
		}
		a.Level = level
	}
	if req.SortOrder != nil {
		a.SortOrder = *req.SortOrder
	}
	if req.Status != nil {
		s := *req.Status
		if s != "draft" && s != "published" && s != "archived" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "validation_error", "message": "状态值无效", "code": CodeBadRequest},
			})
			return
		}
		if s == "published" && a.Status != "published" {
			now := time.Now()
			a.PublishedAt = &now
		}
		a.Status = s
	}

	if err := h.repo.Update(a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "更新公告失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *AnnouncementHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "公告 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	if err := h.repo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "删除公告失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "公告已删除"})
}

func (h *AnnouncementHandler) Publish(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "公告 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	if err := h.repo.UpdateStatus(id, "published"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "发布公告失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "公告已发布"})
}

func (h *AnnouncementHandler) Archive(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "公告 ID 无效", "code": CodeBadRequest},
		})
		return
	}

	if err := h.repo.UpdateStatus(id, "archived"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "归档公告失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "公告已归档"})
}

func (h *AnnouncementHandler) MarkRead(c *gin.Context) {
	userID := getUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_id", "message": "公告 ID 无效", "code": CodeBadRequest},
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

func (h *AnnouncementHandler) GetReadHistory(c *gin.Context) {
	userID := getUserID(c)
	announcements, err := h.repo.GetReadHistory(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取已读历史失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": announcements})
}

func (h *AnnouncementHandler) GetUnread(c *gin.Context) {
	userID := getUserID(c)
	announcements, err := h.repo.GetUnread(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取未读公告失败", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": announcements})
}

func (h *AnnouncementHandler) GetUnreadCount(c *gin.Context) {
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

func getUserID(c *gin.Context) uuid.UUID {
	if idStr := c.GetString("user_id"); idStr != "" {
		if uid, err := uuid.Parse(idStr); err == nil {
			return uid
		}
	}
	return uuid.Nil
}