package model

import (
	"time"

	"gorm.io/datatypes"
)

// AlertRule 告警规则
type AlertRule struct {
	ID              uint           `gorm:"primaryKey" json:"id"`
	Name            string         `gorm:"not null" json:"name"`
	Description     string         `gorm:"not null;default:''" json:"description"`
	Enabled         bool           `gorm:"not null;default:true" json:"enabled"`
	MetricType      string         `gorm:"column:metric_type;not null" json:"metric_type"`
	Operator        string         `gorm:"not null" json:"operator"`
	Threshold       float64        `gorm:"not null" json:"threshold"`
	WindowMinutes   int            `gorm:"not null;default:1" json:"window_minutes"`
	SustainedMinutes int           `gorm:"not null;default:2" json:"sustained_minutes"`
	Severity        string         `gorm:"not null;default:'P1'" json:"severity"`
	CooldownMinutes int            `gorm:"not null;default:10" json:"cooldown_minutes"`
	NotifyEmail     bool           `gorm:"not null;default:true" json:"notify_email"`
	Filters         datatypes.JSON `gorm:"default:'{}'" json:"filters"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func (AlertRule) TableName() string { return "alert_rules" }

// AlertEvent 告警事件
type AlertEvent struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	RuleID         uint           `gorm:"not null;index" json:"rule_id"`
	Severity       string         `gorm:"not null" json:"severity"`
	Status         string         `gorm:"not null;default:'firing'" json:"status"`
	Title          string         `gorm:"size:500;not null" json:"title"`
	Description    string         `gorm:"type:text;not null;default:''" json:"description"`
	MetricValue    float64        `gorm:"not null;default:0" json:"metric_value"`
	ThresholdValue float64        `gorm:"not null;default:0" json:"threshold_value"`
	Dimensions     datatypes.JSON `gorm:"default:'{}'" json:"dimensions"`
	FiredAt        time.Time      `gorm:"not null;index" json:"fired_at"`
	ResolvedAt     *time.Time     `json:"resolved_at"`
	EmailSent      bool           `gorm:"not null;default:false" json:"email_sent"`
	CreatedAt      time.Time      `json:"created_at"`
}

func (AlertEvent) TableName() string { return "alert_events" }

// AlertSilence 告警静默
type AlertSilence struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	RuleID    uint      `gorm:"not null;index" json:"rule_id"`
	Platform  string    `gorm:"default:''" json:"platform"`
	GroupID   *int      `json:"group_id"`
	Region    string    `gorm:"default:''" json:"region"`
	Until     time.Time `gorm:"not null;index" json:"until"`
	Reason    string    `gorm:"type:text;not null;default:''" json:"reason"`
	CreatedBy string    `gorm:"default:''" json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

func (AlertSilence) TableName() string { return "alert_silences" }
