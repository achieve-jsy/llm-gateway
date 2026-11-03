package repository

import (
	"context"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AlertDimension struct {
	ScopeType     string
	ScopeID       string
	PeriodType    budgetdomain.PeriodType
	Period        string
	Threshold     int
	DimensionType budgetdomain.DimensionType
	ModelTierID   *uuid.UUID
}

// BudgetAlertRepo 处理告警去重表。
type BudgetAlertRepo struct {
	db *gorm.DB
}

// AlertOwnerRow 告警负责人（user_id + 邮箱，用于站内信定向投递）。
type AlertOwnerRow struct {
	UserID string
	Email  string
}

func NewBudgetAlertRepo(db *gorm.DB) *BudgetAlertRepo {
	return &BudgetAlertRepo{db: db}
}

// ExistsForPeriod 判断某对象/周期/档位是否已发过告警（去重判断）。
func (r *BudgetAlertRepo) ExistsForPeriod(ctx context.Context, d AlertDimension) (bool, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&model.BudgetAlertRecord{}).
		Where("scope_type = ? AND scope_id = ? AND period_type = ? AND period = ? AND threshold = ? AND dimension_type = ?",
			d.ScopeType, d.ScopeID, d.PeriodType, d.Period, d.Threshold, d.DimensionType)
	if d.ModelTierID == nil {
		query = query.Where("model_tier_id IS NULL")
	} else {
		query = query.Where("model_tier_id = ?", *d.ModelTierID)
	}
	err := query.Count(&count).Error
	return count > 0, err
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

func (r *BudgetAlertRepo) DeleteByDimension(ctx context.Context, d AlertDimension) error {
	query := r.db.WithContext(ctx).Where(
		"scope_type = ? AND scope_id = ? AND period_type = ? AND period = ? AND dimension_type = ?",
		d.ScopeType, d.ScopeID, d.PeriodType, d.Period, d.DimensionType,
	)
	if d.ModelTierID == nil {
		query = query.Where("model_tier_id IS NULL")
	} else {
		query = query.Where("model_tier_id = ?", *d.ModelTierID)
	}
	return query.Delete(&model.BudgetAlertRecord{}).Error
}

// DeleteByDimensionAndThreshold 删除某对象/周期/档位的去重记录（发送失败回滚用）。
// 比 DeleteByDimension 多匹配 threshold，避免回滚 80% 时误删同周期的 100% 记录。
func (r *BudgetAlertRepo) DeleteByDimensionAndThreshold(ctx context.Context, d AlertDimension, threshold int) error {
	query := r.db.WithContext(ctx).Where(
		"scope_type = ? AND scope_id = ? AND period_type = ? AND period = ? AND dimension_type = ? AND threshold = ?",
		d.ScopeType, d.ScopeID, d.PeriodType, d.Period, d.DimensionType, threshold,
	)
	if d.ModelTierID == nil {
		query = query.Where("model_tier_id IS NULL")
	} else {
		query = query.Where("model_tier_id = ?", *d.ModelTierID)
	}
	return query.Delete(&model.BudgetAlertRecord{}).Error
}

// FindOwnersByBudgetScope 查询某 scope 的告警接收人列表，用于站内信定向投递。
// 规则：
//   - company        → 全局 admin
//   - studio         → studio_admin（该工作室）+ 全局 admin
//   - dept_daily/dept_service → dept_admin（该部门）+ studio_admin（所属工作室）+ 全局 admin
//   - member         → 成员本人（scopeID 即 users.id），不通知 admin
func (r *BudgetAlertRepo) FindOwnersByBudgetScope(scopeType, scopeID string) ([]AlertOwnerRow, error) {
	// member scope：只通知成员本人，scopeID 即 users.id（UUID 字符串）
	if scopeType == model.BudgetScopeMember {
		var rows []AlertOwnerRow
		err := r.db.Raw(`
			SELECT id::text AS user_id, COALESCE(email,'') AS email
			FROM users
			WHERE id::text = ?
		`, scopeID).Scan(&rows).Error
		return rows, err
	}

	// 其他 scope：先查全局 admin
	var rows []AlertOwnerRow
	err := r.db.Raw(`
		SELECT ur.user_id, COALESCE(u.email,'') AS email
		FROM user_roles ur
		LEFT JOIN users u ON u.id::text = ur.user_id
		WHERE ur.role = 'admin'
	`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	switch scopeType {
	case model.BudgetScopeStudio:
		// studio_admin 管辖该工作室
		var extra []AlertOwnerRow
		if err := r.db.Raw(`
			SELECT ur.user_id, COALESCE(u.email,'') AS email
			FROM user_roles ur
			LEFT JOIN users u ON u.id::text = ur.user_id
			WHERE ur.role = 'studio_admin' AND ur.scope_id = ?
		`, scopeID).Scan(&extra).Error; err == nil {
			rows = append(rows, extra...)
		}
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		// dept_admin 直接管辖该部门
		var extra []AlertOwnerRow
		if err := r.db.Raw(`
			SELECT ur.user_id, COALESCE(u.email,'') AS email
			FROM user_roles ur
			LEFT JOIN users u ON u.id::text = ur.user_id
			WHERE ur.role = 'dept_admin' AND ur.scope_id = ?
		`, scopeID).Scan(&extra).Error; err == nil {
			rows = append(rows, extra...)
		}
		// 再查该部门所属工作室的 studio_admin（scopeID 可能是 UUID 也可能是部门名 dept_id）
		var studioID string
		r.db.Raw(`
			SELECT dsm.studio_id FROM departments d
			JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
			WHERE d.id::text = ? OR d.dept_id = ?
			LIMIT 1
		`, scopeID, scopeID).Scan(&studioID)
		if studioID != "" {
			var extra2 []AlertOwnerRow
			if err := r.db.Raw(`
				SELECT ur.user_id, COALESCE(u.email,'') AS email
				FROM user_roles ur
				LEFT JOIN users u ON u.id::text = ur.user_id
				WHERE ur.role = 'studio_admin' AND ur.scope_id = ?
			`, studioID).Scan(&extra2).Error; err == nil {
				rows = append(rows, extra2...)
			}
		}
	}

	// 去重
	seen := map[string]struct{}{}
	deduped := rows[:0]
	for _, r := range rows {
		if _, ok := seen[r.UserID]; !ok {
			seen[r.UserID] = struct{}{}
			deduped = append(deduped, r)
		}
	}
	return deduped, nil
}
