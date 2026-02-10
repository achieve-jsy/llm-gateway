package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/keyactivity"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/notification"
	"github.com/google/uuid"
	"gorm.io/gorm"
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

// EmailNotifier 读取 system_configs 中的邮件配置，按配置向告警接收人发送邮件。
// 每次 Notify 重建 Sender，使运行时配置修改无需重启即生效。
type EmailNotifier struct {
	settingsRepo *repository.SettingsRepo
	db           *gorm.DB
}

func NewEmailNotifier(repo *repository.SettingsRepo) *EmailNotifier {
	return &EmailNotifier{settingsRepo: repo, db: repo.DB()}
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

	// 查询通知接收人的邮箱
	recipients, _ := e.lookupUserEmail(ctx, n.UserID)
	if len(recipients) == 0 {
		return nil
	}

	sender := notification.NewEmailSender(smtp.host, smtp.port, smtp.from, smtp.password, recipients)
	if smtp.skipVerify {
		sender.SetSkipTLSVerify(true)
	}

	go e.sendAsync(sender, n, recipients)
	return nil
}

func (e *EmailNotifier) lookupUserEmail(ctx context.Context, userID uuid.UUID) ([]string, error) {
	var email string
	err := e.db.Table("users").
		Where("id = ?", userID).
		Select("COALESCE(email, '')").
		Take(&email).Error
	if err != nil || email == "" {
		return nil, nil
	}
	return []string{email}, nil
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
	slog.Info("email_notifier: sent", "type", n.Type, "recipient", recipients[0])
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
