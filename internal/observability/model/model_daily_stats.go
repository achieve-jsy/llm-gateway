package model

import "time"

// ModelDailyStats 模型维度每日预聚合统计
// PK: (stat_date, model, studio_id)
type ModelDailyStats struct {
	StatDate         time.Time `json:"stat_date" gorm:"type:date;primaryKey"`
	Model            string    `json:"model" gorm:"type:text;primaryKey"`
	StudioID         string    `json:"studio_id" gorm:"type:text;primaryKey;default:''"`
	ReqCount         int       `json:"req_count" gorm:"not null;default:0"`
	SuccessCount     int       `json:"success_count" gorm:"not null;default:0"`
	UserCount        int       `json:"user_count" gorm:"not null;default:0"`
	DeptCount        int       `json:"dept_count" gorm:"not null;default:0"`
	ActiveDays       int       `json:"active_days" gorm:"not null;default:0"`
	PromptTokens     int64     `json:"prompt_tokens" gorm:"not null;default:0"`
	CachedTokens     int64     `json:"cached_tokens" gorm:"not null;default:0"`
	CacheWriteTokens int64     `json:"cache_write_tokens" gorm:"not null;default:0"`
	CompletionTokens int64     `json:"completion_tokens" gorm:"not null;default:0"`
	ReasoningTokens  int64     `json:"reasoning_tokens" gorm:"not null;default:0"`
	EstCostUSD       float64   `json:"est_cost_usd" gorm:"type:decimal(18,6);not null;default:0"`
	AvgDurationMs    int       `json:"avg_duration_ms" gorm:"not null;default:0"`
	P50DurationMs    int       `json:"p50_duration_ms" gorm:"not null;default:0"`
	P90DurationMs    int       `json:"p90_duration_ms" gorm:"not null;default:0"`
	AvgFirstTokenMs  int       `json:"avg_first_token_ms" gorm:"not null;default:0"`
}

func (ModelDailyStats) TableName() string {
	return "model_daily_stats"
}
