package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

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
		// 再查该部门所属工作室的 studio_admin
		var studioID string
		r.db.Raw(`
			SELECT dsm.studio_id FROM departments d
			JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
			WHERE d.id::text = ?
			LIMIT 1
		`, scopeID).Scan(&studioID)
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
