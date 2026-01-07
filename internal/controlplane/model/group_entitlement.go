package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	EntitlementScopeStudio = "studio"
	EntitlementScopeDept   = "dept"
)

type GroupEntitlement struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	GroupID   uint       `gorm:"not null;uniqueIndex:idx_ent_unique;index:idx_ent_group_id" json:"group_id"`
	ScopeType string     `gorm:"type:varchar(16);not null;uniqueIndex:idx_ent_unique;index:idx_ent_scope" json:"scope_type"`
	ScopeID   string     `gorm:"type:varchar(64);not null;uniqueIndex:idx_ent_unique;index:idx_ent_scope" json:"scope_id"`
	Enabled   bool       `gorm:"not null;default:true" json:"enabled"`
	GrantedBy *uuid.UUID `gorm:"type:uuid" json:"granted_by,omitempty"`
	CreatedAt time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (GroupEntitlement) TableName() string { return "group_entitlements" }
