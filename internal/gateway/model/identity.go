package model

import "time"

// UserIdentity represents the authenticated user identity
type UserIdentity struct {
	UserID          string `json:"user_id"`
	KeyID           string `json:"key_id"`
	DepartmentID    string `json:"department_id"`
	StudioID        string `json:"studio_id,omitempty"`
	AccessAllGroups bool   `json:"access_all_groups"`
	// BypassOrgCheck 豁免组织归属检查（来自 api_keys.bypass_org_check，供系统探针/内部 Key 使用）
	BypassOrgCheck bool `json:"bypass_org_check"`
	LogEnabled     bool `json:"log_enabled"`
	// v0.7: 可用模型组名称集合（public ∪ 本部门已开通 controlled），由 verify-key 快照下发。
	UsableGroups []string `json:"usable_groups,omitempty"`
	// v0.7: Key 类型（personal/service）
	KeyType string `json:"key_type,omitempty"`
	// v0.7: 预算链（scope 列表 + 各层额度），由 verify-key 快照下发。
	BudgetChain []BudgetScope `json:"budget_chain,omitempty"`
}

// BudgetScope 预算链中单层 scope 的标识与额度（与控制面 service.BudgetScope 镜像）。
type BudgetScope struct {
	ScopeType       string      `json:"scope_type"`
	ScopeID         string      `json:"scope_id"`
	AmountUSD       float64     `json:"amount_usd"`        // 月限额，-1=不限
	DailyAmountUSD  float64     `json:"daily_amount_usd"`  // 日限额，-1=不限
	WeeklyAmountUSD float64     `json:"weekly_amount_usd"` // 周限额，-1=不限
	Label           string      `json:"label"`
	TierLimits      []TierLimit `json:"tier_limits,omitempty"`
}

type TierLimit struct {
	TierID            string     `json:"tier_id"`
	TierName          string     `json:"tier_name"`
	PeriodType        string     `json:"period_type"`
	BaseLimitUSD      string     `json:"base_limit_usd"`
	EffectiveLimitUSD string     `json:"effective_limit_usd"`
	Enabled           bool       `json:"enabled"`
	EffectiveAt       time.Time  `json:"effective_at"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
}

// MeModelItem represents a single model in /v1/me/models response
type MeModelItem struct {
	ID                  string `json:"id"`
	Object              string `json:"object"`
	OwnedBy             string `json:"owned_by"`
	Type                string `json:"type"`
	Enabled             bool   `json:"enabled"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	LastCheckError      string `json:"last_check_error,omitempty"`
	ResponseTime        int    `json:"response_time"`
}

// MeModelsResponse is the response for /v1/me/models endpoint
type MeModelsResponse struct {
	Object string        `json:"object"`
	Data   []MeModelItem `json:"data"`
}
