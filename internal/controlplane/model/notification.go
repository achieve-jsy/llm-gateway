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

// NotificationChannelRule 通知渠道规则：控制某种通知类型是否允许通过某条渠道发送。
type NotificationChannelRule struct {
	NotificationType string `gorm:"primaryKey;column:notification_type;type:varchar(32)" json:"notification_type"`
	Channel          string `gorm:"primaryKey;column:channel;type:varchar(16)" json:"channel"` // "inapp" | "email"
	Enabled          bool   `gorm:"column:enabled;not null;default:true" json:"enabled"`
}

func (NotificationChannelRule) TableName() string { return "notification_channel_rules" }

// ValidNotificationChannel 判断 channel 是否为合法值。
func ValidNotificationChannel(ch string) bool {
	return ch == "inapp" || ch == "email"
}

// NotificationTypeMeta 通知类型元数据：key + 中文标签
type NotificationTypeMeta struct {
	Type  string `json:"type"`
	Label string `json:"label"`
}

// NotificationChannelMeta 通知渠道元数据：channel key + 中文标签
type NotificationChannelMeta struct {
	Channel string `json:"channel"`
	Label   string `json:"label"`
}

// NotificationMetadata 包含所有通知类型和渠道的元数据
type NotificationMetadata struct {
	Types    []NotificationTypeMeta    `json:"types"`
	Channels []NotificationChannelMeta `json:"channels"`
}

// AllNotificationTypeMetas 返回所有通知类型及其中文标签
func AllNotificationTypeMetas() []NotificationTypeMeta {
	return []NotificationTypeMeta{
		{Type: NotificationTypeBudgetAlert, Label: "预算告警"},
		{Type: NotificationTypeExpansionSubmitted, Label: "扩容申请提交"},
		{Type: NotificationTypeExpansionApproved, Label: "扩容审批通过"},
		{Type: NotificationTypeExpansionRejected, Label: "扩容审批拒绝"},
		{Type: NotificationTypeExpansionDecision, Label: "扩容审批决策"},
		{Type: NotificationTypeExpansionApplyFailed, Label: "扩容执行失败"},
		{Type: NotificationTypeExpansionExpiring, Label: "临时扩容即将到期"},
		{Type: NotificationTypeExpansionRestored, Label: "扩容已恢复"},
		{Type: NotificationTypeBudgetOwnerAttention, Label: "预算负责人提醒"},
		{Type: NotificationTypeKeyActivity, Label: "密钥活动异常"},
		{Type: NotificationTypeKeyRequestApproved, Label: "密钥申请审批通过"},
	}
}

// AllNotificationChannelMetas 返回所有通知渠道及其中文标签
func AllNotificationChannelMetas() []NotificationChannelMeta {
	return []NotificationChannelMeta{
		{Channel: "inapp", Label: "站内信"},
		{Channel: "email", Label: "邮件"},
	}
}
