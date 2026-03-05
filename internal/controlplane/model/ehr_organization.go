package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EhrOrganization struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	OrgID          string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_ehr_org_org_id" json:"org_id"`
	OrgName        string    `gorm:"type:varchar(255);not null" json:"org_name"`
	OrgCode        string    `gorm:"type:varchar(64);not null;default:''" json:"org_code"`
	EhrCreateDate string    `gorm:"type:varchar(64);not null;default:''" json:"ehr_create_date"`
	EhrModifyDate  string    `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_org_modify_date" json:"ehr_modify_date"`
	SyncedAt       time.Time `gorm:"not null;autoCreateTime" json:"synced_at"`
	CreatedAt      time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (EhrOrganization) TableName() string { return "ehr_organizations" }

func (m *EhrOrganization) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}
