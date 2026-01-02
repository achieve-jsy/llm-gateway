package model

import (
	"time"

	"github.com/google/uuid"
)

// 授权范围类型
const (
	EntitlementScopeStudio = "studio" // 平台授权给工作室
	EntitlementScopeDept   = "dept"   // 工作室/负责人授权给部门
)

// GroupEntitlement 模型组授权表（两级：studio + dept），统一一张表靠 scope_type 区分。
// 受控组可见/可用的判定：dept 行 enabled=true 且其对应 studio 行也 enabled=true（级联）。
type GroupEntitlement struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	GroupID   uint      `gorm:"not null;index:idx_group_ent,unique" json:"group_id"`
	ScopeType string    `gorm:"type:varchar(16);not null;index:idx_group_ent,unique" json:"scope_type"` // studio | dept
	ScopeID   string    `gorm:"type:varchar(64);not null;index:idx_group_ent,unique" json:"scope_id"`   // studio UUID | SSO dept_id
	Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
	GrantedBy uuid.UUID `gorm:"type:uuid" json:"granted_by"` // 操作人 user_id
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (GroupEntitlement) TableName() string { return "group_entitlements" }
