package model

import (
	"time"

	"github.com/google/uuid"
)

// KeyTypePersonal 个人 Key 类型
const KeyTypePersonal = "personal"

// KeyTypeService 程序服务 Key 类型
const KeyTypeService = "service"

type ApiKey struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID       uuid.UUID `gorm:"type:uuid;not null;index:idx_api_keys_user_id" json:"user_id"`
	Name         string    `gorm:"type:varchar(128);not null" json:"name"`
	KeyHash      string    `gorm:"type:varchar(128);not null;uniqueIndex:uniq_key_hash" json:"-"`
	EncryptedKey string    `gorm:"type:text;not null" json:"-"`
	KeyPrefix    string    `gorm:"type:varchar(8);not null;default:sk-" json:"key_prefix"`
	KeyHint      string    `gorm:"type:varchar(8);not null" json:"key_hint"`
	Enabled         bool       `gorm:"not null;default:true;index:idx_api_keys_enabled" json:"enabled"`
	LogEnabled      bool       `gorm:"not null;default:true" json:"log_enabled"`
	AccessAllGroups bool       `gorm:"not null;default:false" json:"access_all_groups"`
	// BypassOrgCheck 豁免组织归属检查（供系统探针、平台内部 Key 使用，默认 false）
	BypassOrgCheck  bool       `gorm:"not null;default:false" json:"bypass_org_check"`
	UserName        string     `gorm:"type:varchar(128)" json:"user_name,omitempty"`
	DeptID          string     `gorm:"type:text;default:''" json:"dept_id,omitempty"`
	DeptName        string     `gorm:"type:varchar(128)" json:"dept_name,omitempty"`
	StudioID        string     `gorm:"type:varchar(128)" json:"studio_id,omitempty"`
	StudioName      string     `gorm:"type:varchar(128)" json:"studio_name,omitempty"`
	// v0.7: Service Key 扩展字段
	KeyType   string     `gorm:"type:varchar(16);not null;default:'personal';index:idx_api_keys_type" json:"key_type"`
	Purpose   string     `gorm:"type:varchar(128);not null;default:''" json:"purpose,omitempty"`
	CreatedBy *uuid.UUID `gorm:"type:uuid" json:"created_by,omitempty"`
	// v0.8: IP 白名单（JSON 数组字符串，如 `["1.2.3.4","10.0.0.0/8"]`；空字符串或 "[]" = 不限制）
	IPAllowlist string `gorm:"type:text;not null;default:''" json:"ip_allowlist,omitempty"`
	// 四层配额体系（Phase 1）
	// Service Key 主体配额（key_type=service）；NULL = 继承 default_service_key
	QuotaRPM            *int `gorm:"column:quota_rpm" json:"quota_rpm,omitempty"`
	QuotaMaxConcurrency *int `gorm:"column:quota_max_concurrency" json:"quota_max_concurrency,omitempty"`
	CreatedAt       time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
	LastUsedAt      *time.Time `gorm:"type:timestamp with time zone" json:"last_used_at,omitempty"`
	ExpiresAt       *time.Time `gorm:"type:timestamp with time zone" json:"expires_at,omitempty"`
}

func (ApiKey) TableName() string { return "api_keys" }
