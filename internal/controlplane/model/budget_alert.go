package model

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/google/uuid"
)

// 告警阈值档位
const (
	AlertThreshold80  = 80  // 80% 预警
	AlertThreshold100 = 100 // 100% 满额（程序服务额外标红）
)

// BudgetAlertRecord 告警去重表。
// P0 Expand 阶段保留旧 idx_budget_alert，并新增含 dimension_key 的维度唯一索引。
// 旧索引仅在 P3 启用前的 Contract migration 中删除，避免 Expand 阶段破坏旧版本写入契约。
// 改额（扩容）时删除对应行，使扩容后再次逼近阈值可重新告警。
type BudgetAlertRecord struct {
	ID            uuid.UUID                  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ScopeType     string                     `gorm:"type:varchar(32);not null;uniqueIndex:idx_budget_alert,priority:1;uniqueIndex:uniq_budget_alert_dimension,priority:1" json:"scope_type"`
	ScopeID       string                     `gorm:"type:varchar(255);not null;uniqueIndex:idx_budget_alert,priority:2;uniqueIndex:uniq_budget_alert_dimension,priority:2" json:"scope_id"`
	PeriodType    string                     `gorm:"type:varchar(16);not null;uniqueIndex:idx_budget_alert,priority:3;uniqueIndex:uniq_budget_alert_dimension,priority:3" json:"period_type"`
	Period        string                     `gorm:"type:varchar(16);not null;uniqueIndex:idx_budget_alert,priority:4;uniqueIndex:uniq_budget_alert_dimension,priority:4" json:"period"`
	Threshold     int                        `gorm:"not null;uniqueIndex:idx_budget_alert,priority:5;uniqueIndex:uniq_budget_alert_dimension,priority:5" json:"threshold"`
	DimensionKey  string                     `gorm:"type:varchar(48);not null;default:'total';uniqueIndex:uniq_budget_alert_dimension,priority:6" json:"dimension_key"`
	DimensionType budgetdomain.DimensionType `gorm:"type:varchar(8);not null;default:'total';index" json:"dimension_type"`
	ModelTierID   *uuid.UUID                 `gorm:"type:uuid;index;check:chk_budget_alert_dimension,(dimension_type = 'total' AND model_tier_id IS NULL) OR (dimension_type = 'tier' AND model_tier_id IS NOT NULL)" json:"model_tier_id,omitempty"`
	ModelTier     *ModelTier                 `gorm:"foreignKey:ModelTierID;constraint:OnDelete:RESTRICT" json:"-"`
	NotifiedAt    time.Time                  `json:"notified_at"`
}

func (BudgetAlertRecord) TableName() string { return "budget_alert_records" }
