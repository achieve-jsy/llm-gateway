package handler

import (
	"context"
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

type SettingsServiceInterface interface {
	GetSystemSettings(ctx context.Context) (*model.SystemSettings, error)
	GetPublicSettings(ctx context.Context) (*model.PublicSettings, error)
	UpdateSettings(ctx context.Context, settings *model.SystemSettings) error
	ExportAll(ctx context.Context) (map[string]string, error)
	ImportSettings(ctx context.Context, settings map[string]string) error
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

func (h *SettingsHandler) TestWebhook(c *gin.Context) {
	s, err := h.svc.GetSystemSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "获取设置失败", "code": CodeInternalError},
		})
		return
	}
	if s == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "获取设置失败", "code": CodeInternalError},
		})
		return
	}
	if !s.WebhookEnabled {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Webhook 未启用", "code": CodeBadRequest},
		})
		return
	}
	if s.WebhookURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Webhook URL 未配置", "code": CodeBadRequest},
		})
		return
	}
	sender := service.NewWebhookSender(s.WebhookURL, s.WebhookSecret)
	if err := sender.SendTest(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "测试发送失败: " + err.Error(), "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "测试发送成功",
	})
}

func (h *SettingsHandler) ExportSettings(c *gin.Context) {
	data, err := h.svc.ExportAll(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "导出设置失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    data,
	})
}

func (h *SettingsHandler) ImportSettings(c *gin.Context) {
	var input map[string]string
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "请求参数无效", "code": CodeBadRequest},
		})
		return
	}
	if err := h.svc.ImportSettings(c.Request.Context(), input); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "导入设置失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
	})
}