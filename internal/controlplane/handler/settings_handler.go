package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/notification"
	"github.com/gin-gonic/gin"
)

type SettingsServiceInterface interface {
	GetSystemSettings(ctx context.Context) (*model.SystemSettings, error)
	GetPublicSettings(ctx context.Context) (*model.PublicSettings, error)
	UpdateSettings(ctx context.Context, settings *model.SystemSettings) error
	ExportAll(ctx context.Context) (map[string]string, error)
	ImportSettings(ctx context.Context, settings map[string]string) error
	GetSMTPPassword(ctx context.Context) string
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
		if errors.Is(err, service.ErrBudgetShadowAndPositive) || errors.Is(err, service.ErrBudgetMasterDisabled) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": err.Error(),
					"code":    CodeBadRequest,
				},
			})
			return
		}
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

func (h *SettingsHandler) TestEmail(c *gin.Context) {
	var req struct {
		Email      string `json:"email"`
		Host       string `json:"smtp_host"`
		Port       int    `json:"smtp_port"`
		From       string `json:"smtp_from"`
		Password   string `json:"smtp_password"`
		SkipVerify bool   `json:"smtp_tls_skip_verify"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "请求参数有误", "code": CodeBadRequest},
		})
		return
	}

	saved, _ := h.svc.GetSystemSettings(c.Request.Context())

	host := req.Host
	port := req.Port
	from := req.From
	password := req.Password
	skipVerify := req.SkipVerify

	// 前端回显的脱敏占位（••••••）不能当真实密码；一律回落已保存明文。
	if service.IsMaskedSecret(password) {
		password = ""
	}

	if host == "" && saved != nil {
		host = saved.SMTPHost
		port = saved.SMTPPort
		from = saved.SMTPFrom
		if !req.SkipVerify {
			skipVerify = saved.SMTPTLSSkipVerify
		}
	}
	if password == "" {
		password = h.svc.GetSMTPPassword(c.Request.Context())
	}

	if host == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "请填写 SMTP 服务器地址", "code": CodeBadRequest},
		})
		return
	}
	if from == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "请填写发件人邮箱", "code": CodeBadRequest},
		})
		return
	}
	if password == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "请填写 SMTP 密码", "code": CodeBadRequest},
		})
		return
	}
	if port == 0 && saved != nil {
		port = saved.SMTPPort
	}
	if port == 0 {
		port = 587
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "请填写测试邮件地址", "code": CodeBadRequest},
		})
		return
	}

	recipients := []string{email}
	sender := notification.NewEmailSender(host, port, from, password, recipients)
	sender.SetSkipTLSVerify(skipVerify)
	if err := sender.SendText(c.Request.Context(),
		"这是来自 LLM Gateway 的测试邮件，收到即表示 SMTP 配置验证成功。\n\n发送时间："+
			notification.FormatLocalTime(time.Now())); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "测试邮件发送失败: " + err.Error(), "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "测试邮件已发送至 " + email,
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
		if errors.Is(err, service.ErrImportForbiddenKey) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"message": err.Error(), "code": CodeBadRequest},
			})
			return
		}
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
