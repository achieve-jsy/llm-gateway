package model

import (
	"time"
)

// CallLog 调用明细
type CallLog struct {
	RequestID           string    `gorm:"type:text;primaryKey"                                          json:"request_id"`
	TS                  time.Time `gorm:"not null;index:idx_call_logs_ts"                               json:"ts"`
	KeyID               string    `gorm:"type:text;index:idx_call_logs_key_ts,composite:key_ts"         json:"key_id"`
	KeyName             string    `gorm:"type:varchar(128)"                                             json:"key_name,omitempty"`
	KeyType             string    `gorm:"type:varchar(16);not null;default:personal;index:idx_call_logs_key_type" json:"key_type"`
	UserID              string    `gorm:"type:text"                                                     json:"user_id"`
	UserName            string    `gorm:"type:varchar(128)"                                             json:"user_name,omitempty"`
	DeptID              string    `gorm:"type:text;index:idx_call_logs_dept_ts,composite:dept_ts"       json:"dept_id"`
	DeptName            string    `gorm:"type:varchar(128)"                                             json:"dept_name,omitempty"`
	StudioID            string    `gorm:"type:text"                                                     json:"studio_id"`
	StudioName          string    `gorm:"type:varchar(128)"                                             json:"studio_name,omitempty"`
	Model               string    `gorm:"type:text;not null;index:idx_call_logs_model_ts,composite:model_ts" json:"model"`
	RoutedModel         string    `gorm:"type:text"                                                     json:"routed_model"`
	GroupName           string    `gorm:"type:text"                                                     json:"group_name"`
	Provider            string    `gorm:"type:text"                                                     json:"provider"`
	PromptTokens        int       `gorm:"not null;default:0"                                            json:"prompt_tokens"`
	CachedTokens        int       `gorm:"not null;default:0"                                            json:"cached_tokens"`
	CacheWriteTokens    int       `gorm:"not null;default:0"                                            json:"cache_write_tokens"`
	CompletionTokens    int       `gorm:"not null;default:0"                                            json:"completion_tokens"`
	ReasoningTokens     int       `gorm:"not null;default:0"                                            json:"reasoning_tokens"`
	TotalTokens         int       `gorm:"not null;default:0"                                            json:"total_tokens"`
	Status              int       `                                                                     json:"status"`
	UpstreamStatus      int       `                                                                     json:"upstream_status"`
	DurationMs          int       `                                                                     json:"duration_ms"`
	FirstTokenMs        *int      `                                                                     json:"first_token_ms,omitempty"`
	StageTTFBMs         *int      `gorm:"column:stage_ttfb_ms"                                          json:"stage_ttfb_ms,omitempty"`
	StageAuthMs         *int16    `gorm:"column:stage_auth_ms"                                          json:"stage_auth_ms,omitempty"`
	StageRouteMs        *int16    `gorm:"column:stage_route_ms"                                         json:"stage_route_ms,omitempty"`
	StageUpstreamMs     *int16    `gorm:"column:stage_upstream_ms"                                      json:"stage_upstream_ms,omitempty"`
	Stream              bool      `gorm:"not null;default:false"                                        json:"stream"`
	FailoverUsed        bool      `gorm:"not null;default:false"                                        json:"failover_used"`
	RoutedViaGroup      bool      `gorm:"not null;default:false"                                        json:"routed_via_group"`
	UsageMissing        bool      `gorm:"not null;default:false"                                        json:"usage_missing"`
	ReasoningEffort     string    `gorm:"type:varchar(16)"                                              json:"reasoning_effort,omitempty"`
	UserAgent           string    `gorm:"type:text"                                                     json:"user_agent,omitempty"`
	IPAddress           string    `gorm:"type:varchar(64)"                                              json:"ip_address,omitempty"`
	InputPriceSnapshot  *float64  `gorm:"type:decimal(14,6)"                                            json:"input_price_snapshot,omitempty"`
	OutputPriceSnapshot *float64  `gorm:"type:decimal(14,6)"                                            json:"output_price_snapshot,omitempty"`
	InputCostUSD        float64   `gorm:"type:decimal(18,8);not null;default:0"                         json:"input_cost_usd"`
	OutputCostUSD       float64   `gorm:"type:decimal(18,8);not null;default:0"                         json:"output_cost_usd"`
	CacheReadCostUSD    float64   `gorm:"type:decimal(18,8);not null;default:0"                         json:"cache_read_cost_usd"`
	EstCostUSD          float64   `gorm:"type:decimal(18,6);not null;default:0"                         json:"est_cost_usd"`
}

func (CallLog) TableName() string {
	return "call_logs"
}