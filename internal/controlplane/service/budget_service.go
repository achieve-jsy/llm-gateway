package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// BudgetService 处理预算额度的 CRUD 与告警去重记录管理。
type BudgetService struct {
	repo      *repository.BudgetRepo
	alertRepo *repository.BudgetAlertRepo
	publisher EventPublisher
	rdb       *redis.Client
}

func NewBudgetService(repo *repository.BudgetRepo, alertRepo *repository.BudgetAlertRepo) *BudgetService {
	return &BudgetService{repo: repo, alertRepo: alertRepo}
}

// SetPublisher 注入事件发布器（可选，router setup 时调用）
// 用于 SetBudget 改额后广播 auth_config_changed，刷新网关 BudgetChain 快照。
func (s *BudgetService) SetPublisher(pub EventPublisher) {
	s.publisher = pub
}

// SetRedis 注入 Redis 客户端（可选，router setup 时调用）
// 用于 SetBudget 改额后写入 budget:amount:{scope_type}:{scope_id} 键，
// 让网关能实时读取最新额度（无需等 verify-key 缓存刷新）。
func (s *BudgetService) SetRedis(rdb *redis.Client) {
	s.rdb = rdb
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
	// v0.7: 写 Redis budget:amount:{scope_type}:{scope_id} 键，
	// 让网关能即时读取最新额度（不依赖 verify-key 缓存刷新）。
	// TTL=0 表示不设过期时间（配置类数据，仅在下次改额时被覆盖）。
	if s.rdb != nil {
		key := fmt.Sprintf("budget:amount:%s:%s", scopeType, scopeID)
		if err := s.rdb.Set(context.Background(), key, amountUSD, 0).Err(); err != nil {
			// 非致命：Redis 写失败只影响"即时生效"性能，DB 已落库
			slog.Warn("budget: redis set amount failed", "key", key, "error", err)
		}
	}
	// v0.7: 广播 auth_config_changed → 网关清空 KeyCache，下次 verify-key 拿到新 BudgetChain
	// （即使没有 Redis 写入，这一步也确保缓存的 AmountUSD 被刷新）
	if s.publisher != nil {
		s.publisher.Publish(event.AuthConfigChanged, "auth_config", uuid.Nil)
	}
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

// InitDefaultBudgets 上线初始化：为所有「没有预算配置的部门+工作室」创建默认 budget_allocations。
// 幂等：已有配置的对象跳过（由 repo.UpsertAllocation 的唯一约束保证）。
// 在 control-plane 启动时由 router 调用一次。
//
// 默认额度从参数传入（来自 config 文件），便于运维按需调整。
func (s *BudgetService) InitDefaultBudgets(
	ctx context.Context,
	defaultDeptDailyUSD, defaultDeptServiceUSD, defaultStudioUSD float64,
) (created int, err error) {
	// 1. dept_daily / dept_service：为每个部门创建默认额度
	var deptRows []struct {
		DeptID    string `gorm:"column:dept_id"`
		StudioID  string `gorm:"column:studio_id"`
		StudioName string `gorm:"column:studio_name"`
	}
	_ = s.repo.DB().Raw(`
		SELECT DISTINCT d.dept_id::text AS dept_id,
		       dsm.studio_id::text AS studio_id,
		       s.name AS studio_name
		FROM departments d
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		LEFT JOIN studios s ON s.id = dsm.studio_id
		WHERE d.dept_id IS NOT NULL
	`).Scan(&deptRows).Error

	for _, row := range deptRows {
		if row.DeptID == "" {
			continue
		}
		// dept_daily
		if existing, _ := s.repo.GetAllocation(model.BudgetScopeDeptDaily, row.DeptID); existing == nil {
			_ = s.repo.UpsertAllocation(&model.BudgetAllocation{
				ScopeType: model.BudgetScopeDeptDaily,
				ScopeID:   row.DeptID,
				AmountUSD: defaultDeptDailyUSD,
				Period:    "monthly",
				Enabled:   true,
			})
			created++
		}
		// dept_service
		if existing, _ := s.repo.GetAllocation(model.BudgetScopeDeptService, row.DeptID); existing == nil {
			_ = s.repo.UpsertAllocation(&model.BudgetAllocation{
				ScopeType: model.BudgetScopeDeptService,
				ScopeID:   row.DeptID,
				AmountUSD: defaultDeptServiceUSD,
				Period:    "monthly",
				Enabled:   true,
			})
			created++
		}
		// studio（同工作室只创建一次）
		if row.StudioID != "" {
			if existing, _ := s.repo.GetAllocation(model.BudgetScopeStudio, row.StudioID); existing == nil {
				_ = s.repo.UpsertAllocation(&model.BudgetAllocation{
					ScopeType: model.BudgetScopeStudio,
					ScopeID:   row.StudioID,
					AmountUSD: defaultStudioUSD,
					Period:    "monthly",
					Enabled:   true,
				})
				created++
			}
		}
	}
	return created, nil
}
