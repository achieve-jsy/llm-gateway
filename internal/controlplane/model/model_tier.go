package model

import (
	"time"

	"github.com/google/uuid"
)

type ModelTier struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name           string     `gorm:"type:varchar(64);not null;uniqueIndex:uniq_model_tiers_active_name,expression:lower(name),where:archived_at IS NULL" json:"name"`
	Description    string     `gorm:"type:text;not null;default:''" json:"description"`
	Color          string     `gorm:"type:varchar(7);not null;default:'#64748B'" json:"color"`
	SortOrder      int        `gorm:"not null;default:0;index:idx_model_tiers_sort" json:"sort_order"`
	Enabled        bool       `gorm:"not null;default:true" json:"enabled"`
	ArchivedAt     *time.Time `gorm:"index:idx_model_tiers_archived_at" json:"archived_at,omitempty"`
	CreatedAt      time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
	GroupCount     int64      `gorm:"-" json:"group_count"`
	BudgetCount    int64      `gorm:"-" json:"budget_count"`
	HistoryCount   int64      `gorm:"-" json:"history_count"`
	ReferenceCount int64      `gorm:"-" json:"reference_count"`
}

func (ModelTier) TableName() string {
	return "model_tiers"
}
