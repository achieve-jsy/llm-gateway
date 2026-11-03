package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

type NotificationChannelRepo struct {
	db *gorm.DB
}

func NewNotificationChannelRepo(db *gorm.DB) *NotificationChannelRepo {
	return &NotificationChannelRepo{db: db}
}

func (r *NotificationChannelRepo) GetAll() ([]model.NotificationChannelRule, error) {
	var rules []model.NotificationChannelRule
	err := r.db.Order("notification_type, channel").Find(&rules).Error
	return rules, err
}

func (r *NotificationChannelRepo) Upsert(rules []model.NotificationChannelRule) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, rule := range rules {
			if err := tx.Exec(
				`INSERT INTO notification_channel_rules (notification_type, channel, enabled)
				 VALUES (?, ?, ?)
				 ON CONFLICT (notification_type, channel) DO UPDATE SET enabled = EXCLUDED.enabled`,
				rule.NotificationType, rule.Channel, rule.Enabled,
			).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
