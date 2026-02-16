package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/keyactivity"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/notification"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// Notifier 接口 + 实现：把站内信和邮件解耦，让调用方只管"发一条通知"
// ─────────────────────────────────────────────────────────────────────────────

// Notifier 解耦的通知分发接口。调用方构造一条 Notification 并调用 Notify，
// 内部实现按已配置通道分发（写站内信 + 发邮件等）。
type Notifier interface {
	Notify(ctx context.Context, n *model.Notification) error
}

// ─────────────────────────────────────────────────────────────────────────────
// DBNotifier：站内信通道（写 notifications 表）
// ─────────────────────────────────────────────────────────────────────────────

// DBNotifier 写 notifications 表（站内信）。
type DBNotifier struct {
	repo *repository.NotificationRepo
}

func NewDBNotifier(repo *repository.NotificationRepo) *DBNotifier {
	return &DBNotifier{repo: repo}
}

func (w *DBNotifier) Notify(_ context.Context, n *model.Notification) error {
	if w.repo == nil {
		return nil
	}
	if err := w.repo.Create(n); err != nil {
		slog.Warn("notifier: db write failed", "user_id", n.UserID, "type", n.Type, "error", err)
		return err
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// EmailNotifier：邮件通道（按 ops 邮件通知配置发送给告警收件人）
// ─────────────────────────────────────────────────────────────────────────────

// EmailNotifier 读取 system_configs 中的邮件配置，按配置向告警收件人发送邮件。
// 每次 Notify 重建 Sender，使运行时配置修改无需重启即生效。
type EmailNotifier struct {
	settingsRepo *repository.SettingsRepo
}

func NewEmailNotifier(repo *repository.SettingsRepo) *EmailNotifier {
	return &EmailNotifier{settingsRepo: repo}
}

func (e *EmailNotifier) Notify(ctx context.Context, n *model.Notification) error {
	if e.settingsRepo == nil {
		return nil
	}

	enabled, _ := e.settingsRepo.GetValue(SettingKeyEmailEnabled)
	if !strings.EqualFold(enabled, "true") {
		return nil
	}

	smtp, err := loadSMTPConfig(e.settingsRepo)
	if err != nil {
		slog.Warn("email_notifier: load smtp config failed", "error", err)
		return nil
	}
	if smtp.host == "" || smtp.from == "" {
		return nil
	}

	recipients, _ := loadAlertRecipients(e.settingsRepo)
	if len(recipients) == 0 {
		return nil
	}

	sender := notification.NewEmailSender(smtp.host, smtp.port, smtp.from, smtp.password, recipients)
	if smtp.skipVerify {
		sender.SetSkipTLSVerify(true)
	}

	// 异步发送，不阻塞告警评估链路
	go e.sendAsync(sender, n, recipients)
	return nil
}

func (e *EmailNotifier) sendAsync(sender *notification.EmailSender, n *model.Notification, recipients []string) {
	subject := "[LLM Gateway] " + n.Title
	body := fmt.Sprintf("%s\n\n类型: %s\n级别: %s\n时间: %v\n",
		n.Content, n.Type, n.Level, n.CreatedAt)
	if err := sender.SendTextWithSubject(context.Background(), subject, body); err != nil {
		slog.Warn("email_notifier: send failed",
			"recipients", recipients, "type", n.Type, "error", err)
		return
	}
	slog.Info("email_notifier: sent", "type", n.Type, "count", len(recipients))
}

type smtpConfig struct {
	host, from, password string
	port                 int
	skipVerify           bool
}

func loadSMTPConfig(repo *repository.SettingsRepo) (*smtpConfig, error) {
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

// emailNotificationConfigDTO 与 ops_handler.EmailNotificationConfig 对齐的结构，
// 仅做 JSON 反序列化使用，避免循环依赖。
type emailNotificationConfigDTO struct {
	AlertRecipients []string `json:"alert_recipients"`
}

func loadAlertRecipients(repo *repository.SettingsRepo) ([]string, error) {
	// settingKeyEmailNotification 与 ops_handler 同名配置键保持一致
	const settingKey = "ops_email_notification_config"
	val, err := repo.GetValue(settingKey)
	if err != nil || val == "" {
		return nil, err
	}
	var cfg emailNotificationConfigDTO
	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cfg.AlertRecipients))
	for _, r := range cfg.AlertRecipients {
		r = strings.TrimSpace(r)
		if r != "" {
			out = append(out, r)
		}
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// MultiNotifier：分发到多个通道
// ─────────────────────────────────────────────────────────────────────────────

// MultiNotifier 把一条通知分发到多个通道。任一通道失败不阻断其他通道。
type MultiNotifier struct {
	n []Notifier
}

func NewMultiNotifier(nn ...Notifier) *MultiNotifier {
	return &MultiNotifier{n: nn}
}

func (m *MultiNotifier) Notify(ctx context.Context, n *model.Notification) error {
	var firstErr error
	for _, sub := range m.n {
		if sub == nil {
			continue
		}
		if err := sub.Notify(ctx, n); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ─────────────────────────────────────────────────────────────────────────────
// 适配器：供 keyactivity.Scanner 使用（保持其原有 Notifier 签名）
// ─────────────────────────────────────────────────────────────────────────────

// KeyActivityNotifierAdapter 实现 keyactivity.Notifier，把扁平签名转成 model.Notification 后
// 交给 service.Notifier 分发。
type KeyActivityNotifierAdapter struct {
	inner Notifier
}

func NewKeyActivityNotifierAdapter(n Notifier) keyactivity.Notifier {
	return &KeyActivityNotifierAdapter{inner: n}
}

func (a *KeyActivityNotifierAdapter) Notify(userUUID uuid.UUID, title, body, level, refType, refID string) error {
	return a.inner.Notify(context.Background(), &model.Notification{
		UserID:  userUUID,
		Type:    model.NotificationTypeKeyActivity,
		Level:   level,
		Title:   title,
		Content: body,
		RefType: refType,
		RefID:   refID,
	})
}
