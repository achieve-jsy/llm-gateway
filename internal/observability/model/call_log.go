package model

import (
	"time"

	"github.com/google/uuid"
)

// CallLog 调用明细
type CallLog struct {
	RequestID                     string                         `gorm:"type:text;primaryKey"                                          json:"request_id"`
	TS                            time.Time                      `gorm:"not null;index:idx_call_logs_ts;index:idx_call_logs_budget_tier_ts,priority:2" json:"ts"`
	KeyID                         string                         `gorm:"type:text;index:idx_call_logs_key_ts,composite:key_ts"         json:"key_id"`
	KeyName                       string                         `gorm:"type:varchar(128)"                                             json:"key_name,omitempty"`
	KeyType                       string                         `gorm:"type:varchar(16);not null;default:personal;index:idx_call_logs_key_type" json:"key_type"`
	UserID                        string                         `gorm:"type:text"                                                     json:"user_id"`
	UserName                      string                         `gorm:"type:varchar(128)"                                             json:"user_name,omitempty"`
	DeptID                        string                         `gorm:"type:text;index:idx_call_logs_dept_ts,composite:dept_ts"       json:"dept_id"`
	DeptName                      string                         `gorm:"type:varchar(128)"                                             json:"dept_name,omitempty"`
	StudioID                      string                         `gorm:"type:text"                                                     json:"studio_id"`
	StudioName                    string                         `gorm:"type:varchar(128)"                                             json:"studio_name,omitempty"`
	Model                         string                         `gorm:"type:text;not null;index:idx_call_logs_model_ts,composite:model_ts" json:"model"`
	RoutedModel                   string                         `gorm:"type:text"                                                     json:"routed_model"`
	GroupName                     string                         `gorm:"type:text"                                                     json:"group_name"`
	Provider                      string                         `gorm:"type:text"                                                     json:"provider"`
	PromptTokens                  int                            `gorm:"not null;default:0"                                            json:"prompt_tokens"`
	CachedTokens                  int                            `gorm:"not null;default:0"                                            json:"cached_tokens"`
	CacheWriteTokens              int                            `gorm:"not null;default:0"                                            json:"cache_write_tokens"`
	CompletionTokens              int                            `gorm:"not null;default:0"                                            json:"completion_tokens"`
	ReasoningTokens               int                            `gorm:"not null;default:0"                                            json:"reasoning_tokens"`
	TotalTokens                   int                            `gorm:"not null;default:0"                                            json:"total_tokens"`
	Status                        int                            `                                                                     json:"status"`
	UpstreamStatus                int                            `                                                                     json:"upstream_status"`
	DurationMs                    int                            `                                                                     json:"duration_ms"`
	FirstTokenMs                  *int                           `                                                                     json:"first_token_ms,omitempty"`
	StageTTFBMs                   *int                           `gorm:"column:stage_ttfb_ms"                                          json:"stage_ttfb_ms,omitempty"`
	StageAuthMs                   *int16                         `gorm:"column:stage_auth_ms"                                          json:"stage_auth_ms,omitempty"`
	StageRouteMs                  *int16                         `gorm:"column:stage_route_ms"                                         json:"stage_route_ms,omitempty"`
	StageUpstreamMs               *int16                         `gorm:"column:stage_upstream_ms"                                      json:"stage_upstream_ms,omitempty"`
	Stream                        bool                           `gorm:"not null;default:false"                                        json:"stream"`
	FailoverUsed                  bool                           `gorm:"not null;default:false"                                        json:"failover_used"`
	RoutedViaGroup                bool                           `gorm:"not null;default:false"                                        json:"routed_via_group"`
	UsageMissing                  bool                           `gorm:"not null;default:false"                                        json:"usage_missing"`
	ReasoningEffort               string                         `gorm:"type:varchar(16)"                                              json:"reasoning_effort,omitempty"`
	UserAgent                     string                         `gorm:"type:text"                                                     json:"user_agent,omitempty"`
	IPAddress                     string                         `gorm:"type:varchar(64)"                                              json:"ip_address,omitempty"`
	InputPriceSnapshot            *float64                       `gorm:"type:decimal(14,6)"                                            json:"input_price_snapshot,omitempty"`
	OutputPriceSnapshot           *float64                       `gorm:"type:decimal(14,6)"                                            json:"output_price_snapshot,omitempty"`
	InputCostUSD                  float64                        `gorm:"type:decimal(18,8);not null;default:0"                         json:"input_cost_usd"`
	OutputCostUSD                 float64                        `gorm:"type:decimal(18,8);not null;default:0"                         json:"output_cost_usd"`
	CacheReadCostUSD              float64                        `gorm:"type:decimal(18,8);not null;default:0"                         json:"cache_read_cost_usd"`
	EstCostUSD                    float64                        `gorm:"type:decimal(18,6);not null;default:0"                         json:"est_cost_usd"`
	InboundEndpoint               string                         `gorm:"type:varchar(128)"                                             json:"inbound_endpoint"`
	BudgetTierID                  *uuid.UUID                     `gorm:"type:uuid;index:idx_call_logs_budget_tier_ts,priority:1" json:"budget_tier_id,omitempty"`
	BudgetTierNameSnapshot        string                         `gorm:"type:varchar(64)" json:"budget_tier_name_snapshot,omitempty"`
	BudgetTierAccountingEligible  bool                           `gorm:"not null;default:false" json:"budget_tier_accounting_eligible"`
	BudgetTierConstraintsSnapshot []BudgetTierConstraintSnapshot `gorm:"type:jsonb;not null;default:'[]';serializer:json" json:"budget_tier_constraints_snapshot"`
	BudgetBlockCode               *string                        `gorm:"type:varchar(64);index" json:"budget_block_code,omitempty"`
	BudgetBlockDimension          *string                        `gorm:"type:varchar(8)" json:"budget_block_dimension,omitempty"`
	BudgetBlockScopeType          *string                        `gorm:"type:varchar(32)" json:"budget_block_scope_type,omitempty"`
	BudgetBlockScopeID            *string                        `gorm:"type:varchar(128)" json:"budget_block_scope_id,omitempty"`
	BudgetBlockPeriodType         *string                        `gorm:"type:varchar(8)" json:"budget_block_period_type,omitempty"`
}

// BudgetTierConstraintSnapshot freezes the exact v2 tier counter selected for
// a request. Rebuilds consume this fact instead of inferring historical
// eligibility from mutable allocations or tier-limit configuration.
type BudgetTierConstraintSnapshot struct {
	RedisKey   string    `json:"redis_key"`
	ScopeType  string    `json:"scope_type"`
	ScopeID    string    `json:"scope_id"`
	TierID     string    `json:"tier_id"`
	PeriodType string    `json:"period_type"`
	Period     string    `json:"period"`
	ResetAt    time.Time `json:"reset_at"`
}

func (CallLog) TableName() string {
	return "call_logs"
}

const (
	KeyTypePersonal = "personal"
	KeyTypeService  = "service"
)

// WhereExcludeServiceKeyLogs 用于用户维度聚合 SQL，排除程序 Service Key 调用。
const WhereExcludeServiceKeyLogs = `COALESCE(NULLIF(key_type, ''), 'personal') != 'service'`

// WhereExcludeServiceKeyLogsCL 带 call_logs 表别名 cl。
const WhereExcludeServiceKeyLogsCL = `COALESCE(NULLIF(cl.key_type, ''), 'personal') != 'service'`
