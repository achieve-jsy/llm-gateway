package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GroupOpenModePublic 公开组：全公司默认可见可用
const GroupOpenModePublic = "public"

// GroupOpenModeControlled 受控组：需授权后才可见可用
const GroupOpenModeControlled = "controlled"

type Group struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"type:varchar(64);uniqueIndex:uniq_model_groups_name,where:deleted_at IS NULL;not null" json:"name"`
	Description string         `gorm:"type:text" json:"description"`
	// ModelType 组类型：chat / embedding / embedding_vl / rerank / video（与 models.model_type 对齐）
	ModelType               string `gorm:"type:varchar(50);not null;default:chat;index" json:"model_type"`
	Strategy                string `gorm:"type:varchar(32);not null;default:round_robin;check:strategy IN ('round_robin','priority','weighted','auto')" json:"strategy"`
	Enabled                 bool   `gorm:"not null;default:true;index:idx_model_groups_enabled,where:enabled = TRUE AND deleted_at IS NULL" json:"enabled"`
	MaxSwitches             int    `gorm:"not null;default:2" json:"max_switches"`
	SameModelRetries        int    `gorm:"not null;default:1" json:"same_model_retries"`
	FailoverCooldownSeconds int    `gorm:"not null;default:60" json:"failover_cooldown_seconds"`
	TimeoutSeconds          int    `gorm:"not null;default:0" json:"timeout_seconds"`
	OpenMode                string `gorm:"type:varchar(16);not null;default:'public';check:open_mode IN ('public','controlled')" json:"open_mode"`
	IsCapabilityOverride    bool   `gorm:"not null;default:false" json:"is_capability_override"`
	OverrideSupportsVision  bool   `gorm:"not null;default:false" json:"override_supports_vision"`
	OverrideSupportsFC      bool   `gorm:"not null;default:false;column:override_supports_function_calling" json:"override_supports_function_calling"`
	OverrideSupportsTC      bool   `gorm:"not null;default:false;column:override_supports_tool_choice" json:"override_supports_tool_choice"`
	// 四层配额体系（Phase 1）
	// NULL = 不限，运行时按主体独立计数
	QuotaRPM            *int       `gorm:"column:quota_rpm" json:"quota_rpm,omitempty"`
	QuotaMaxConcurrency *int       `gorm:"column:quota_max_concurrency" json:"quota_max_concurrency,omitempty"`
	BudgetTierID        *uuid.UUID `gorm:"type:uuid;index:idx_model_groups_budget_tier,where:budget_tier_id IS NOT NULL AND deleted_at IS NULL" json:"budget_tier_id,omitempty"`
	BudgetTier          *ModelTier `gorm:"foreignKey:BudgetTierID;constraint:OnDelete:RESTRICT" json:"budget_tier,omitempty"`
	// Session 粘性路由配置（NULL = 跟随全局默认，TRUE/FALSE = 组级显式覆盖）
	StickyEnabled    *bool        `gorm:"default:null" json:"sticky_enabled"`
	StickyTTLSeconds *int         `gorm:"default:null" json:"sticky_ttl_seconds"`
	Models           []GroupModel `gorm:"foreignKey:GroupID" json:"models,omitempty"`
}

func (Group) TableName() string {
	return "model_groups"
}

type GroupModel struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	GroupID        uint      `gorm:"not null;uniqueIndex:uniq_group_model;index" json:"group_id"`
	ModelID        uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uniq_group_model;index:idx_group_models_model_id" json:"model_id"`
	ModelName      string    `gorm:"->" json:"model_name"`
	Priority       *int      `gorm:"default:null" json:"priority"`
	Weight         *int      `gorm:"default:null;check:weight >= 1 AND weight <= 100" json:"weight"`
	ComplexityTier string    `gorm:"type:varchar(16);not null;default:MEDIUM;check:complexity_tier IN ('SIMPLE','MEDIUM','COMPLEX','REASONING')" json:"complexity_tier"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (GroupModel) TableName() string {
	return "group_models"
}
