package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

// BudgetRepo 处理预算额度表的读写。
type BudgetRepo struct {
	db *gorm.DB
}

func NewBudgetRepo(db *gorm.DB) *BudgetRepo {
	return &BudgetRepo{db: db}
}

// GetAllocation 查询单层预算配置（按 scope_type + scope_id）。
// 未找到时返回 nil, nil（不是错误）。
func (r *BudgetRepo) GetAllocation(scopeType, scopeID string) (*model.BudgetAllocation, error) {
	var a model.BudgetAllocation
	err := r.db.Where("scope_type = ? AND scope_id = ? AND enabled = true", scopeType, scopeID).First(&a).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// UpsertAllocation 创建或更新预算配置（按 scope 唯一键）。
func (r *BudgetRepo) UpsertAllocation(a *model.BudgetAllocation) error {
	return r.db.Where(model.BudgetAllocation{
		ScopeType: a.ScopeType,
		ScopeID:   a.ScopeID,
	}).Assign(model.BudgetAllocation{
		AmountUSD:       a.AmountUSD,
		DailyAmountUSD:  a.DailyAmountUSD,
		WeeklyAmountUSD: a.WeeklyAmountUSD,
		Period:          a.Period,
		Enabled:         a.Enabled,
		CreatedBy:       a.CreatedBy,
		UpdatedBy:       a.UpdatedBy,
	}).FirstOrCreate(a).Error
}

// UpdateAmount 更新预算额度（改额时调用，同时清理告警去重记录由上层完成）。
func (r *BudgetRepo) UpdateAmount(scopeType, scopeID string, amountUSD float64) error {
	return r.db.Model(&model.BudgetAllocation{}).
		Where("scope_type = ? AND scope_id = ?", scopeType, scopeID).
		Update("amount_usd", amountUSD).Error
}

// ListByScopeIDs 批量查询多个 scope 的预算配置（用于构造 BudgetChain）。
func (r *BudgetRepo) ListByScopeIDs(scopes []struct{ ScopeType, ScopeID string }) ([]model.BudgetAllocation, error) {
	if len(scopes) == 0 {
		return nil, nil
	}
	// 构造 (scope_type, scope_id) IN (...) 条件
	type pair struct {
		ScopeType string
		ScopeID   string
	}
	pairs := make([]pair, len(scopes))
	for i, s := range scopes {
		pairs[i] = pair{s.ScopeType, s.ScopeID}
	}
	var allocs []model.BudgetAllocation
	// 逐条查（scope 链最多 4 层，性能可接受）
	for _, p := range pairs {
		var a model.BudgetAllocation
		if err := r.db.Where("scope_type = ? AND scope_id = ?", p.ScopeType, p.ScopeID).First(&a).Error; err == nil {
			allocs = append(allocs, a)
		}
	}
	return allocs, nil
}

// GetConsumption 查询某层某周期的已用量对账副本。
func (r *BudgetRepo) GetConsumption(scopeType, scopeID, period string) (*model.BudgetConsumption, error) {
	var c model.BudgetConsumption
	err := r.db.Where("scope_type = ? AND scope_id = ? AND period = ?", scopeType, scopeID, period).First(&c).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// UpsertConsumption 更新或创建已用量对账副本（对账任务调用）。
func (r *BudgetRepo) UpsertConsumption(c *model.BudgetConsumption) error {
	return r.db.Where(model.BudgetConsumption{
		ScopeType: c.ScopeType,
		ScopeID:   c.ScopeID,
		Period:    c.Period,
	}).Assign(model.BudgetConsumption{
		UsedUSD:  c.UsedUSD,
		SyncedAt: c.SyncedAt,
	}).FirstOrCreate(c).Error
}

// ListScoped 按管辖范围列出预算配置（用于管理面展示）。
func (r *BudgetRepo) ListScoped(scopeTypes []string, scopeIDs []string) ([]model.BudgetAllocation, error) {
	var allocs []model.BudgetAllocation

	// admin 查全部：scopeIDs 为空时，只按 scopeTypes 过滤
	if len(scopeIDs) == 0 {
		if len(scopeTypes) == 0 {
			return nil, nil
		}
		err := r.db.Where("scope_type IN ?", scopeTypes).
			Order("scope_type, scope_id").Find(&allocs).Error
		return allocs, err
	}

	// manager 查管辖范围：同时按 scopeTypes 和 scopeIDs 过滤
	err := r.db.Where("scope_type IN ? AND scope_id IN ?", scopeTypes, scopeIDs).
		Order("scope_type, scope_id").Find(&allocs).Error
	return allocs, err
}

// ListMemberIDsByDeptID 返回指定部门下所有成员的 users.id。
func (r *BudgetRepo) ListMemberIDsByDeptID(deptID string) ([]string, error) {
	var rows []struct {
		ID string `gorm:"column:id"`
	}
	err := r.db.Raw(`
		SELECT u.id::text AS id
		FROM users u
		JOIN departments d ON d.id = u.dept_id
		WHERE d.dept_id = ?
	`, deptID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ID != "" {
			ids = append(ids, row.ID)
		}
	}
	return ids, nil
}

// MemberInDeptIDs 判断成员是否归属于指定部门集合。
func (r *BudgetRepo) MemberInDeptIDs(memberID string, deptIDs []string) (bool, error) {
	if memberID == "" || len(deptIDs) == 0 {
		return false, nil
	}
	var count int64
	err := r.db.Raw(`
		SELECT COUNT(*)
		FROM users u
		JOIN departments d ON d.id = u.dept_id
		WHERE u.id::text = ? AND d.dept_id IN ?
	`, memberID, deptIDs).Scan(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteAllocation 删除指定成员的单独配额，还原为共享部门预算池。
func (r *BudgetRepo) DeleteAllocation(scopeType, scopeID string) error {
	return r.db.Where("scope_type = ? AND scope_id = ?", scopeType, scopeID).
		Delete(&model.BudgetAllocation{}).Error
}

// DB 暴露底层 db 实例，供跨 repo 联合查询使用（如 InitDefaultBudgets）。
func (r *BudgetRepo) DB() *gorm.DB {
	return r.db
}

// ListDeptIDsByStudioID 返回指定工作室下所有部门的 dept_id（SSO 字符串）。
// 同时查询 dept_studio_map 和 departments.studio_id 两个来源，取并集。
func (r *BudgetRepo) ListDeptIDsByStudioID(studioID string) ([]string, error) {
	var rows []struct {
		DeptID string `gorm:"column:dept_id"`
	}
	err := r.db.Raw(`
		SELECT DISTINCT d.dept_id
		FROM departments d
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		WHERE dsm.studio_id = ?::uuid
		   OR d.studio_id  = ?::uuid
	`, studioID, studioID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.DeptID != "" {
			ids = append(ids, row.DeptID)
		}
	}
	return ids, nil
}

// ListDeptStudioRows 列出所有（部门 - 所属工作室）组合。
// 满足 service.BudgetAllocationRepo 接口。
// 返回的 DeptID 永不为空，StudioID 可能为空。
func (r *BudgetRepo) ListDeptStudioRows() ([]model.DeptStudioRow, error) {
	var rows []model.DeptStudioRow
	err := r.db.Raw(`
		SELECT DISTINCT d.dept_id::text AS dept_id,
		       COALESCE(dsm.studio_id::text, '') AS studio_id,
		       COALESCE(s.name, '') AS studio_name
		FROM departments d
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		LEFT JOIN studios s ON s.id = dsm.studio_id
		WHERE d.dept_id IS NOT NULL
	`).Scan(&rows).Error
	return rows, err
}

// ResolveScopeNames 批量将 scope_id 解析为可读名称。
// scope_type 决定查哪张表：
//   - studio → studios.name
//   - dept_daily / dept_service → departments.dept_name
//   - member → users.name
func (r *BudgetRepo) ResolveScopeNames(scopeType string, scopeIDs []string) (map[string]string, error) {
	if len(scopeIDs) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(scopeIDs))

	switch scopeType {
	case model.BudgetScopeStudio:
		var rows []struct {
			ID   string `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		err := r.db.Raw(`SELECT id::text AS id, name FROM studios WHERE id::text IN ?`, scopeIDs).Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.ID] = row.Name
		}

	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		var rows []struct {
			DeptID   string `gorm:"column:dept_id"`
			DeptName string `gorm:"column:dept_name"`
		}
		err := r.db.Raw(`SELECT dept_id::text AS dept_id, COALESCE(dept_name, dept_id::text) AS dept_name FROM departments WHERE dept_id::text IN ?`, scopeIDs).Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.DeptID] = row.DeptName
		}

	case model.BudgetScopeMember:
		var rows []struct {
			ID   string `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		err := r.db.Raw(`SELECT id::text AS id, COALESCE(name, id::text) AS name FROM users WHERE id::text IN ?`, scopeIDs).Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.ID] = row.Name
		}
	}

	return result, nil
}
