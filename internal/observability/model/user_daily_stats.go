package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// HourlyDistribution 小时分布 JSONB（key="0"~"23"，value=调用次数）
type HourlyDistribution map[string]int

// Value implements driver.Valuer for JSONB serialization
func (h HourlyDistribution) Value() (driver.Value, error) {
	if h == nil {
		return "{}", nil
	}
	b, err := json.Marshal(h)
	return string(b), err
}

// Scan implements sql.Scanner for JSONB deserialization
func (h *HourlyDistribution) Scan(src interface{}) error {
	if src == nil {
		*h = make(HourlyDistribution)
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case string:
		b = []byte(v)
	case []byte:
		b = v
	default:
		*h = make(HourlyDistribution)
		return nil
	}
	return json.Unmarshal(b, h)
}

// UserDailyStats 用户维度每日预聚合统计
// PK: (stat_date, user_id) — 一个用户只属于一个部门，studio/dept 通过 JOIN 获取
type UserDailyStats struct {
	StatDate           time.Time          `json:"stat_date" gorm:"type:date;primaryKey"`
	UserID             string             `json:"user_id" gorm:"type:text;primaryKey"`
	StudioID           string             `json:"studio_id" gorm:"type:text;not null;default:''"`
	DeptID             string             `json:"dept_id" gorm:"type:text;not null;default:''"`
	ReqCount           int                `json:"req_count" gorm:"not null;default:0"`
	SuccessCount       int                `json:"success_count" gorm:"not null;default:0"`
	ErrorCount         int                `json:"error_count" gorm:"not null;default:0"`
	ModelCount         int                `json:"model_count" gorm:"not null;default:0"`
	PromptTokens       int64              `json:"prompt_tokens" gorm:"not null;default:0"`
	CachedTokens       int64              `json:"cached_tokens" gorm:"not null;default:0"`
	CompletionTokens   int64              `json:"completion_tokens" gorm:"not null;default:0"`
	ReasoningTokens    int64              `json:"reasoning_tokens" gorm:"not null;default:0"`
	EstCostUSD         float64            `json:"est_cost_usd" gorm:"type:decimal(18,6);not null;default:0"`
	HourlyDistribution HourlyDistribution `json:"hourly_distribution" gorm:"type:jsonb;not null;default:'{}'"`
}

func (UserDailyStats) TableName() string {
	return "user_daily_stats"
}
