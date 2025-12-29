package service

import (
	"context"
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

// BudgetService 处理预算额度的 CRUD 与告警去重记录管理。
type BudgetService struct {
	repo      *repository.BudgetRepo
	alertRepo *repository.BudgetAlertRepo
}

func NewBudgetService(repo *repository.BudgetRepo, alertRepo *repository.BudgetAlertRepo) *BudgetService {
	return &BudgetService{repo: repo, alertRepo: alertRepo}
}

// BudgetItem 预算展示项（含实时 used_usd 占位，网关侧从 Redis 读）。
type BudgetItem struct {
	ScopeType string  `json:"scope_type"`
	ScopeID   string  `json:"scope_id"`
	Label     string  `json:"label,omitempty"`
	AmountUSD float64 `json:"amount_usd"`
	Period    string  `json:"period"`
	Enabled   bool    `json:"enabled"`
	// UsedUSD 在控制面接口中从 budget_consumption 取（对账副本，非实时）
	UsedUSD   float64 `json:"used_usd"`
	UsedRatio float64 `json:"used_ratio"`
}

// SetBudget 设置/更新某层预算额度。
// 改额后自动清理该 period 的告警去重记录，使扩容后可再次告警（§H 决策）。
func (s *BudgetService) SetBudget(ctx context.Context, scopeType, scopeID string, amountUSD float64, createdBy uuid.UUID) error {
	if amountUSD < 0 {
		return fmt.Errorf("amount_usd must be >= 0")
	}
	a := &model.BudgetAllocation{
		ScopeType: scopeType,
		ScopeID:   scopeID,
		AmountUSD: amountUSD,
		Period:    "monthly",
		Enabled:   true,
		CreatedBy: createdBy,
	}
	if err := s.repo.UpsertAllocation(a); err != nil {
		return fmt.Errorf("upsert budget: %w", err)
	}
	// 改额后清理本月告警去重记录，让扩容后能再次触发告警
	period := time.Now().Format("2006-01")
	_ = s.alertRepo.DeleteByScope(scopeType, scopeID, period)
	return nil
}

// GetBudget 查询单层预算（控制面展示，含 used_usd 对账副本）。
func (s *BudgetService) GetBudget(ctx context.Context, scopeType, scopeID string) (*BudgetItem, error) {
	alloc, err := s.repo.GetAllocation(scopeType, scopeID)
	if err != nil {
		return nil, err
	}
	item := &BudgetItem{
		ScopeType: scopeType,
		ScopeID:   scopeID,
		Period:    "monthly",
		Enabled:   true,
	}
	if alloc != nil {
		item.AmountUSD = alloc.AmountUSD
		item.Enabled = alloc.Enabled
	}
	// 查对账副本 used_usd
	period := time.Now().Format("2006-01")
	cons, _ := s.repo.GetConsumption(scopeType, scopeID, period)
	if cons != nil {
		item.UsedUSD = cons.UsedUSD
		if item.AmountUSD > 0 {
			item.UsedRatio = item.UsedUSD / item.AmountUSD
		}
	}
	return item, nil
}

// ListScoped 按管辖范围列出预算配置（管理面用）。
func (s *BudgetService) ListScoped(ctx context.Context, scopeTypes []string, scopeIDs []string) ([]BudgetItem, error) {
	allocs, err := s.repo.ListScoped(scopeTypes, scopeIDs)
	if err != nil {
		return nil, err
	}
	period := time.Now().Format("2006-01")
	items := make([]BudgetItem, 0, len(allocs))
	for _, a := range allocs {
		item := BudgetItem{
			ScopeType: a.ScopeType,
			ScopeID:   a.ScopeID,
			AmountUSD: a.AmountUSD,
			Period:    a.Period,
			Enabled:   a.Enabled,
		}
		cons, _ := s.repo.GetConsumption(a.ScopeType, a.ScopeID, period)
		if cons != nil {
			item.UsedUSD = cons.UsedUSD
			if item.AmountUSD > 0 {
				item.UsedRatio = item.UsedUSD / item.AmountUSD
			}
		}
		items = append(items, item)
	}
	return items, nil
}
