package model

import (
	"time"

	"github.com/google/uuid"
)

// scope_type 枚举
const (
	BudgetScopeCompany     = "company"      // 全公司唯一，scope_id 固定为 "GLOBAL"
	BudgetScopeStudio      = "studio"       // 工作室，scope_id = studio UUID
	BudgetScopeDeptDaily   = "dept_daily"   // 部门成员日常预算（共享池）
	BudgetScopeDeptService = "dept_service" // 部门程序服务预算
	BudgetScopeMember      = "member"       // 成员单独设额（可选），scope_id = user UUID

	BudgetScopeGlobal = "GLOBAL" // company 层固定 scope_id
)

// BudgetAllocation 各层级预算额度配置（周期无关，每个周期共用）。
// 允许超配——子级之和可超过父级，不做校验。
type BudgetAllocation struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ScopeType string    `gorm:"type:varchar(16);not null;index:idx_budget_alloc_scope,unique" json:"scope_type"`
	ScopeID   string    `gorm:"type:varchar(64);not null;index:idx_budget_alloc_scope,unique" json:"scope_id"`
	AmountUSD float64   `gorm:"type:numeric(12,4);not null;default:0" json:"amount_usd"`
	Period    string    `gorm:"type:varchar(8);not null;default:'monthly'" json:"period"` // monthly（本期固定）
	Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedBy uuid.UUID `gorm:"type:uuid" json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (BudgetAllocation) TableName() string { return "budget_allocations" }

// BudgetConsumption 周期已用量持久副本（可重建）。
// 实时计数在 Redis，本表由每日对账任务从 call_logs 重算校准；
// Redis 丢失时作为兜底数据源回填。
type BudgetConsumption struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ScopeType string    `gorm:"type:varchar(16);not null;index:idx_budget_cons,unique" json:"scope_type"`
	ScopeID   string    `gorm:"type:varchar(64);not null;index:idx_budget_cons,unique" json:"scope_id"`
	Period    string    `gorm:"type:varchar(7);not null;index:idx_budget_cons,unique" json:"period"` // YYYY-MM
	UsedUSD   float64   `gorm:"type:numeric(14,6);not null;default:0" json:"used_usd"`
	SyncedAt  *time.Time `json:"synced_at,omitempty"` // 最近一次对账校准时间
	UpdatedAt time.Time  `json:"updated_at"`
}

func (BudgetConsumption) TableName() string { return "budget_consumption" }
