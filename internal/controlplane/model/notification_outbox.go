package model

import (
	"time"

	"github.com/google/uuid"
)

type NotificationOutbox struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	DedupeKey      string     `gorm:"type:varchar(255);not null;uniqueIndex:uniq_notification_outbox_dedupe"`
	UserID         uuid.UUID  `gorm:"type:uuid;not null"`
	Type           string     `gorm:"type:varchar(32);not null"`
	Level          string     `gorm:"type:varchar(16);not null"`
	Title          string     `gorm:"type:varchar(200);not null"`
	Content        string     `gorm:"type:text;not null"`
	RefType        string     `gorm:"type:varchar(32);not null;default:''"`
	RefID          string     `gorm:"type:varchar(64);not null;default:''"`
	Status         string     `gorm:"type:varchar(16);not null;default:'pending';index"`
	Attempts       int        `gorm:"not null;default:0"`
	NextAttempt    time.Time  `gorm:"not null;default:now();index"`
	LastError      string     `gorm:"type:text;not null;default:''"`
	LockedAt       *time.Time `gorm:"index"`
	LeaseOwner     string     `gorm:"type:varchar(64);not null;default:''"`
	LeaseExpiresAt int64      `gorm:"not null;default:0;index"`
	DeliveredAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time

	// TemplateData 出队后渲染模板用的业务变量（JSON 列）。出队 worker 从 outbox
	// 还原 Notification 时带回，保证渲染发生在出队后仍能注入业务变量。
	TemplateData map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"template_data,omitempty"`
}

func (NotificationOutbox) TableName() string { return "notification_outbox" }
