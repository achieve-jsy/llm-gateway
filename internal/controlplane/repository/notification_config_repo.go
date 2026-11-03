package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

type NotificationConfigRepo struct {
	db *gorm.DB
}

func NewNotificationConfigRepo(db *gorm.DB) *NotificationConfigRepo {
	return &NotificationConfigRepo{db: db}
}

// Upsert 插入或更新一条渠道配置（以 notification_type + channel 为冲突键）。
func (r *NotificationConfigRepo) Upsert(cfg model.NotificationChannelConfig) error {
	now := time.Now().UTC()
	return r.db.Exec(`
		INSERT INTO notification_channel_configs
			(notification_type, channel, enabled, webhook_url, webhook_secret, email_recipients, subject_template, body_template, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (notification_type, channel) DO UPDATE SET
			enabled          = EXCLUDED.enabled,
			webhook_url      = EXCLUDED.webhook_url,
			webhook_secret   = EXCLUDED.webhook_secret,
			email_recipients = EXCLUDED.email_recipients,
			subject_template = EXCLUDED.subject_template,
			body_template    = EXCLUDED.body_template,
			updated_at       = EXCLUDED.updated_at`,
		cfg.NotificationType, cfg.Channel, cfg.Enabled,
		cfg.WebhookURL, cfg.WebhookSecret, cfg.EmailRecipients,
		cfg.SubjectTemplate, cfg.BodyTemplate,
		now, now,
	).Error
}

// Get 获取单个通知类型+渠道的配置。
func (r *NotificationConfigRepo) Get(notificationType, channel string) (*model.NotificationChannelConfig, error) {
	var cfg model.NotificationChannelConfig
	err := r.db.First(&cfg, "notification_type = ? AND channel = ?", notificationType, channel).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ListByType 获取某种通知类型的所有渠道配置。
func (r *NotificationConfigRepo) ListByType(notificationType string) ([]model.NotificationChannelConfig, error) {
	var cfgs []model.NotificationChannelConfig
	err := r.db.Where("notification_type = ?", notificationType).
		Order("channel").
		Find(&cfgs).Error
	return cfgs, err
}

// ListEnabledByType 获取某种通知类型下所有启用的渠道配置。
func (r *NotificationConfigRepo) ListEnabledByType(notificationType string) ([]model.NotificationChannelConfig, error) {
	var cfgs []model.NotificationChannelConfig
	err := r.db.Where("notification_type = ? AND enabled = ?", notificationType, true).
		Order("channel").
		Find(&cfgs).Error
	return cfgs, err
}

// ListAll 获取全部渠道配置。
func (r *NotificationConfigRepo) ListAll() ([]model.NotificationChannelConfig, error) {
	var cfgs []model.NotificationChannelConfig
	err := r.db.Order("notification_type, channel").Find(&cfgs).Error
	return cfgs, err
}

// Delete 删除一条渠道配置。
func (r *NotificationConfigRepo) Delete(notificationType, channel string) error {
	return r.db.Where("notification_type = ? AND channel = ?", notificationType, channel).
		Delete(&model.NotificationChannelConfig{}).Error
}
