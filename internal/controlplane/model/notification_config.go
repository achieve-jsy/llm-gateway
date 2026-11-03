package model

import (
	"time"

	"github.com/google/uuid"
)

// NotificationChannelConfig 通知渠道配置：每种通知类型对应一条 webhook 或 email 投递配置。
type NotificationChannelConfig struct {
	NotificationType string    `gorm:"primaryKey;column:notification_type;type:varchar(100)" json:"notification_type"`
	Channel          string    `gorm:"primaryKey;column:channel;type:varchar(50)" json:"channel"` // "webhook" | "email"
	Enabled          bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	WebhookURL       *string   `gorm:"column:webhook_url;type:text" json:"webhook_url,omitempty"`
	WebhookSecret    *string   `gorm:"column:webhook_secret;type:text" json:"webhook_secret,omitempty"`
	EmailRecipients  *string   `gorm:"column:email_recipients;type:text" json:"email_recipients,omitempty"`
	SubjectTemplate  *string   `gorm:"column:subject_template;type:text" json:"subject_template,omitempty"`
	BodyTemplate     *string   `gorm:"column:body_template;type:text" json:"body_template,omitempty"`
	CreatedAt        time.Time `gorm:"column:created_at;not null;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at;not null;autoUpdateTime" json:"updated_at"`
}

func (NotificationChannelConfig) TableName() string { return "notification_channel_configs" }

// NotificationWebhookTarget webhook 端点:一个通知类型可配置多个端点,告警 fan-out 到全部启用端点。
// URL/secret 从 (type, channel) 配置行拆到此处;配置行仍持有 webhook 总开关与共享模板。
type NotificationWebhookTarget struct {
	ID               uuid.UUID `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	NotificationType string    `gorm:"column:notification_type;type:varchar(100);not null" json:"notification_type"`
	Name             string    `gorm:"column:name;type:varchar(100);not null;default:''" json:"name"`
	URL              string    `gorm:"column:url;type:text;not null" json:"url"`
	Secret           *string   `gorm:"column:secret;type:text" json:"secret,omitempty"`
	Enabled          bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	SortOrder        int       `gorm:"column:sort_order;not null;default:0" json:"sort_order"`
	CreatedAt        time.Time `gorm:"column:created_at;not null;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at;not null;autoUpdateTime" json:"updated_at"`
}

func (NotificationWebhookTarget) TableName() string { return "notification_webhook_targets" }

// ValidNotificationConfigChannel 判断 channel 是否为合法的投递渠道。
func ValidNotificationConfigChannel(ch string) bool {
	return ch == "webhook" || ch == "email"
}
