package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EhrCompany struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	OrgID         string    `gorm:"type:varchar(64);not null;index:idx_ehr_company_org_id" json:"org_id"`
	CompanyID     string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_ehr_company_org_company" json:"company_id"`
	CompanyCode   string    `gorm:"type:varchar(64);not null;default:''" json:"company_code"`
	CompanyName   string    `gorm:"type:varchar(255);not null" json:"company_name"`
	ParentID      string    `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_company_parent" json:"parent_id"`
	EhrModifyDate string    `gorm:"type:varchar(64);not null;default:'';index:idx_ehr_company_modify_date" json:"ehr_modify_date"`
	SyncedAt      time.Time `gorm:"not null;autoCreateTime" json:"synced_at"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (EhrCompany) TableName() string { return "ehr_companies" }

func (m *EhrCompany) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}
