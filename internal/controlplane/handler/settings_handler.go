package handler

import (
	"context"
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/gin-gonic/gin"
)

type SettingsServiceInterface interface {
	GetSystemSettings(ctx context.Context) (*model.SystemSettings, error)
	GetPublicSettings(ctx context.Context) (*model.PublicSettings, error)
	UpdateSettings(ctx context.Context, settings *model.SystemSettings) error
}

type SettingsHandler struct {
	svc SettingsServiceInterface
}

func NewSettingsHandler(svc SettingsServiceInterface) *SettingsHandler {
	return &SettingsHandler{svc: svc}
}

func (h *SettingsHandler) GetSystemSettings(c *gin.Context) {
	settings, err := h.svc.GetSystemSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "获取系统设置失败",
				"code":    CodeInternalError,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    settings,
	})
}

func (h *SettingsHandler) UpdateSettings(c *gin.Context) {
	var input model.SystemSettings
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "请求参数无效",
				"code":    CodeBadRequest,
			},
		})
		return
	}
	if err := h.svc.UpdateSettings(c.Request.Context(), &input); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "更新系统设置失败",
				"code":    CodeInternalError,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
	})
}

func (h *SettingsHandler) GetPublicSettings(c *gin.Context) {
	settings, err := h.svc.GetPublicSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "获取公开设置失败",
				"code":    CodeInternalError,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    settings,
	})
}