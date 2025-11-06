package model

import (
	"time"

	"github.com/google/uuid"
)

// Provider 渠道配置实体
type Provider struct {
	ID                   uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name                 string    `gorm:"type:varchar(100);not null;uniqueIndex:idx_providers_name_unique" json:"name"`
	DefinitionID         string    `gorm:"type:varchar(50)" json:"definition_id"`
	Type                 string    `gorm:"type:varchar(50);not null" json:"type"`
	Endpoint             string    `gorm:"type:varchar(500);not null" json:"endpoint"`
	EncryptedCredentials string    `gorm:"type:text;not null" json:"-"`
	Enabled              bool      `gorm:"not null;default:true" json:"enabled"`
	Weight               int       `gorm:"not null;default:1" json:"weight"`
	Priority             int       `gorm:"not null;default:0" json:"priority"`
	Status               int       `gorm:"not null;default:1" json:"status"`
	TestModel            string    `gorm:"type:varchar(200);default:''" json:"test_model"`
	ResponseTime         int       `gorm:"default:0" json:"response_time"`
	Balance              float64   `gorm:"type:decimal(12,4);default:0" json:"balance"`
	Remark               string    `gorm:"type:varchar(500);default:''" json:"remark"`
	MaxRetries           int       `gorm:"not null;default:3" json:"max_retries"`
	Timeout              int       `gorm:"not null;default:30" json:"timeout"`
	Tag                  string    `gorm:"type:varchar(100);default:''" json:"tag"`
	CreatedAt            time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`

	// 关联
	Models     []Model             `gorm:"foreignKey:ProviderID" json:"models,omitempty"`
	Definition *ProviderDefinition `gorm:"foreignKey:DefinitionID" json:"definition,omitempty"`
}

func (Provider) TableName() string {
	return "providers"
}
