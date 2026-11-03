package model

import (
	"time"

	"github.com/google/uuid"
)

// ModelCatalog 模型 Catalog 条目（来源例如 LiteLLM 等元数据源）。
//
// 设计要点：
//   - 唯一键 (source, source_model_id)：source_model_id 可能含 '/' 例如 azure_ai/deepseek-v3.2，
//     因此**所有 API 不要把 source_model_id 放路径参数**，请使用 query 形式。
//   - token 价格字段以 "per million tokens" 为单位（与 models.input_price/output_price 同语义），
//     一键填充时直接复制即可，**不要再次做单位换算**。
//   - 媒体价格字段（per_image / input_per_image / per_second_input / per_second_output /
//     per_request）以「每张 / 每秒 / 每次」的美元原值为单位，**不折算成 token 价**。
//   - tier_threshold/input_price_above_tier/output_price_above_tier 表达阶梯定价（如 Claude
//     200K 阈值上的不同单价）；若上游条目无阶梯字段，则 tier_threshold 为 nil。
//   - RawPayload 保留原始 JSON entry，预留以后扩字段（避免每次新字段都要改 schema）。
//
// GORM tag 配置注意：
//   - id 使用 PG 端 gen_random_uuid()，与现有 Provider/Model 一致；
//   - SQLite 测试库不识别 gen_random_uuid()，因此在 repo Upsert/Create 时仍由 Go 侧
//     主动生成 ID，避免依赖 PG 端 default。
type ModelCatalog struct {
	ID                              uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Source                          string     `gorm:"type:varchar(50);not null;uniqueIndex:idx_catalog_source_id" json:"source"`
	SourceModelID                   string     `gorm:"type:varchar(200);not null;uniqueIndex:idx_catalog_source_id" json:"source_model_id"`
	DisplayName                     string     `gorm:"type:varchar(200)" json:"display_name"`
	Provider                        string     `gorm:"type:varchar(50);index:idx_catalog_provider" json:"provider"`
	Mode                            string     `gorm:"type:varchar(30)" json:"mode"`
	ContextLength                   int64      `gorm:"default:0" json:"context_length"`
	MaxOutputTokens                 int64      `gorm:"default:0" json:"max_output_tokens"`
	InputPricePerMillion            float64    `gorm:"type:decimal(14,6);default:0" json:"input_price_per_million"`
	OutputPricePerMillion           float64    `gorm:"type:decimal(14,6);default:0" json:"output_price_per_million"`
	CacheReadPrice                  *float64   `gorm:"type:decimal(14,6)" json:"cache_read_price,omitempty"`
	CacheWritePrice                 *float64   `gorm:"type:decimal(14,6)" json:"cache_write_price,omitempty"`
	TierThreshold                   *int64     `json:"tier_threshold,omitempty"`
	InputPriceAboveTier             *float64   `gorm:"type:decimal(14,6)" json:"input_price_above_tier,omitempty"`
	OutputPriceAboveTier            *float64   `gorm:"type:decimal(14,6)" json:"output_price_above_tier,omitempty"`
	// 媒体价（对应 core.ModelPricing 既有的 per_image / input_per_image /
	// per_second_input / per_second_output / per_request 字段族）。
	// 单位是「每张 / 每秒 / 每次」的美元原值，**不经过 tokenPriceScale 缩放**；
	// 可空：NULL = 无价（区别于 token 价列的 0 占位），供计价层判定 usage_missing。
	PerImage                        *float64   `gorm:"type:decimal(18,8)" json:"per_image,omitempty"`
	InputPerImage                   *float64   `gorm:"type:decimal(18,8)" json:"input_per_image,omitempty"`
	PerSecondInput                  *float64   `gorm:"type:decimal(18,8)" json:"per_second_input,omitempty"`
	PerSecondOutput                 *float64   `gorm:"type:decimal(18,8)" json:"per_second_output,omitempty"`
	PerRequest                      *float64   `gorm:"type:decimal(18,8)" json:"per_request,omitempty"`
	SupportsFunctionCalling         bool       `gorm:"default:false" json:"supports_function_calling"`
	SupportsVision                  bool       `gorm:"default:false" json:"supports_vision"`
	SupportsPromptCaching           bool       `gorm:"default:false" json:"supports_prompt_caching"`
	SupportsResponseSchema          bool       `gorm:"default:false" json:"supports_response_schema"`
	SupportsToolChoice              bool       `gorm:"default:false" json:"supports_tool_choice"`
	SupportsParallelFunctionCalling bool       `gorm:"default:false" json:"supports_parallel_function_calling"`
	DeprecationDate                 *time.Time `json:"deprecation_date,omitempty"`
	RawPayload                      string     `gorm:"type:text" json:"raw_payload,omitempty"`
	SyncedAt                        time.Time  `gorm:"not null" json:"synced_at"`
	CreatedAt                       time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt                       time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

// TableName 显式指定表名，避免 gorm 复数推断歧义。
func (ModelCatalog) TableName() string { return "model_catalog" }
