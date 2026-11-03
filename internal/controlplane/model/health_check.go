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

	// 模型检测独立配置（与渠道检测解耦）
	ModelCheckEnabled          bool `gorm:"not null;default:true" json:"model_check_enabled"`
	ModelCheckIntervalSeconds  int  `gorm:"not null;default:300" json:"model_check_interval_seconds"`
	ModelCheckFailureThreshold int  `gorm:"not null;default:3" json:"model_check_failure_threshold"`

	// 模型组检测独立配置（与模型检测解耦）
	GroupCheckEnabled         bool `gorm:"not null;default:true" json:"group_check_enabled"`
	GroupCheckIntervalSeconds int  `gorm:"not null;default:300" json:"group_check_interval_seconds"`

	ModelRecoveryEnabled          bool `gorm:"not null;default:true" json:"model_recovery_enabled"`
	ModelRecoverySuccessThreshold int  `gorm:"not null;default:2" json:"model_recovery_success_threshold"`

	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (HealthCheckConfig) TableName() string { return "health_check_configs" }

// 检测策略的默认值。
//
// 这些值此前散落在各处：GORM tag 里一份、health_check_service 的三个调用点
// 各写一遍 `maxFailures := 3` / `recoveryThreshold := 2`、UI 表单里再一份。
// 想调一个默认策略要改四五个地方，漏掉任何一处都会造成「配置写 5、代码按 3 跑」。
// 统一到这里，其余地方只引用常量。
const (
	DefaultCheckIntervalSeconds           = 300
	DefaultFailureThreshold               = 3
	DefaultModelCheckIntervalSeconds      = 300
	DefaultModelCheckFailureThreshold     = 3
	DefaultGroupCheckIntervalSeconds      = 300
	DefaultModelRecoverySuccessThreshold  = 2
	DefaultTimeoutSeconds                 = 15
)

// ApplyDefaults 把零值字段补齐为默认值。
//
// 由 repository.GetConfig 在读取后调用，因此所有调用方拿到的都是「已补齐」的
// 配置，不必在各自的使用点再写一遍 `if x <= 0 { x = 3 }`。
//
// 注意：只补 <= 0 的字段，不覆盖任何显式配置。
func (c *HealthCheckConfig) ApplyDefaults() {
	if c.IntervalSeconds <= 0 {
		c.IntervalSeconds = DefaultCheckIntervalSeconds
	}
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = DefaultFailureThreshold
	}
	if c.ModelCheckIntervalSeconds <= 0 {
		c.ModelCheckIntervalSeconds = DefaultModelCheckIntervalSeconds
	}
	if c.ModelCheckFailureThreshold <= 0 {
		c.ModelCheckFailureThreshold = DefaultModelCheckFailureThreshold
	}
	if c.GroupCheckIntervalSeconds <= 0 {
		c.GroupCheckIntervalSeconds = DefaultGroupCheckIntervalSeconds
	}
	if c.ModelRecoverySuccessThreshold <= 0 {
		c.ModelRecoverySuccessThreshold = DefaultModelRecoverySuccessThreshold
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = DefaultTimeoutSeconds
	}
}

// HealthCheckLog 健康检测记录
type HealthCheckLog struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TargetType     string    `gorm:"type:varchar(20);not null" json:"target_type"`
	TargetID       uuid.UUID `gorm:"type:uuid;not null" json:"target_id"`
	TargetName     string    `gorm:"type:varchar(200);not null;default:''" json:"target_name"`
	ProviderID     uuid.UUID `gorm:"type:uuid;not null;default:'00000000-0000-0000-0000-000000000000'" json:"provider_id"`
	ProviderName   string    `gorm:"type:varchar(200);not null;default:''" json:"provider_name"`
	Success        bool      `gorm:"not null" json:"success"`
	ResponseTimeMs int       `gorm:"not null;default:0" json:"response_time_ms"`
	ErrorMessage   string    `gorm:"type:text;default:''" json:"error_message"`
	CheckedAt      time.Time `gorm:"not null;autoCreateTime" json:"checked_at"`
}

func (HealthCheckLog) TableName() string { return "health_check_logs" }