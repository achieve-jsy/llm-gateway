package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"text/template"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/notification"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NotificationManager 统一通知管理器：根据配置决定发往哪个渠道、用什么模板
type NotificationManager interface {
	// Send 根据 notification_channel_configs 配置发送通知到所有启用的渠道
	Send(ctx context.Context, notificationType string, params map[string]interface{}) error

	// SendTo 强制发送到指定渠道（忽略 enabled 状态）
	SendTo(ctx context.Context, notificationType string, params map[string]interface{}, channels []string) error
}

// notificationManager 实现
type notificationManager struct {
	configRepo    *repository.NotificationConfigRepo
	settingsRepo  SettingsRepository
	webhookSender *WebhookSender
	emailSender   *notification.EmailSender
	logger        *slog.Logger

	// 统一通知入口（Phase 2 起启用）新增依赖，全部 nil-safe：
	rulesGate ChannelGate                  // notification_channel_rules 门控
	db        *gorm.DB                     // notification_deliveries 去重
	inappRepo *repository.NotificationRepo // notifications 站内信写入
	email     *EmailNotifier               // per-user 邮件回退路径
}

// NewNotificationManager 创建通知管理器
func NewNotificationManager(
	configRepo *repository.NotificationConfigRepo,
	settingsRepo SettingsRepository,
	webhookSender *WebhookSender,
	emailSender *notification.EmailSender,
	logger *slog.Logger,
) NotificationManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &notificationManager{
		configRepo:    configRepo,
		settingsRepo:  settingsRepo,
		webhookSender: webhookSender,
		emailSender:   emailSender,
		logger:        logger,
	}
}

func (m *notificationManager) Send(ctx context.Context, notificationType string, params map[string]interface{}) error {
	// 1. 查询该类型所有启用的渠道配置
	configs, err := m.configRepo.ListEnabledByType(notificationType)
	if err != nil {
		m.logger.Warn("notification_manager: load channel configs failed, falling back to global",
			"type", notificationType, "error", err)
		configs = nil
	}

	// 2. 如果无配置，fallback 到全局设置
	if len(configs) == 0 {
		return m.sendFallback(ctx, notificationType, params)
	}

	// 3. 对每个启用的渠道发送
	var firstErr error
	for _, cfg := range configs {
		if err := m.sendToChannel(ctx, cfg, params); err != nil {
			m.logger.Warn("notification_manager: send to channel failed",
				"type", notificationType, "channel", cfg.Channel, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (m *notificationManager) SendTo(ctx context.Context, notificationType string, params map[string]interface{}, channels []string) error {
	// 查询该类型的所有配置
	allConfigs, err := m.configRepo.ListByType(notificationType)
	if err != nil {
		m.logger.Warn("notification_manager: load channel configs failed",
			"type", notificationType, "error", err)
		allConfigs = nil
	}

	// 构建目标渠道集合
	targetChannels := make(map[string]bool)
	for _, ch := range channels {
		targetChannels[ch] = true
	}

	var firstErr error
	sent := make(map[string]bool)

	// 发送有配置的渠道（忽略 enabled 状态）
	for _, cfg := range allConfigs {
		if targetChannels[cfg.Channel] {
			if err := m.sendToChannel(ctx, cfg, params); err != nil {
				m.logger.Warn("notification_manager: send to channel failed",
					"type", notificationType, "channel", cfg.Channel, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
			sent[cfg.Channel] = true
		}
	}

	// 对没有配置但指定要发的渠道，使用 fallback
	for _, ch := range channels {
		if !sent[ch] {
			if err := m.sendFallbackChannel(ctx, notificationType, params, ch); err != nil {
				m.logger.Warn("notification_manager: fallback send failed",
					"type", notificationType, "channel", ch, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}

	return firstErr
}

// sendToChannel 发送到单个渠道配置
func (m *notificationManager) sendToChannel(ctx context.Context, cfg model.NotificationChannelConfig, params map[string]interface{}) error {
	// 渲染模板
	subjectTpl := ""
	bodyTpl := ""
	if cfg.SubjectTemplate != nil {
		subjectTpl = *cfg.SubjectTemplate
	}
	if cfg.BodyTemplate != nil {
		bodyTpl = *cfg.BodyTemplate
	}

	subject, body, err := m.renderTemplates(subjectTpl, bodyTpl, cfg.NotificationType, params)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	switch cfg.Channel {
	case "webhook":
		return m.sendWebhook(ctx, cfg, subject, body)
	case "email":
		return m.sendEmail(ctx, cfg, subject, body)
	default:
		return fmt.Errorf("unsupported channel type: %s", cfg.Channel)
	}
}

// sendWebhook 发送 webhook
func (m *notificationManager) sendWebhook(ctx context.Context, cfg model.NotificationChannelConfig, subject, body string) error {
	// 优先用配置中的 URL，否则用全局 URL
	webhookURL := ""
	if cfg.WebhookURL != nil {
		webhookURL = *cfg.WebhookURL
	}
	if webhookURL == "" {
		var err error
		webhookURL, err = m.settingsRepo.GetValue(SettingKeyWebhookURL)
		if err != nil || webhookURL == "" {
			m.logger.Debug("notification_manager: webhook URL not configured, skipping")
			return nil
		}
	}

	webhookSecret := ""
	if cfg.WebhookSecret != nil {
		webhookSecret = *cfg.WebhookSecret
	}

	// 使用 WebhookSender 发送。飞书 text 类型要求 content.text 为字符串，
	// 故用 SendText 拼装（subject + body），而非发送 {title, content} 对象。
	sender := NewWebhookSender(webhookURL, webhookSecret)
	if err := sender.SendText(ctx, subject+"\n\n"+body); err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}

	m.logger.Info("notification_manager: webhook sent", "type", cfg.NotificationType)
	return nil
}

// sendEmail 发送邮件
func (m *notificationManager) sendEmail(ctx context.Context, cfg model.NotificationChannelConfig, subject, body string) error {
	// 优先用配置中的 recipients，否则用全局 emailSender
	recipientsStr := ""
	if cfg.EmailRecipients != nil {
		recipientsStr = *cfg.EmailRecipients
	}

	// 如果配置中有 recipients，创建临时 sender
	if recipientsStr != "" {
		recipients := strings.Split(recipientsStr, ",")
		for i := range recipients {
			recipients[i] = strings.TrimSpace(recipients[i])
		}

		// 从全局设置读取 SMTP 配置
		smtpCfg, err := loadSMTPConfigFromSettings(m.settingsRepo)
		if err != nil {
			return fmt.Errorf("load SMTP config: %w", err)
		}

		sender := notification.NewEmailSender(smtpCfg.host, smtpCfg.port, smtpCfg.from, smtpCfg.password, recipients)
		if smtpCfg.skipVerify {
			sender.SetSkipTLSVerify(true)
		}

		if err := sender.SendTextWithSubject(ctx, subject, body); err != nil {
			return fmt.Errorf("send email: %w", err)
		}

		m.logger.Info("notification_manager: email sent", "type", cfg.NotificationType, "recipients", recipients)
		return nil
	}

	// Fallback: 使用全局 emailSender（如果已初始化）
	if m.emailSender == nil {
		m.logger.Debug("notification_manager: email recipients not configured, skipping")
		return nil
	}

	if err := m.emailSender.SendTextWithSubject(ctx, subject, body); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	m.logger.Info("notification_manager: email sent (global)", "type", cfg.NotificationType)
	return nil
}

// channelEnabled 判断通知类型在某个渠道是否允许发送。
// 由 notification_channel_rules 总开关控制；rulesGate 为空或表内无该行时默认放行。
func (m *notificationManager) channelEnabled(ctx context.Context, notifType, channel string) bool {
	if m.rulesGate == nil {
		return true
	}
	return m.rulesGate.IsEnabled(ctx, notifType, channel)
}

// Notify 统一分发入口：把一条通知按配置路由到 inapp/email/webhook 三渠道。
// 任一渠道失败不阻断其他渠道；被门控跳过或已投递时返回 nil（避免 outbox 误标 failed）。
func (m *notificationManager) Notify(ctx context.Context, n *model.Notification) error {
	if n == nil {
		return nil
	}
	var firstErr error
	for _, dispatch := range []func(context.Context, *model.Notification) error{
		m.dispatchInapp,
		m.dispatchEmail,
		m.dispatchWebhook,
	} {
		if err := dispatch(ctx, n); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// NewUnifiedNotifier 创建统一通知分发器。configRepo/inappRepo/email/rulesGate 均可为 nil（nil-safe）。
func NewUnifiedNotifier(
	configRepo *repository.NotificationConfigRepo,
	settingsRepo SettingsRepository,
	db *gorm.DB,
	inappRepo *repository.NotificationRepo,
	email *EmailNotifier,
	rulesGate ChannelGate,
	logger *slog.Logger,
) Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &notificationManager{
		configRepo:   configRepo,
		settingsRepo: settingsRepo,
		db:           db,
		inappRepo:    inappRepo,
		email:        email,
		rulesGate:    rulesGate,
		logger:       logger,
	}
}

// dispatchInapp 站内信：仅受 rules 总开关控制，写入 notifications 表。
func (m *notificationManager) dispatchInapp(ctx context.Context, n *model.Notification) error {
	if !m.channelEnabled(ctx, n.Type, "inapp") {
		return nil
	}
	if m.inappRepo == nil {
		return nil
	}
	if n.DedupeKey != "" && m.inappDelivered(ctx, n) {
		return nil
	}
	return m.inappRepo.Create(n)
}

// inappDelivered 判断同 (source, dedupe_key) 的站内信是否已存在（幂等跳过）。
func (m *notificationManager) inappDelivered(ctx context.Context, n *model.Notification) bool {
	if m.db == nil {
		return false
	}
	source := n.Source
	if source == "" {
		source = "direct"
	}
	var count int64
	if err := m.db.WithContext(ctx).
		Model(&model.Notification{}).
		Where("source = ? AND dedupe_key = ?", source, n.DedupeKey).
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// dispatchEmail 邮件：rules 总开关 AND config 明细开关 AND 全局 email_enabled。
// config 行存在 → 按 UserID 定向邮箱 + config 模板发送（带去重）；无 config 行 → 回退 per-user EmailNotifier。
// 系统级通知（UserID 为空）没有目标用户邮箱，直接跳过——避免 lookupUserEmail(uuid.Nil)
// 查空邮箱后静默 return，让管理员误以为邮件列生效。系统级告警走 webhook（运维群）。
func (m *notificationManager) dispatchEmail(ctx context.Context, n *model.Notification) error {
	if n.UserID == uuid.Nil {
		return nil
	}
	if !m.channelEnabled(ctx, n.Type, "email") {
		return nil
	}
	if m.configRepo != nil {
		cfg, err := m.configRepo.Get(n.Type, "email")
		if err == nil && cfg != nil {
			// config 行存在即"拥有" email 路径：enabled=false 直接停发，不回落 per-user。
			if !cfg.Enabled {
				return nil
			}
			return m.sendEmailFromConfig(ctx, n, *cfg)
		}
	}
	if m.email == nil {
		return nil
	}
	return m.email.Notify(ctx, n)
}

// sendEmailFromConfig 按 UserID 定向邮箱 + config 模板发送邮件，带全局 email_enabled 检查与投递去重。
// 收件人由系统按通知接收人（用户）自动确定，不在配置里指定。
func (m *notificationManager) sendEmailFromConfig(ctx context.Context, n *model.Notification, cfg model.NotificationChannelConfig) error {
	if !m.emailGlobalEnabled(ctx) {
		return nil
	}
	if m.email == nil {
		return nil
	}
	recipients, err := m.email.lookupUserEmail(ctx, n.UserID)
	if err != nil || len(recipients) == 0 {
		return nil // 无目标邮箱，跳过（不视为错误，避免 outbox 误标 failed）
	}
	subject, body, err := m.renderNotificationTemplates(cfg, n)
	if err != nil {
		return fmt.Errorf("render email template: %w", err)
	}
	smtpCfg, err := loadSMTPConfigFromSettings(m.settingsRepo)
	if err != nil {
		return fmt.Errorf("load SMTP config: %w", err)
	}
	sender := notification.NewEmailSender(smtpCfg.host, smtpCfg.port, smtpCfg.from, smtpCfg.password, recipients)
	if smtpCfg.skipVerify {
		sender.SetSkipTLSVerify(true)
	}
	if n.DedupeKey != "" {
		shouldSend, persisted, err := acquireDelivery(ctx, m.db, n.DedupeKey, "email", n)
		if err != nil {
			return err
		}
		if !shouldSend {
			return nil
		}
		sendErr := sender.SendTextWithSubjectAndDedupeKey(ctx, subject, body, n.DedupeKey)
		return settleDelivery(ctx, m.db, persisted, sendErr)
	}
	return sender.SendTextWithSubject(ctx, subject, body)
}

// dispatchWebhook 飞书 webhook：仅当存在启用的 webhook 配置且 URL 非空时投递，带去重。
func (m *notificationManager) dispatchWebhook(ctx context.Context, n *model.Notification) error {
	if m.configRepo == nil {
		return nil
	}
	cfg, err := m.configRepo.Get(n.Type, "webhook")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // 未配置 webhook 行，跳过
		}
		return fmt.Errorf("load webhook config: %w", err)
	}
	if !cfg.Enabled || cfg.WebhookURL == nil || *cfg.WebhookURL == "" {
		return nil
	}
	subject, body, err := m.renderNotificationTemplates(*cfg, n)
	if err != nil {
		return fmt.Errorf("render webhook template: %w", err)
	}
	if n.DedupeKey != "" {
		shouldSend, persisted, err := acquireDelivery(ctx, m.db, n.DedupeKey, "webhook", n)
		if err != nil {
			return err
		}
		if !shouldSend {
			return nil
		}
		sendErr := m.sendWebhook(ctx, *cfg, subject, body)
		return settleDelivery(ctx, m.db, persisted, sendErr)
	}
	return m.sendWebhook(ctx, *cfg, subject, body)
}

// emailGlobalEnabled 读取全局邮件总开关。
func (m *notificationManager) emailGlobalEnabled(ctx context.Context) bool {
	if m.settingsRepo == nil {
		return false
	}
	enabled, err := m.settingsRepo.GetValue(SettingKeyEmailEnabled)
	if err != nil {
		return false
	}
	return strings.EqualFold(enabled, "true")
}

// renderNotificationTemplates 用 config 模板渲染通知；无自定义模板时直接用 Notification 的标题/内容。
func (m *notificationManager) renderNotificationTemplates(cfg model.NotificationChannelConfig, n *model.Notification) (string, string, error) {
	subject := n.Title
	body := n.Content
	var err error
	if cfg.SubjectTemplate != nil && *cfg.SubjectTemplate != "" {
		if subject, err = m.renderTemplateString(*cfg.SubjectTemplate, n); err != nil {
			return "", "", err
		}
	}
	if cfg.BodyTemplate != nil && *cfg.BodyTemplate != "" {
		if body, err = m.renderTemplateString(*cfg.BodyTemplate, n); err != nil {
			return "", "", err
		}
	}
	return subject, body, nil
}

// renderTemplateString 以 Notification 字段为变量渲染单个模板。
func (m *notificationManager) renderTemplateString(tpl string, n *model.Notification) (string, error) {
	data := map[string]interface{}{
		"Title": n.Title, "Content": n.Content, "Level": n.Level,
		"RefType": n.RefType, "RefID": n.RefID,
		"Timestamp":        time.Now().Format("2006-01-02 15:04:05"),
		"NotificationType": n.Type,
	}
	tmpl, err := template.New("render").Parse(tpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// sendFallback fallback 到全局设置
func (m *notificationManager) sendFallback(ctx context.Context, notificationType string, params map[string]interface{}) error {
	var firstErr error

	// 发送 webhook（如果配置了全局 URL）
	if err := m.sendFallbackChannel(ctx, notificationType, params, "webhook"); err != nil {
		m.logger.Warn("notification_manager: fallback webhook failed", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	// 发送邮件（如果配置了全局 emailSender）
	if err := m.sendFallbackChannel(ctx, notificationType, params, "email"); err != nil {
		m.logger.Warn("notification_manager: fallback email failed", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr == nil {
		m.logger.Debug("notification_manager: fallback sent", "type", notificationType)
	}

	return firstErr
}

// sendFallbackChannel 使用全局设置发送到单个渠道
func (m *notificationManager) sendFallbackChannel(ctx context.Context, notificationType string, params map[string]interface{}, channel string) error {
	subject, body, err := m.renderTemplates("", "", notificationType, params)
	if err != nil {
		return fmt.Errorf("render default template: %w", err)
	}

	// 构造一个虚拟配置（所有字段为 nil）
	cfg := model.NotificationChannelConfig{
		NotificationType: notificationType,
		Channel:          channel,
	}

	switch channel {
	case "webhook":
		return m.sendWebhook(ctx, cfg, subject, body)
	case "email":
		return m.sendEmail(ctx, cfg, subject, body)
	default:
		return fmt.Errorf("unsupported fallback channel: %s", channel)
	}
}

// renderTemplates 渲染主题和正文模板
func (m *notificationManager) renderTemplates(subjectTpl, bodyTpl, notificationType string, params map[string]interface{}) (string, string, error) {
	// 注入公共变量
	data := make(map[string]interface{})
	for k, v := range params {
		data[k] = v
	}
	data["Timestamp"] = time.Now().Format("2006-01-02 15:04:05")
	data["NotificationType"] = notificationType

	// 如果模板为空，使用默认模板
	if subjectTpl == "" {
		if def, ok := DefaultTemplates[notificationType]; ok {
			subjectTpl = def.Subject
		} else {
			subjectTpl = "[LLM Gateway] {{.NotificationType}}"
		}
	}
	if bodyTpl == "" {
		if def, ok := DefaultTemplates[notificationType]; ok {
			bodyTpl = def.Body
		} else {
			bodyTpl = "Notification: {{.NotificationType}}\nTime: {{.Timestamp}}"
		}
	}

	// 渲染 subject
	subjectTmpl, err := template.New("subject").Parse(subjectTpl)
	if err != nil {
		return "", "", fmt.Errorf("parse subject template: %w", err)
	}
	var subjectBuf bytes.Buffer
	if err := subjectTmpl.Execute(&subjectBuf, data); err != nil {
		return "", "", fmt.Errorf("execute subject template: %w", err)
	}

	// 渲染 body
	bodyTmpl, err := template.New("body").Parse(bodyTpl)
	if err != nil {
		return "", "", fmt.Errorf("parse body template: %w", err)
	}
	var bodyBuf bytes.Buffer
	if err := bodyTmpl.Execute(&bodyBuf, data); err != nil {
		return "", "", fmt.Errorf("execute body template: %w", err)
	}

	return subjectBuf.String(), bodyBuf.String(), nil
}

// loadSMTPConfigFromSettings delegates to loadSMTPConfig (defined in notifier.go).
// Kept as alias for backward compatibility with notification_manager callers.
func loadSMTPConfigFromSettings(repo SettingsRepository) (*smtpConfig, error) {
	if r, ok := repo.(*repository.SettingsRepo); ok {
		return loadSMTPConfig(r)
	}
	// Fallback: manually read via interface
	host, _ := repo.GetValue(SettingKeySMTPHost)
	from, _ := repo.GetValue(SettingKeySMTPFrom)
	pwd, _ := repo.GetValue(SettingKeySMTPPassword)
	portStr, _ := repo.GetValue(SettingKeySMTPPort)
	skipStr, _ := repo.GetValue(SettingKeySMTPTLSSkipVerify)

	port := 587
	if portStr != "" {
		if p, err := fmt.Sscanf(portStr, "%d", &port); err != nil || p == 0 {
			port = 587
		}
	}
	return &smtpConfig{
		host:       host,
		from:       from,
		password:   pwd,
		port:       port,
		skipVerify: strings.EqualFold(skipStr, "true"),
	}, nil
}

// SendTestEmail 发送一封测试邮件（供通知配置测试端点使用），目标为调用方传入的单个收件人。
// 复用真实发送的 SMTP 加载与构造逻辑，保证「测试通过」与「真实投递」配置一致。
func SendTestEmail(ctx context.Context, repo SettingsRepository, to, subject, body string) error {
	cfg, err := loadSMTPConfigFromSettings(repo)
	if err != nil {
		return err
	}
	if cfg.host == "" || cfg.from == "" {
		return errors.New("SMTP 未配置，无法发送测试邮件")
	}
	sender := notification.NewEmailSender(cfg.host, cfg.port, cfg.from, cfg.password, []string{to})
	if cfg.skipVerify {
		sender.SetSkipTLSVerify(true)
	}
	return sender.SendTextWithSubject(ctx, subject, body)
}
