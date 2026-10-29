package model

import (
	"time"
)

// UsageDaily 按天聚合
type UsageDaily struct {
	// StatDate 除主键前缀外单独建索引：用量看板/百分位统计都按"stat_date 窗口 + 其他维度聚合"读，
	// 靠主键或 idx_usage_daily_dept 的列前缀只能碰巧可走。
	StatDate         time.Time `gorm:"type:date;primaryKey;index:idx_usage_daily_stat_date" json:"stat_date"`
	KeyID            string    `gorm:"type:text;primaryKey" json:"key_id"`
	UserID           string    `gorm:"type:text;not null;default:''" json:"user_id"`
	DeptID           string    `gorm:"type:text;primaryKey;not null;default:''" json:"dept_id"`
	StudioID         string    `gorm:"type:text;primaryKey;not null;default:''" json:"studio_id"`
	Model            string    `gorm:"type:text;primaryKey" json:"model"`
	ReqCount         int       `gorm:"not null;default:0" json:"req_count"`
	SuccessCount     int       `gorm:"not null;default:0" json:"success_count"`
	PromptTokens     int64     `gorm:"not null;default:0" json:"prompt_tokens"`
	CachedTokens     int64     `gorm:"not null;default:0" json:"cached_tokens"`
	CacheWriteTokens int64     `gorm:"not null;default:0" json:"cache_write_tokens"`
	CompletionTokens int64     `gorm:"not null;default:0" json:"completion_tokens"`
	ReasoningTokens  int64     `gorm:"not null;default:0" json:"reasoning_tokens"`
	EstCostUSD       float64   `gorm:"type:decimal(18,6);not null;default:0" json:"est_cost_usd"`
}

func (UsageDaily) TableName() string {
	return "usage_daily"
}
