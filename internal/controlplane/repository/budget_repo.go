package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BatchTierRiskSnapshot struct {
	ParentLimitUSD       *budgetdomain.Amount
	SiblingLimitTotalUSD budgetdomain.Amount
	ParentVersion        string
	TargetAllocation     *model.BudgetAllocation
	TargetLimit          *model.BudgetTierLimit
}

var ErrInvalidBudgetPeriod = errors.New("invalid budget period")

// BudgetRepo 处理预算额度表的读写。
type BudgetRepo struct {
	db *gorm.DB
}

type MyBudgetScopeAllocation struct {
	Allocation     model.BudgetAllocation
	Label          string
	VisibleAmounts bool
}

func NewBudgetRepo(db *gorm.DB) *BudgetRepo {
	return &BudgetRepo{db: db}
}

func (r *BudgetRepo) ResolveMyBudgetChain(ctx context.Context, userID string) ([]MyBudgetScopeAllocation, error) {
	var identity struct {
		UserID     string
		DeptID     string
		DeptName   string
		StudioID   string
		StudioName string
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT CAST(u.id AS TEXT) AS user_id, COALESCE(d.dept_id, '') AS dept_id,
		       COALESCE(d.dept_name, '') AS dept_name,
		       COALESCE(CAST(d.studio_id AS TEXT), CAST(dsm.studio_id AS TEXT), '') AS studio_id,
		       COALESCE(s.name, '') AS studio_name
		FROM users u
		LEFT JOIN departments d ON d.id = u.dept_id
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		LEFT JOIN studios s ON CAST(s.id AS TEXT) = COALESCE(CAST(d.studio_id AS TEXT), CAST(dsm.studio_id AS TEXT))
		WHERE CAST(u.id AS TEXT) = ?
		LIMIT 1
	`, userID).Scan(&identity).Error
	if err != nil {
		return nil, err
	}
	if identity.UserID == "" || identity.DeptID == "" || identity.StudioID == "" {
		return nil, fmt.Errorf("%w: %s", ErrMyBudgetChainUnavailable, userID)
	}
	defs := []struct {
		scopeType string
		scopeID   string
		label     string
		visible   bool
	}{
		{model.BudgetScopeMember, identity.UserID, "我的额度", true},
		{model.BudgetScopeDeptDaily, identity.DeptID, identity.DeptName + "·成员日常预算", false},
		{model.BudgetScopeStudio, identity.StudioID, identity.StudioName, false},
		{model.BudgetScopeCompany, model.BudgetScopeGlobal, "全公司", false},
	}
	result := make([]MyBudgetScopeAllocation, 0, len(defs))
	for _, def := range defs {
		if def.scopeID == "" {
			continue
		}
		allocation, findErr := r.GetAllocation(def.scopeType, def.scopeID)
		if findErr != nil {
			return nil, findErr
		}
		if allocation == nil {
			if def.scopeType == model.BudgetScopeMember {
				continue
			}
			allocation = &model.BudgetAllocation{ScopeType: def.scopeType, ScopeID: def.scopeID, Enabled: false}
		}
		result = append(result, MyBudgetScopeAllocation{Allocation: *allocation, Label: def.label, VisibleAmounts: def.visible})
	}
	return result, nil
}

func (r *BudgetRepo) ResolveBudgetChain(ctx context.Context, scopeType, scopeID string) ([]MyBudgetScopeAllocation, error) {
	return r.resolveBudgetChain(ctx, r.db, scopeType, scopeID, false, false)
}

func (r *BudgetRepo) ResolveBudgetChainForUpdate(ctx context.Context, tx *gorm.DB, scopeType, scopeID string) ([]MyBudgetScopeAllocation, error) {
	return r.resolveBudgetChain(ctx, tx, scopeType, scopeID, true, true)
}

func (r *BudgetRepo) resolveBudgetChain(ctx context.Context, db *gorm.DB, scopeType, scopeID string, lock bool, createMissing bool) ([]MyBudgetScopeAllocation, error) {
	type definition struct{ scopeType, scopeID, label string }
	defs := make([]definition, 0, 4)
	switch scopeType {
	case model.BudgetScopeCompany:
		defs = append(defs, definition{model.BudgetScopeCompany, scopeID, "全公司"})
	case model.BudgetScopeStudio:
		var studio struct{ ID, Name string }
		query := db.WithContext(ctx).Table("studios").Select("CAST(id AS TEXT) AS id, name").Where("CAST(id AS TEXT) = ?", scopeID)
		if lock {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Take(&studio).Error; err != nil {
			return nil, fmt.Errorf("resolve studio budget chain: %w", err)
		}
		defs = append(defs, definition{scopeType, studio.ID, studio.Name}, definition{model.BudgetScopeCompany, model.BudgetScopeGlobal, "全公司"})
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		var dept struct{ DeptID, DeptName, StudioID, StudioName string }
		query := db.WithContext(ctx).Table("departments d").
			Select("d.dept_id, d.dept_name, COALESCE(CAST(d.studio_id AS TEXT), CAST(dsm.studio_id AS TEXT), '') AS studio_id, COALESCE(s.name, '') AS studio_name").
			Joins("LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id").
			Joins("LEFT JOIN studios s ON CAST(s.id AS TEXT) = COALESCE(CAST(d.studio_id AS TEXT), CAST(dsm.studio_id AS TEXT))").
			Where("d.dept_id = ?", scopeID)
		if lock {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "d"}})
		}
		if err := query.Take(&dept).Error; err != nil {
			return nil, fmt.Errorf("resolve department budget chain: %w", err)
		}
		if dept.StudioID == "" {
			return nil, fmt.Errorf("department %s has no studio", scopeID)
		}
		defs = append(defs, definition{scopeType, dept.DeptID, dept.DeptName}, definition{model.BudgetScopeStudio, dept.StudioID, dept.StudioName}, definition{model.BudgetScopeCompany, model.BudgetScopeGlobal, "全公司"})
	case model.BudgetScopeMember:
		var identity struct{ UserID, DeptID, DeptName, StudioID, StudioName string }
		query := db.WithContext(ctx).Table("users u").
			Select("CAST(u.id AS TEXT) AS user_id, d.dept_id, d.dept_name, COALESCE(CAST(d.studio_id AS TEXT), CAST(dsm.studio_id AS TEXT), '') AS studio_id, COALESCE(s.name, '') AS studio_name").
			Joins("JOIN departments d ON d.id = u.dept_id").
			Joins("LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id").
			Joins("LEFT JOIN studios s ON CAST(s.id AS TEXT) = COALESCE(CAST(d.studio_id AS TEXT), CAST(dsm.studio_id AS TEXT))").
			Where("CAST(u.id AS TEXT) = ?", scopeID)
		if lock {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "u"}})
		}
		if err := query.Take(&identity).Error; err != nil {
			return nil, fmt.Errorf("resolve member budget chain: %w", err)
		}
		if identity.DeptID == "" || identity.StudioID == "" {
			return nil, fmt.Errorf("incomplete member budget chain: %s", scopeID)
		}
		defs = append(defs,
			definition{scopeType, identity.UserID, "我的额度"},
			definition{model.BudgetScopeDeptDaily, identity.DeptID, identity.DeptName},
			definition{model.BudgetScopeStudio, identity.StudioID, identity.StudioName},
			definition{model.BudgetScopeCompany, model.BudgetScopeGlobal, "全公司"})
	default:
		return nil, fmt.Errorf("unsupported budget scope: %s", scopeType)
	}
	locked := make(map[string]model.BudgetAllocation, len(defs))
	for index := len(defs) - 1; index >= 0; index-- {
		def := defs[index]
		query := db.WithContext(ctx).Where("scope_type = ? AND scope_id = ? AND enabled = true", def.scopeType, def.scopeID)
		if lock {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var allocation model.BudgetAllocation
		if err := query.First(&allocation).Error; err != nil {
			if createMissing && errors.Is(err, gorm.ErrRecordNotFound) {
				now := time.Now().UTC()
				allocation = model.BudgetAllocation{
					ID: uuid.New(), ScopeType: def.scopeType, ScopeID: def.scopeID,
					AmountUSD: 0, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
					Period: "monthly", Enabled: true, CreatedAt: now, UpdatedAt: now,
				}
				if createErr := db.WithContext(ctx).Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "scope_type"}, {Name: "scope_id"}},
					DoNothing: true,
				}).Create(&allocation).Error; createErr != nil {
					return nil, fmt.Errorf("create budget allocation %s/%s: %w", def.scopeType, def.scopeID, createErr)
				}
				lockQuery := db.WithContext(ctx).Where("scope_type = ? AND scope_id = ? AND enabled = true", def.scopeType, def.scopeID)
				if lock {
					lockQuery = lockQuery.Clauses(clause.Locking{Strength: "UPDATE"})
				}
				if lockErr := lockQuery.First(&allocation).Error; lockErr != nil {
					return nil, fmt.Errorf("resolve budget allocation %s/%s: %w", def.scopeType, def.scopeID, lockErr)
				}
				locked[def.scopeType+":"+def.scopeID] = allocation
				continue
			}
			return nil, fmt.Errorf("resolve budget allocation %s/%s: %w", def.scopeType, def.scopeID, err)
		}
		locked[def.scopeType+":"+def.scopeID] = allocation
	}
	result := make([]MyBudgetScopeAllocation, 0, len(defs))
	for _, def := range defs {
		result = append(result, MyBudgetScopeAllocation{Allocation: locked[def.scopeType+":"+def.scopeID], Label: def.label, VisibleAmounts: def.scopeType == scopeType})
	}
	return result, nil
}

func (r *BudgetRepo) ResolveMyBudgetChainForUpdate(ctx context.Context, tx *gorm.DB, userID string) ([]MyBudgetScopeAllocation, error) {
	var lockedUser struct{ ID string }
	if err := tx.WithContext(ctx).Table("users").Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("CAST(id AS TEXT) AS id").Where("CAST(id AS TEXT) = ?", userID).Take(&lockedUser).Error; err != nil {
		return nil, fmt.Errorf("lock budget chain user %s: %w", userID, err)
	}
	var identity struct {
		UserID, DeptID, DeptName, StudioID, StudioName string
	}
	result := tx.WithContext(ctx).Raw(`
		SELECT CAST(u.id AS TEXT) AS user_id, COALESCE(d.dept_id, '') AS dept_id,
		       COALESCE(d.dept_name, '') AS dept_name,
		       COALESCE(CAST(d.studio_id AS TEXT), CAST(dsm.studio_id AS TEXT), '') AS studio_id,
		       COALESCE(s.name, '') AS studio_name
		FROM users u
		LEFT JOIN departments d ON d.id = u.dept_id
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		LEFT JOIN studios s ON CAST(s.id AS TEXT) = COALESCE(CAST(d.studio_id AS TEXT), CAST(dsm.studio_id AS TEXT))
		WHERE CAST(u.id AS TEXT) = ? LIMIT 1`, userID).Scan(&identity)
	if result.Error != nil {
		return nil, result.Error
	}
	if identity.UserID == "" {
		return nil, fmt.Errorf("budget chain identity not found: %s", userID)
	}
	if identity.DeptID == "" || identity.StudioID == "" {
		return nil, fmt.Errorf("incomplete budget chain identity for %s", userID)
	}
	for _, lock := range []struct{ table, column, value string }{
		{"departments", "dept_id", identity.DeptID}, {"studios", "id", identity.StudioID},
	} {
		var row struct{ Value string }
		if err := tx.WithContext(ctx).Table(lock.table).Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("CAST("+lock.column+" AS TEXT) AS value").Where("CAST("+lock.column+" AS TEXT) = ?", lock.value).Take(&row).Error; err != nil {
			return nil, fmt.Errorf("lock budget chain organization %s/%s: %w", lock.table, lock.value, err)
		}
	}
	defs := []struct{ scopeType, scopeID, label string }{
		{model.BudgetScopeMember, identity.UserID, "我的额度"},
		{model.BudgetScopeDeptDaily, identity.DeptID, identity.DeptName + "·成员日常预算"},
		{model.BudgetScopeStudio, identity.StudioID, identity.StudioName},
		{model.BudgetScopeCompany, model.BudgetScopeGlobal, "全公司"},
	}
	locked := make(map[string]model.BudgetAllocation, len(defs))
	for index := len(defs) - 1; index >= 0; index-- {
		def := defs[index]
		if def.scopeID == "" {
			return nil, fmt.Errorf("incomplete budget chain for %s at %s", userID, def.scopeType)
		}
		var allocation model.BudgetAllocation
		err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scope_type = ? AND scope_id = ? AND enabled = true", def.scopeType, def.scopeID).First(&allocation).Error
		if err != nil {
			return nil, fmt.Errorf("lock budget chain %s/%s: %w", def.scopeType, def.scopeID, err)
		}
		locked[def.scopeType+":"+def.scopeID] = allocation
	}
	chain := make([]MyBudgetScopeAllocation, 0, len(defs))
	for _, def := range defs {
		chain = append(chain, MyBudgetScopeAllocation{Allocation: locked[def.scopeType+":"+def.scopeID], Label: def.label, VisibleAmounts: def.scopeType == model.BudgetScopeMember})
	}
	return chain, nil
}

type BudgetAffectedGroup struct {
	ID   string
	Name string
}

func (r *BudgetRepo) ResolveBudgetImpact(
	ctx context.Context,
	scopeType, scopeID string,
	modelTierID *string,
	limit int,
) (int, []BudgetAffectedGroup, error) {
	memberQuery := r.db.WithContext(ctx).Table("users u")
	switch scopeType {
	case model.BudgetScopeMember:
		memberQuery = memberQuery.Where("CAST(u.id AS TEXT) = ?", scopeID)
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		memberQuery = memberQuery.Joins("JOIN departments d ON d.id = u.dept_id").Where("d.dept_id = ?", scopeID)
	case model.BudgetScopeStudio:
		memberQuery = memberQuery.Joins("JOIN departments d ON d.id = u.dept_id").
			Joins("LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id").
			Where("COALESCE(CAST(d.studio_id AS TEXT), CAST(dsm.studio_id AS TEXT)) = ?", scopeID)
	}
	var members int64
	if err := memberQuery.Count(&members).Error; err != nil {
		return 0, nil, err
	}
	groupQuery := r.db.WithContext(ctx).Table("model_groups").Select("CAST(id AS TEXT) AS id, name").
		Where("deleted_at IS NULL")
	if modelTierID != nil && *modelTierID != "" {
		groupQuery = groupQuery.Where("CAST(budget_tier_id AS TEXT) = ?", *modelTierID)
	}
	var groups []BudgetAffectedGroup
	if err := groupQuery.Order("name").Limit(limit).Scan(&groups).Error; err != nil {
		return 0, nil, err
	}
	return int(members), groups, nil
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

func (r *BudgetRepo) GetAllocationByID(ctx context.Context, id uuid.UUID) (*model.BudgetAllocation, error) {
	var allocation model.BudgetAllocation
	err := r.db.WithContext(ctx).First(&allocation, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &allocation, nil
}

func (r *BudgetRepo) GetAllocationByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*model.BudgetAllocation, error) {
	var allocation model.BudgetAllocation
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&allocation, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &allocation, nil
}

func (r *BudgetRepo) GetAllocationByIDInTx(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*model.BudgetAllocation, error) {
	var allocation model.BudgetAllocation
	if err := tx.WithContext(ctx).First(&allocation, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &allocation, nil
}

func (r *BudgetRepo) GetAllocationInTx(ctx context.Context, tx *gorm.DB, scopeType, scopeID string) (*model.BudgetAllocation, error) {
	var allocation model.BudgetAllocation
	err := tx.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ? AND enabled = true", scopeType, scopeID).First(&allocation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &allocation, nil
}

func (r *BudgetRepo) GetAllocationForUpdate(ctx context.Context, tx *gorm.DB, scopeType, scopeID string) (*model.BudgetAllocation, error) {
	var allocation model.BudgetAllocation
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("scope_type = ? AND scope_id = ? AND enabled = true", scopeType, scopeID).First(&allocation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &allocation, nil
}

func (r *BudgetRepo) CompareAndSwapAllocationLimits(
	ctx context.Context,
	before model.BudgetAllocation,
	monthly, daily, weekly float64,
	actorID uuid.UUID,
) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.BudgetAllocation{}).
		Where("id = ? AND amount_usd = ? AND daily_amount_usd = ? AND weekly_amount_usd = ? AND enabled = ?",
			before.ID, before.AmountUSD, before.DailyAmountUSD, before.WeeklyAmountUSD, before.Enabled).
		Updates(map[string]any{
			"amount_usd": monthly, "daily_amount_usd": daily, "weekly_amount_usd": weekly,
			"updated_by": actorID, "updated_at": time.Now().UTC(),
		})
	return result.RowsAffected == 1, result.Error
}

func (r *BudgetRepo) GetBatchTierRiskSnapshot(
	ctx context.Context,
	allocation model.BudgetAllocation,
	tierID uuid.UUID,
	periodType budgetdomain.PeriodType,
) (BatchTierRiskSnapshot, error) {
	return r.getBatchTierRiskSnapshot(ctx, r.db, allocation, tierID, periodType, false)
}

func (r *BudgetRepo) GetBatchTierRiskSnapshotInTx(
	ctx context.Context,
	tx *gorm.DB,
	allocation model.BudgetAllocation,
	tierID uuid.UUID,
	periodType budgetdomain.PeriodType,
) (BatchTierRiskSnapshot, error) {
	return r.getBatchTierRiskSnapshot(ctx, tx, allocation, tierID, periodType, true)
}

func (r *BudgetRepo) getBatchTierRiskSnapshot(
	ctx context.Context,
	db *gorm.DB,
	allocation model.BudgetAllocation,
	tierID uuid.UUID,
	periodType budgetdomain.PeriodType,
	lock bool,
) (BatchTierRiskSnapshot, error) {
	zero, _ := budgetdomain.ParseAmount("0")
	result := BatchTierRiskSnapshot{SiblingLimitTotalUSD: zero}
	parentScopeType, parentScopeID := "", ""
	childScopeIDs := []string{}
	switch allocation.ScopeType {
	case model.BudgetScopeStudio:
		parentScopeType, parentScopeID = model.BudgetScopeCompany, model.BudgetScopeGlobal
		var rows []model.BudgetAllocation
		query := db.WithContext(ctx).Where("scope_type = ?", model.BudgetScopeStudio).Order("id")
		if err := query.Find(&rows).Error; err != nil {
			return result, err
		}
		for _, row := range rows {
			childScopeIDs = append(childScopeIDs, row.ScopeID)
		}
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		var studioID string
		if err := db.WithContext(ctx).Raw(`
			SELECT COALESCE(dsm.studio_id::text, d.studio_id::text, '')
			FROM departments d
			LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
			WHERE d.dept_id::text = ?
			LIMIT 1
		`, allocation.ScopeID).Scan(&studioID).Error; err != nil {
			return result, err
		}
		if studioID == "" {
			return result, nil
		}
		parentScopeType, parentScopeID = model.BudgetScopeStudio, studioID
		ids, err := listDeptIDsByStudioIDDB(db.WithContext(ctx), studioID)
		if err != nil {
			return result, err
		}
		childScopeIDs = ids
	case model.BudgetScopeMember:
		var deptID string
		if err := db.WithContext(ctx).Raw(`
			SELECT COALESCE(d.dept_id::text, '')
			FROM users u
			JOIN departments d ON d.id = u.dept_id
			WHERE u.id::text = ?
			LIMIT 1
		`, allocation.ScopeID).Scan(&deptID).Error; err != nil {
			return result, err
		}
		if deptID == "" {
			return result, nil
		}
		parentScopeType, parentScopeID = model.BudgetScopeDeptDaily, deptID
		ids, err := listMemberIDsByDeptIDDB(db.WithContext(ctx), deptID)
		if err != nil {
			return result, err
		}
		childScopeIDs = ids
	default:
		return r.loadTargetTierState(ctx, db, allocation, tierID, periodType, lock, result)
	}
	var parent model.BudgetAllocation
	parentQuery := db.WithContext(ctx).Where("scope_type = ? AND scope_id = ? AND enabled = true", parentScopeType, parentScopeID)
	if lock {
		parentQuery = parentQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := parentQuery.First(&parent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.loadTargetTierState(ctx, db, allocation, tierID, periodType, lock, result)
	}
	if err != nil {
		return result, err
	}

	var childAllocations []model.BudgetAllocation
	childQuery := db.WithContext(ctx).
		Where("scope_type = ? AND (scope_id IN ? OR id = ?) AND enabled = true", allocation.ScopeType, childScopeIDs, allocation.ID).
		Order("id")
	if lock {
		childQuery = childQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := childQuery.Find(&childAllocations).Error; err != nil {
		return result, err
	}
	for index := range childAllocations {
		if childAllocations[index].ID == allocation.ID {
			target := childAllocations[index]
			result.TargetAllocation = &target
			break
		}
	}
	if result.TargetAllocation == nil {
		target := allocation
		result.TargetAllocation = &target
	}

	allocationIDs := make([]uuid.UUID, 0, len(childAllocations)+1)
	allocationIDs = append(allocationIDs, parent.ID)
	for _, child := range childAllocations {
		allocationIDs = append(allocationIDs, child.ID)
	}
	var limits []model.BudgetTierLimit
	limitQuery := db.WithContext(ctx).
		Where("budget_allocation_id IN ? AND model_tier_id = ? AND period_type = ?", allocationIDs, tierID, periodType).
		Order("id")
	if lock {
		limitQuery = limitQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := limitQuery.Find(&limits).Error; err != nil {
		return result, err
	}

	result.ParentVersion = fmt.Sprintf("%s:%s:%f:%f:%f:%t:limit:missing",
		parent.ID, parent.UpdatedAt.UTC().Format(time.RFC3339Nano), parent.AmountUSD,
		parent.DailyAmountUSD, parent.WeeklyAmountUSD, parent.Enabled)
	childByID := make(map[uuid.UUID]model.BudgetAllocation, len(childAllocations))
	for _, child := range childAllocations {
		childByID[child.ID] = child
	}
	total := result.SiblingLimitTotalUSD.Decimal
	var siblingVersion strings.Builder
	for index := range limits {
		limit := &limits[index]
		if limit.BudgetAllocationID == parent.ID {
			if limit.Enabled {
				parentAmount := limit.LimitUSD
				result.ParentLimitUSD = &parentAmount
			}
			result.ParentVersion = fmt.Sprintf("%s:%s:%f:%f:%f:%t:%s:%s:%t",
				parent.ID, parent.UpdatedAt.UTC().Format(time.RFC3339Nano), parent.AmountUSD,
				parent.DailyAmountUSD, parent.WeeklyAmountUSD, parent.Enabled,
				limit.UpdatedAt.UTC().Format(time.RFC3339Nano), limit.LimitUSD.String(), limit.Enabled)
			continue
		}
		if limit.BudgetAllocationID == allocation.ID {
			target := *limit
			result.TargetLimit = &target
		}
		if _, isChild := childByID[limit.BudgetAllocationID]; !isChild {
			continue
		}
		if limit.Enabled {
			total = total.Add(limit.LimitUSD.Decimal)
		}
		fmt.Fprintf(&siblingVersion, ":%s:%s:%s:%t", limit.ID, limit.UpdatedAt.UTC().Format(time.RFC3339Nano), limit.LimitUSD.String(), limit.Enabled)
	}
	result.SiblingLimitTotalUSD, _ = budgetdomain.ParseAmount(total.String())
	result.ParentVersion += ":siblings" + siblingVersion.String()
	return result, nil
}

func (r *BudgetRepo) loadTargetTierState(
	ctx context.Context,
	db *gorm.DB,
	allocation model.BudgetAllocation,
	tierID uuid.UUID,
	periodType budgetdomain.PeriodType,
	lock bool,
	result BatchTierRiskSnapshot,
) (BatchTierRiskSnapshot, error) {
	allocationQuery := db.WithContext(ctx).Where("id = ?", allocation.ID)
	if lock {
		allocationQuery = allocationQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var target model.BudgetAllocation
	if err := allocationQuery.First(&target).Error; err != nil {
		return result, err
	}
	result.TargetAllocation = &target
	limitQuery := db.WithContext(ctx).
		Where("budget_allocation_id = ? AND model_tier_id = ? AND period_type = ?", target.ID, tierID, periodType).
		Order("id")
	if lock {
		limitQuery = limitQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var limits []model.BudgetTierLimit
	if err := limitQuery.Find(&limits).Error; err != nil {
		return result, err
	}
	if len(limits) > 0 {
		limit := limits[0]
		result.TargetLimit = &limit
	}
	return result, nil
}

func listDeptIDsByStudioIDDB(db *gorm.DB, studioID string) ([]string, error) {
	var rows []struct {
		DeptID string `gorm:"column:dept_id"`
	}
	err := db.Raw(`
		SELECT DISTINCT d.dept_id
		FROM departments d
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		WHERE dsm.studio_id = ?::uuid OR d.studio_id = ?::uuid
	`, studioID, studioID).Scan(&rows).Error
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.DeptID != "" {
			ids = append(ids, row.DeptID)
		}
	}
	return ids, err
}

func listMemberIDsByDeptIDDB(db *gorm.DB, deptID string) ([]string, error) {
	var rows []struct {
		ID string `gorm:"column:id"`
	}
	err := db.Raw(`
		SELECT u.id::text AS id
		FROM users u
		JOIN departments d ON d.id = u.dept_id
		WHERE d.dept_id = ?
	`, deptID).Scan(&rows).Error
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ID != "" {
			ids = append(ids, row.ID)
		}
	}
	return ids, err
}

func (r *BudgetRepo) ApplyTotalScheduledLimit(ctx context.Context, tx *gorm.DB, allocationID uuid.UUID, period budgetdomain.PeriodType, target budgetdomain.Amount) error {
	value, _ := target.Decimal.Float64()
	column := ""
	switch period {
	case budgetdomain.PeriodDaily:
		column = "daily_amount_usd"
	case budgetdomain.PeriodWeekly:
		column = "weekly_amount_usd"
	case budgetdomain.PeriodMonthly:
		column = "amount_usd"
	default:
		return ErrInvalidBudgetPeriod
	}
	result := tx.WithContext(ctx).Model(&model.BudgetAllocation{}).Where("id = ?", allocationID).Update(column, value)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
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

func (r *BudgetRepo) CreatePersonalAllocationWithTierLimit(ctx context.Context, allocation *model.BudgetAllocation, params UpsertTierLimitParams) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.CreatePersonalAllocationWithTierLimitInTx(ctx, tx, allocation, params)
	})
}

func (r *BudgetRepo) CreatePersonalAllocationWithTierLimitInTx(ctx context.Context, tx *gorm.DB, allocation *model.BudgetAllocation, params UpsertTierLimitParams) error {
	if err := tx.WithContext(ctx).Create(allocation).Error; err != nil {
		if isUniqueConstraintError(err) {
			return ErrUniqueConflict
		}
		return err
	}
	params.BudgetAllocationID = allocation.ID
	return UpsertTierLimitInTx(ctx, tx, params)
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

func (r *BudgetRepo) ResolveMemberDeptID(memberID string) (string, error) {
	if memberID == "" {
		return "", nil
	}
	m, err := r.MapMemberDeptIDs([]string{memberID})
	if err != nil {
		return "", err
	}
	return m[memberID], nil
}

// MapMemberDeptIDs 批量解析 user_id → SSO dept_id。
func (r *BudgetRepo) MapMemberDeptIDs(memberIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(memberIDs))
	if len(memberIDs) == 0 {
		return out, nil
	}
	uniq := make([]string, 0, len(memberIDs))
	seen := make(map[string]struct{}, len(memberIDs))
	for _, id := range memberIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	var rows []struct {
		UserID string `gorm:"column:user_id"`
		DeptID string `gorm:"column:dept_id"`
	}
	err := r.db.Raw(`
		SELECT CAST(u.id AS TEXT) AS user_id, COALESCE(d.dept_id, '') AS dept_id
		FROM users u
		JOIN departments d ON d.id = u.dept_id
		WHERE CAST(u.id AS TEXT) IN ?
	`, uniq).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.UserID != "" && row.DeptID != "" {
			out[row.UserID] = row.DeptID
		}
	}
	return out, nil
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
