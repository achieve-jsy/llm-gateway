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
	ID                      uint           `gorm:"primaryKey" json:"id"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
	DeletedAt               gorm.DeletedAt `gorm:"index" json:"-"`
	Name                    string         `gorm:"type:varchar(64);uniqueIndex:uniq_model_groups_name,where:deleted_at IS NULL;not null" json:"name"`
	Description             string         `gorm:"type:text" json:"description"`
	Strategy                string         `gorm:"type:varchar(32);not null;default:round_robin;check:strategy IN ('round_robin','priority','weighted')" json:"strategy"`
	Enabled                 bool           `gorm:"not null;default:true;index:idx_model_groups_enabled,where:enabled = TRUE AND deleted_at IS NULL" json:"enabled"`
	MaxSwitches             int            `gorm:"not null;default:2" json:"max_switches"`
	SameModelRetries        int            `gorm:"not null;default:1" json:"same_model_retries"`
	FailoverCooldownSeconds int            `gorm:"not null;default:60" json:"failover_cooldown_seconds"`
	TimeoutSeconds          int            `gorm:"not null;default:0" json:"timeout_seconds"`
	OpenMode                string         `gorm:"type:varchar(16);not null;default:'public';check:open_mode IN ('public','controlled')" json:"open_mode"`
	Models                  []GroupModel   `gorm:"foreignKey:GroupID" json:"models,omitempty"`
}

func (Group) TableName() string {
	return "model_groups"
}

type GroupModel struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	GroupID   uint      `gorm:"not null;uniqueIndex:uniq_group_model;index" json:"group_id"`
	ModelID   uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uniq_group_model;index:idx_group_models_model_id" json:"model_id"`
	ModelName string    `gorm:"->" json:"model_name"`
	Priority  *int      `gorm:"default:null" json:"priority"`
	Weight    *int      `gorm:"default:null;check:weight >= 1 AND weight <= 100" json:"weight"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (GroupModel) TableName() string {
	return "group_models"
}
