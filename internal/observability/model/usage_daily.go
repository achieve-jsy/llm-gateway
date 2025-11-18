package model

import (
	"time"
)

// UsageDaily 按天聚合
type UsageDaily struct {
	StatDate         time.Time `gorm:"type:date;primaryKey" json:"stat_date"`
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