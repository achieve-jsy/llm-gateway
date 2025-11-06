package model

import (
	"time"

	"github.com/google/uuid"
)

// Model 模型注册实体
type Model struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name            string    `gorm:"type:varchar(200);not null;uniqueIndex:idx_models_name_unique" json:"name"`
	ProviderID      uuid.UUID `gorm:"type:uuid;not null;index:idx_models_provider_id" json:"provider_id"`
	ModelType       string    `gorm:"type:varchar(50);not null;default:chat" json:"model_type"`
	Enabled         *bool     `gorm:"not null;default:true;index:idx_models_enabled" json:"enabled"`
	UpstreamModelID string    `gorm:"type:varchar(200);default:''" json:"upstream_model_id"`
	Description     string    `gorm:"type:varchar(500);default:''" json:"description"`
	ContextLength   int       `gorm:"default:0" json:"context_length"`
	MaxOutputTokens int       `gorm:"default:0" json:"max_output_tokens"`
	InputPrice      float64   `gorm:"type:decimal(10,6);default:0" json:"input_price"`
	OutputPrice     float64   `gorm:"type:decimal(10,6);default:0" json:"output_price"`
	Weight          int       `gorm:"not null;default:1" json:"weight"`
	CreatedAt       time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`

	// 关联
	Provider Provider `gorm:"foreignKey:ProviderID" json:"provider,omitempty"`
}

func (Model) TableName() string {
	return "models"
}
