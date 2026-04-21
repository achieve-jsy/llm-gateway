package model

import (
	"time"

	"github.com/google/uuid"
)

// Notification 站内通知：一条记录对应一个用户，系统自动触发（告警、审批结果等）。
// 与 Announcement（管理员手动广播）完全独立。
type Notification struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index:idx_notifications_user_unread" json:"user_id"`
	Type      string    `gorm:"type:varchar(32);not null" json:"type"`                 // budget_alert | expansion_approved | expansion_rejected
	Level     string    `gorm:"type:varchar(16);not null;default:'info'" json:"level"` // info | warning | critical
	Title     string    `gorm:"type:varchar(200);not null" json:"title"`
	Content   string    `gorm:"type:text;not null" json:"content"`
	Source    string    `gorm:"type:varchar(32);not null;default:'direct';uniqueIndex:uniq_notifications_source_dedupe" json:"source"`
	DedupeKey string    `gorm:"type:varchar(255);uniqueIndex:uniq_notifications_source_dedupe" json:"dedupe_key,omitempty"`
	// 关联对象（可选），用于前端跳转
	RefType   string     `gorm:"type:varchar(32);not null;default:''" json:"ref_type"` // budget_scope | expansion_request | key_request
	RefID     string     `gorm:"type:varchar(64);not null;default:''" json:"ref_id"`
	IsRead    bool       `gorm:"not null;default:false;index:idx_notifications_user_unread" json:"is_read"`
	ReadAt    *time.Time `gorm:"type:timestamptz" json:"read_at,omitempty"`
	CreatedAt time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
}

type NotificationDelivery struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	DedupeKey   string    `gorm:"type:varchar(255);not null;uniqueIndex:uniq_notification_delivery_channel"`
	Channel     string    `gorm:"type:varchar(32);not null;uniqueIndex:uniq_notification_delivery_channel"`
	Status      string    `gorm:"type:varchar(16);not null;default:'pending'"`
	Attempts    int       `gorm:"not null;default:0"`
	LastError   string    `gorm:"type:text;not null;default:''"`
	DeliveredAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (NotificationDelivery) TableName() string { return "notification_deliveries" }

func (Notification) TableName() string { return "notifications" }

// 通知类型常量
const (
	NotificationTypeBudgetAlert          = "budget_alert"
	NotificationTypeExpansionApproved    = "expansion_approved"
	NotificationTypeExpansionRejected    = "expansion_rejected"
	NotificationTypeExpansionSubmitted   = "expansion_submitted"
	NotificationTypeExpansionDecision    = "expansion_decision"
	NotificationTypeExpansionApplyFailed = "expansion_apply_failed"
	NotificationTypeExpansionExpiring    = "expansion_expiring"
	NotificationTypeExpansionRestored    = "expansion_restored"
	NotificationTypeBudgetOwnerAttention = "budget_owner_attention"
	NotificationTypeKeyActivity          = "key_activity"
	NotificationTypeKeyRequestApproved   = "key_request_approved"
)

// 通知级别常量（与 Announcement.Level 对齐）
const (
	NotificationLevelInfo     = "info"
	NotificationLevelWarning  = "warning"
	NotificationLevelCritical = "critical"
)
