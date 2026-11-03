package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Model 模型注册实体
//
// catalog 联动字段：
//   - CatalogID：可选关联到 model_catalog 表的具体条目；
//   - CtxOverridden / MaxOutputOverridden：admin 手工改过 ContextLength /
//     MaxOutputTokens 后置 true，自动同步任务遇到 *_overridden=true 时跳过；
//   - InputPrice / OutputPrice / UpstreamModelID 永远不参与自动同步，由 admin
//     主动 "apply price diff" 触发覆盖。
//
// 价格语义：InputPrice / OutputPrice 单位统一为 USD per 1,000,000 tokens，
// 与 model_catalog.InputPricePerMillion / OutputPricePerMillion 同语义；
// 一键填充直接复制，禁止再次乘除。
type Model struct {
	ID                  uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name                string     `gorm:"type:varchar(200);not null;uniqueIndex:idx_models_name_provider_unique,composite:name_provider" json:"name"`
	ProviderID          uuid.UUID  `gorm:"type:uuid;not null;index:idx_models_provider_id;uniqueIndex:idx_models_name_provider_unique,composite:name_provider" json:"provider_id"`
	ModelType           string     `gorm:"type:varchar(50);not null;default:chat" json:"model_type"`
	Enabled             *bool      `gorm:"not null;default:true;index:idx_models_enabled" json:"enabled"`
	UpstreamModelID     string     `gorm:"type:varchar(200);default:''" json:"upstream_model_id"`
	Description         string     `gorm:"type:varchar(500);default:''" json:"description"`
	ContextLength       int        `gorm:"default:0" json:"context_length"`
	MaxOutputTokens     int        `gorm:"default:0" json:"max_output_tokens"`
	InputPrice          float64    `gorm:"type:decimal(14,6);default:0" json:"input_price"`
	OutputPrice         float64    `gorm:"type:decimal(14,6);default:0" json:"output_price"`
	PriceMode           string     `gorm:"type:varchar(20);not null;default:catalog" json:"price_mode"`
	ReasoningPrice      *float64   `gorm:"type:decimal(14,6)" json:"reasoning_price,omitempty"`
	CachedPrice         *float64   `gorm:"type:decimal(14,6)" json:"cached_price,omitempty"`
	Weight              int        `gorm:"not null;default:1" json:"weight"`
	CatalogID           *uuid.UUID `gorm:"type:uuid;index:idx_models_catalog_id" json:"catalog_id,omitempty"`
	CtxOverridden       bool       `gorm:"not null;default:false" json:"ctx_overridden"`
	MaxOutputOverridden bool       `gorm:"not null;default:false" json:"max_output_overridden"`
	ConsecutiveFailures int        `gorm:"not null;default:0" json:"consecutive_failures"`
	LastCheckError      string     `gorm:"type:varchar(500);default:''" json:"last_check_error"`
	ResponseTime        int        `gorm:"default:0" json:"response_time"`
	CreatedAt           time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`

	// 关联（默认查询不预加载，列表 API 需要时显式 Preload）
	Provider Provider      `gorm:"foreignKey:ProviderID" json:"provider,omitempty"`
	Catalog  *ModelCatalog `gorm:"foreignKey:CatalogID;references:ID" json:"catalog,omitempty"`
}

func (Model) TableName() string {
	return "models"
}

func (m *Model) Validate() error {
	if m.PriceMode != PriceModeCatalog && m.PriceMode != PriceModeManual {
		return fmt.Errorf("price_mode must be 'catalog' or 'manual'")
	}
	if m.PriceMode == PriceModeManual {
		if m.InputPrice <= 0 {
			return fmt.Errorf("manual mode requires input_price > 0")
		}
		if m.OutputPrice <= 0 {
			return fmt.Errorf("manual mode requires output_price > 0")
		}
	}
	return nil
}
