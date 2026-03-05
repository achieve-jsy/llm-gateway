package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EhrPosition struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	OrgID      string    `gorm:"type:varchar(64);not null;index:idx_ehr_pos_org_id" json:"org_id"`
	PositionID string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_ehr_pos_org_pos" json:"position_id"`
	Name       string    `gorm:"type:varchar(255);not null" json:"name"`
	Type       int32     `gorm:"not null;default:0" json:"type"`
	NamePath   string    `gorm:"type:text;not null;default:''" json:"name_path"`
	ParentID   string    `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_pos_parent" json:"parent_id"`
	SyncedAt   time.Time `gorm:"not null;autoCreateTime" json:"synced_at"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (EhrPosition) TableName() string { return "ehr_positions" }

func (m *EhrPosition) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}
