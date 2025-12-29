package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

// BudgetAlertRepo 处理告警去重表。
type BudgetAlertRepo struct {
	db *gorm.DB
}

func NewBudgetAlertRepo(db *gorm.DB) *BudgetAlertRepo {
	return &BudgetAlertRepo{db: db}
}

// ExistsForPeriod 判断某对象/周期/档位是否已发过告警（去重判断）。
func (r *BudgetAlertRepo) ExistsForPeriod(scopeType, scopeID, period string, threshold int) bool {
	var count int64
	r.db.Model(&model.BudgetAlertRecord{}).
		Where("scope_type = ? AND scope_id = ? AND period = ? AND threshold = ?",
			scopeType, scopeID, period, threshold).
		Count(&count)
	return count > 0
}

// Create 写入告警去重记录。
func (r *BudgetAlertRepo) Create(a *model.BudgetAlertRecord) error {
	return r.db.Create(a).Error
}

// DeleteByScope 改额时清理该 period 的去重记录（使扩容后能再次告警）。
func (r *BudgetAlertRepo) DeleteByScope(scopeType, scopeID, period string) error {
	return r.db.Where("scope_type = ? AND scope_id = ? AND period = ?", scopeType, scopeID, period).
		Delete(&model.BudgetAlertRecord{}).Error
}
