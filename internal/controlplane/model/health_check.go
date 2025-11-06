package model

import (
	"time"

	"github.com/google/uuid"
)

// HealthCheckConfig 健康检测策略配置
type HealthCheckConfig struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Enabled          bool      `gorm:"not null;default:false" json:"enabled"`
	IntervalSeconds  int       `gorm:"not null;default:300" json:"interval_seconds"`
	FailureThreshold int       `gorm:"not null;default:3" json:"failure_threshold"`
	FailureAction    string    `gorm:"type:varchar(20);not null;default:'auto_disable'" json:"failure_action"`
	CheckProviders   bool      `gorm:"not null;default:true" json:"check_providers"`
	CheckModels      bool      `gorm:"not null;default:true" json:"check_models"`
	CheckMode        string    `gorm:"type:varchar(20);not null;default:'connectivity'" json:"check_mode"`
	TimeoutSeconds   int       `gorm:"not null;default:15" json:"timeout_seconds"`
	CreatedAt        time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (HealthCheckConfig) TableName() string { return "health_check_configs" }

// HealthCheckLog 健康检测记录
type HealthCheckLog struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TargetType     string    `gorm:"type:varchar(20);not null" json:"target_type"`
	TargetID       uuid.UUID `gorm:"type:uuid;not null" json:"target_id"`
	TargetName     string    `gorm:"type:varchar(200);not null;default:''" json:"target_name"`
	Success        bool      `gorm:"not null" json:"success"`
	ResponseTimeMs int       `gorm:"not null;default:0" json:"response_time_ms"`
	ErrorMessage   string    `gorm:"type:text;default:''" json:"error_message"`
	CheckedAt      time.Time `gorm:"not null;autoCreateTime" json:"checked_at"`
}

func (HealthCheckLog) TableName() string { return "health_check_logs" }
