package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// QuotaServiceInterface 配额服务接口
type QuotaServiceInterface interface {
	SetUserQuota(ctx context.Context, userID string, rpm, maxConc *int) error
	GetUserEffectiveQuota(ctx context.Context, userID string) (*service.QuotaEffective, error)
	SetServiceKeyQuota(ctx context.Context, keyID string, rpm, maxConc *int) error
	GetServiceKeyEffectiveQuota(ctx context.Context, keyID string) (*service.QuotaEffective, error)
	SetGroupQuota(ctx context.Context, groupID uint, rpm, maxConc *int) error
	GetGroupEffectiveQuota(ctx context.Context, groupID uint) (*service.QuotaEffective, error)
	SetModelQuota(ctx context.Context, modelID string, rpm, maxConc *int) error
	GetModelEffectiveQuota(ctx context.Context, modelID string) (*service.QuotaEffective, error)
}

// QuotaHandler 配额管理 HTTP 处理器
type QuotaHandler struct {
	svc *service.QuotaService
}

func NewQuotaHandler(svc *service.QuotaService) *QuotaHandler {
	return &QuotaHandler{svc: svc}
}

// QuotaPatchRequest PATCH /api/v1/.../quota 请求体
type QuotaPatchRequest struct {
	RPM            *int `json:"rpm"`
	MaxConcurrency *int `json:"max_concurrency"`
}

func (h *QuotaHandler) SetUserQuota(c *gin.Context) {
	userID := c.Param("id")
	var req QuotaPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "请求参数无效", "code": CodeBadRequest},
		})
		return
	}
	if err := h.svc.SetUserQuota(c.Request.Context(), userID, req.RPM, req.MaxConcurrency); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "设置用户配额失败: " + err.Error(), "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "用户配额已更新"})
}

func (h *QuotaHandler) GetUserQuota(c *gin.Context) {
	userID := c.Param("id")
	eff, err := h.svc.GetUserEffectiveQuota(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": "用户不存在", "code": CodeBadRequest},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": eff})
}

func (h *QuotaHandler) SetServiceKeyQuota(c *gin.Context) {
	keyID := c.Param("id")
	var req QuotaPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "请求参数无效", "code": CodeBadRequest},
		})
		return
	}
	if err := h.svc.SetServiceKeyQuota(c.Request.Context(), keyID, req.RPM, req.MaxConcurrency); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "设置 Service Key 配额失败: " + err.Error(), "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "Service Key 配额已更新"})
}

func (h *QuotaHandler) GetServiceKeyQuota(c *gin.Context) {
	keyID := c.Param("id")
	eff, err := h.svc.GetServiceKeyEffectiveQuota(c.Request.Context(), keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": "Key 不存在或非 Service Key", "code": CodeKeyNotFound},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": eff})
}

func (h *QuotaHandler) SetGroupQuota(c *gin.Context) {
	groupID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "无效的组 ID", "code": CodeBadRequest},
		})
		return
	}
	var req QuotaPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "请求参数无效", "code": CodeBadRequest},
		})
		return
	}
	if err := h.svc.SetGroupQuota(c.Request.Context(), uint(groupID), req.RPM, req.MaxConcurrency); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "设置模型组配额失败: " + err.Error(), "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "模型组配额已更新"})
}

func (h *QuotaHandler) GetGroupQuota(c *gin.Context) {
	groupID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "无效的组 ID", "code": CodeBadRequest},
		})
		return
	}
	eff, err := h.svc.GetGroupEffectiveQuota(c.Request.Context(), uint(groupID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": "模型组不存在", "code": CodeGroupNotFound},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": eff})
}

func (h *QuotaHandler) SetModelQuota(c *gin.Context) {
	modelID := c.Param("id")
	var req QuotaPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "请求参数无效", "code": CodeBadRequest},
		})
		return
	}
	if err := h.svc.SetModelQuota(c.Request.Context(), modelID, req.RPM, req.MaxConcurrency); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "设置模型配额失败: " + err.Error(), "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "模型配额已更新"})
}

func (h *QuotaHandler) GetModelQuota(c *gin.Context) {
	modelID := c.Param("id")
	eff, err := h.svc.GetModelEffectiveQuota(c.Request.Context(), modelID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": "模型不存在", "code": CodeModelNotFound},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": eff})
}

func (h *QuotaHandler) GetMyQuota(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"type": "unauthorized", "message": "未登录", "code": CodeInternalError},
		})
		return
	}
	uid, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "用户 ID 格式错误", "code": CodeInternalError},
		})
		return
	}
	eff, err := h.svc.GetUserEffectiveQuota(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "获取配额失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": eff})
}
