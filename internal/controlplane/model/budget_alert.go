package model

import (
	"time"

	"github.com/google/uuid"
)

// 告警阈值档位
const (
	AlertThreshold80  = 80  // 80% 预警
	AlertThreshold100 = 100 // 100% 满额（程序服务额外标红）
)

// BudgetAlertRecord 告警去重表。
// 唯一键 (scope_type, scope_id, period_type, period, threshold) 保证同对象/同周期/同档位只告警一次（FR-4.3）。
// 改额（扩容）时删除对应行，使扩容后再次逼近阈值可重新告警。
type BudgetAlertRecord struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ScopeType  string    `gorm:"type:varchar(16);not null;index:idx_budget_alert,unique" json:"scope_type"`
	ScopeID    string    `gorm:"type:varchar(64);not null;index:idx_budget_alert,unique" json:"scope_id"`
	PeriodType string    `gorm:"type:varchar(8);not null;index:idx_budget_alert,unique" json:"period_type"` // monthly（本期）
	Period     string    `gorm:"type:varchar(10);not null;index:idx_budget_alert,unique" json:"period"`      // YYYY-MM
	Threshold  int       `gorm:"not null;index:idx_budget_alert,unique" json:"threshold"`                   // 80 | 100
	NotifiedAt time.Time `json:"notified_at"`
}

func (BudgetAlertRecord) TableName() string { return "budget_alert_records" }
