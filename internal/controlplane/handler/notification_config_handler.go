package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"text/template"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NotificationConfigHandler handles notification channel configuration APIs
type NotificationConfigHandler struct {
	repo             *repository.NotificationConfigRepo
	webhookTargetRepo *repository.NotificationWebhookTargetRepo
	settingsRepo     service.SettingsRepository
}

// NewNotificationConfigHandler creates a new handler instance
func NewNotificationConfigHandler(repo *repository.NotificationConfigRepo, webhookTargetRepo *repository.NotificationWebhookTargetRepo, settingsRepo service.SettingsRepository) *NotificationConfigHandler {
	return &NotificationConfigHandler{repo: repo, webhookTargetRepo: webhookTargetRepo, settingsRepo: settingsRepo}
}

// GetMetadata GET /api/v1/controlplane/notification-configs/metadata
func (h *NotificationConfigHandler) GetMetadata(c *gin.Context) {
	metadata := service.GetNotificationMetadata()
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    metadata,
	})
}

// ListAll GET /api/v1/controlplane/notification-configs
func (h *NotificationConfigHandler) ListAll(c *gin.Context) {
	configs, err := h.repo.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "获取通知配置列表失败",
				"code":    CodeInternalError,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    configs,
	})
}

// Get GET /api/v1/controlplane/notification-configs/:type/:channel
func (h *NotificationConfigHandler) Get(c *gin.Context) {
	notifType := c.Param("type")
	channel := c.Param("channel")

	if notifType == "" || channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "通知类型和渠道不能为空",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	if !model.ValidNotificationConfigChannel(channel) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "无效的渠道类型，仅支持 webhook 或 email",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	config, err := h.repo.Get(notifType, channel)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"message": "通知配置不存在",
					"code":    CodeNotFound,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "获取通知配置失败",
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    config,
	})
}

// Update PUT /api/v1/controlplane/notification-configs/:type/:channel
func (h *NotificationConfigHandler) Update(c *gin.Context) {
	notifType := c.Param("type")
	channel := c.Param("channel")

	if notifType == "" || channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "通知类型和渠道不能为空",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	if !model.ValidNotificationConfigChannel(channel) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "无效的渠道类型，仅支持 webhook 或 email",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	var input struct {
		Enabled         *bool   `json:"enabled"`
		WebhookURL      *string `json:"webhook_url"`
		WebhookSecret   *string `json:"webhook_secret"`
		SubjectTemplate *string `json:"subject_template"`
		BodyTemplate    *string `json:"body_template"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "请求参数无效",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	// Get existing or create new
	config, err := h.repo.Get(notifType, channel)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "获取现有配置失败",
				"code":    CodeInternalError,
			},
		})
		return
	}

	if config == nil {
		config = &model.NotificationChannelConfig{
			NotificationType: notifType,
			Channel:          channel,
			Enabled:          true,
		}
	}

	// Apply updates
	if input.Enabled != nil {
		config.Enabled = *input.Enabled
	}
	if input.WebhookURL != nil {
		config.WebhookURL = input.WebhookURL
	}
	if input.WebhookSecret != nil {
		config.WebhookSecret = input.WebhookSecret
	}
	if input.SubjectTemplate != nil {
		config.SubjectTemplate = input.SubjectTemplate
	}
	if input.BodyTemplate != nil {
		config.BodyTemplate = input.BodyTemplate
	}

	// 多 webhook 后端点已迁至 notification_webhook_targets 子表，
	// config 行 webhook_url 仅作 legacy 回退（无端点时兜底），允许为空。
	// 空 URL + 无端点时发送路径无目标、静默跳过，不构成校验失败。

	// 模板保存前语法校验：错误立即拒绝，避免等真实触发渲染时才发现。
	for _, tpl := range []struct {
		name string
		val  *string
	}{
		{"subject_template", config.SubjectTemplate},
		{"body_template", config.BodyTemplate},
	} {
		if tpl.val == nil || *tpl.val == "" {
			continue
		}
		if _, err := template.New("validate").Parse(*tpl.val); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": tpl.name + " 模板语法错误: " + err.Error(),
					"code":    CodeBadRequest,
				},
			})
			return
		}
	}

	if err := h.repo.Upsert(*config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "保存通知配置失败",
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    config,
	})
}

// Test POST /api/v1/controlplane/notification-configs/:type/:channel/test
func (h *NotificationConfigHandler) Test(c *gin.Context) {
	notifType := c.Param("type")
	channel := c.Param("channel")

	if notifType == "" || channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "通知类型和渠道不能为空",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	if !model.ValidNotificationConfigChannel(channel) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "无效的渠道类型，仅支持 webhook 或 email",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	config, err := h.repo.Get(notifType, channel)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"message": "通知配置不存在，请先配置",
					"code":    CodeNotFound,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "获取通知配置失败",
				"code":    CodeInternalError,
			},
		})
		return
	}

	if !config.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "该通知配置已禁用，无法发送测试消息",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	var testParams map[string]interface{}
	if err := c.ShouldBindJSON(&testParams); err != nil {
		testParams = make(map[string]interface{})
	}
	// target_id 为测试目标端点标识（多 webhook 时选择具体端点），不属于模板变量。
	targetID, _ := testParams["target_id"].(string)
	delete(testParams, "target_id")
	// to_email 为测试邮件可选收件人（指定后覆盖当前登录用户邮箱，便于发给真实收件人），不属于模板变量。
	toEmail, _ := testParams["to_email"].(string)
	delete(testParams, "to_email")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	var sendErr error
	var message string

	if channel == "webhook" {
		sendErr, message = h.testWebhook(ctx, config, testParams, targetID)
	} else if channel == "email" {
		targetEmail := toEmail
		if targetEmail == "" {
			targetEmail = c.GetString("user_email")
		}
		sendErr, message = h.testEmail(ctx, config, testParams, targetEmail)
	}

	if sendErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "测试消息发送失败: " + sendErr.Error(),
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": message,
		"data": gin.H{
			"success": true,
		},
	})
}

// notificationTestData 构造测试渲染用的模板变量，与真实发送（service.renderTemplateString）
// 注入的 Notification 字段集合一致。缺失的字段用示例值补齐，避免模板引用的标准变量
// 渲染成 <no value>，让「测试通过」与「真实发送效果」保持一致。
func notificationTestData(params map[string]interface{}) map[string]interface{} {
	data := make(map[string]interface{}, len(params)+7)
	for k, v := range params {
		data[k] = v
	}
	defaults := map[string]interface{}{
		"Title":            "【测试】通知配置测试",
		"Content":          "这是一条测试消息。如果您收到此消息，说明通知配置正确。",
		"Level":            "info",
		"RefType":          "test",
		"RefID":            "test-001",
		"NotificationType": "test",
		"Timestamp":        time.Now().Format("2006-01-02 15:04:05"),
	}
	for k, v := range defaults {
		if _, ok := data[k]; !ok {
			data[k] = v
		}
	}
	return data
}

// testWebhook sends a test webhook message。多 webhook 下按 target_id 选目标端点：
// 显式 target_id → 该端点；否则第一个启用端点；无端点时回退 config 行 URL（legacy 单飞书配置）。
func (h *NotificationConfigHandler) testWebhook(ctx context.Context, config *model.NotificationChannelConfig, params map[string]interface{}, targetID string) (error, string) {
	url, secret := "", ""
	if targetID != "" {
		id, err := uuid.Parse(targetID)
		if err != nil {
			return errors.New("invalid target_id"), "target_id 无效"
		}
		tgt, err := h.webhookTargetRepo.Get(id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("webhook target not found"), "Webhook 端点不存在"
			}
			return err, "获取 Webhook 端点失败"
		}
		url = tgt.URL
		if tgt.Secret != nil {
			secret = *tgt.Secret
		}
	} else if h.webhookTargetRepo != nil {
		if targets, err := h.webhookTargetRepo.ListEnabledByType(config.NotificationType); err == nil && len(targets) > 0 {
			tgt := targets[0]
			url = tgt.URL
			if tgt.Secret != nil {
				secret = *tgt.Secret
			}
		}
	}
	if url == "" {
		if config.WebhookURL == nil || *config.WebhookURL == "" {
			return errors.New("webhook URL not configured"), "Webhook URL 未配置"
		}
		url = *config.WebhookURL
		if config.WebhookSecret != nil {
			secret = *config.WebhookSecret
		}
	}

	sender := service.NewWebhookSender(url, secret)
	data := notificationTestData(params)
	text := data["Content"].(string)

	// Apply body template if exists
	if config.BodyTemplate != nil && *config.BodyTemplate != "" {
		tmpl, err := template.New("body").Parse(*config.BodyTemplate)
		if err == nil {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, data); err == nil {
				text = buf.String()
			}
		}
	}

	if err := sender.SendText(ctx, text); err != nil {
		return err, ""
	}

	return nil, "测试消息发送成功"
}

// ListWebhookTargets GET /api/v1/controlplane/notification-configs/webhook-targets
func (h *NotificationConfigHandler) ListWebhookTargets(c *gin.Context) {
	if h.webhookTargetRepo == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "Webhook 端点仓库未初始化", "code": CodeInternalError},
		})
		return
	}
	targets, err := h.webhookTargetRepo.ListAllTargets()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "获取 Webhook 端点列表失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    targets,
	})
}

// CreateWebhookTarget POST /api/v1/controlplane/notification-configs/:type/webhook/targets
func (h *NotificationConfigHandler) CreateWebhookTarget(c *gin.Context) {
	notifType := c.Param("type")
	if notifType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "通知类型不能为空", "code": CodeBadRequest},
		})
		return
	}
	var input struct {
		Name      string  `json:"name"`
		URL       string  `json:"url"`
		Secret    *string `json:"secret"`
		Enabled   *bool   `json:"enabled"`
		SortOrder int     `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "请求参数无效", "code": CodeBadRequest},
		})
		return
	}
	if input.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Webhook 端点 url 不能为空", "code": CodeBadRequest},
		})
		return
	}
	target := model.NotificationWebhookTarget{
		ID:               uuid.New(),
		NotificationType: notifType,
		Name:             input.Name,
		URL:              input.URL,
		Secret:           input.Secret,
		Enabled:          true,
		SortOrder:        input.SortOrder,
	}
	if input.Enabled != nil {
		target.Enabled = *input.Enabled
	}
	if err := h.webhookTargetRepo.Create(&target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "创建 Webhook 端点失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    target,
	})
}

// UpdateWebhookTarget PUT /api/v1/controlplane/notification-configs/:type/webhook/targets/:id
func (h *NotificationConfigHandler) UpdateWebhookTarget(c *gin.Context) {
	notifType := c.Param("type")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Webhook 端点 id 无效", "code": CodeBadRequest},
		})
		return
	}
	target, err := h.webhookTargetRepo.Get(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"message": "Webhook 端点不存在", "code": CodeNotFound},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "获取 Webhook 端点失败", "code": CodeInternalError},
		})
		return
	}
	if target.NotificationType != notifType {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Webhook 端点与通知类型不匹配", "code": CodeBadRequest},
		})
		return
	}
	var input struct {
		Name      string  `json:"name"`
		URL       string  `json:"url"`
		Secret    *string `json:"secret"`
		Enabled   *bool   `json:"enabled"`
		SortOrder *int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "请求参数无效", "code": CodeBadRequest},
		})
		return
	}
	if input.Name != "" {
		target.Name = input.Name
	}
	if input.URL != "" {
		target.URL = input.URL
	}
	// secret 仅在请求显式携带时更新；nil = 保持不变（前端不回显明文），"" = 清空。
	if input.Secret != nil {
		target.Secret = input.Secret
	}
	if input.Enabled != nil {
		target.Enabled = *input.Enabled
	}
	if input.SortOrder != nil {
		target.SortOrder = *input.SortOrder
	}
	if target.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Webhook 端点 url 不能为空", "code": CodeBadRequest},
		})
		return
	}
	if err := h.webhookTargetRepo.Update(target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "更新 Webhook 端点失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    target,
	})
}

// DeleteWebhookTarget DELETE /api/v1/controlplane/notification-configs/:type/webhook/targets/:id
func (h *NotificationConfigHandler) DeleteWebhookTarget(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Webhook 端点 id 无效", "code": CodeBadRequest},
		})
		return
	}
	if err := h.webhookTargetRepo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "删除 Webhook 端点失败", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
	})
}

// testEmail 渲染模板并发往当前登录用户邮箱（真实 SMTP 投递），验证配置与真实发送一致。
func (h *NotificationConfigHandler) testEmail(ctx context.Context, config *model.NotificationChannelConfig, params map[string]interface{}, targetEmail string) (error, string) {
	if targetEmail == "" {
		return errors.New("current user email is empty"), "无法获取当前用户邮箱，请确认账号已配置邮箱后再测试"
	}

	subject := "【测试】通知配置测试 - " + config.NotificationType
	if config.SubjectTemplate != nil && *config.SubjectTemplate != "" {
		tmpl, err := template.New("subject").Parse(*config.SubjectTemplate)
		if err == nil {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, notificationTestData(params)); err == nil {
				subject = buf.String()
			}
		}
	}

	body := "这是一条测试通知消息。\n\n通知类型: " + config.NotificationType + "\n时间: " + time.Now().Format("2006-01-02 15:04:05")
	if config.BodyTemplate != nil && *config.BodyTemplate != "" {
		tmpl, err := template.New("body").Parse(*config.BodyTemplate)
		if err == nil {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, notificationTestData(params)); err == nil {
				body = buf.String()
			}
		}
	}

	if err := service.SendTestEmail(ctx, h.settingsRepo, targetEmail, subject, body); err != nil {
		return err, ""
	}
	return nil, "测试邮件已发送到 " + targetEmail
}

// httpError represents an HTTP error response
type httpError struct {
	status int
}

func (e *httpError) Error() string {
	return "HTTP " + http.StatusText(e.status)
}
