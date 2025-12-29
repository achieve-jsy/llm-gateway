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
		AmountUSD: a.AmountUSD,
		Period:    a.Period,
		Enabled:   a.Enabled,
		CreatedBy: a.CreatedBy,
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
	if len(scopeIDs) == 0 {
		return nil, nil
	}
	var allocs []model.BudgetAllocation
	err := r.db.Where("scope_type IN ? AND scope_id IN ?", scopeTypes, scopeIDs).
		Order("scope_type, scope_id").Find(&allocs).Error
	return allocs, err
}
