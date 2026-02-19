package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// BudgetService 处理预算额度的 CRUD 与告警去重记录管理。
type BudgetService struct {
	repo      BudgetAllocationRepo
	alertRepo BudgetAlertRecordRepo
	publisher EventPublisher
	rdb       *redis.Client
}

// BudgetAllocationRepo 预算额度表的访问接口。
// 实现者：repository.BudgetRepo（真实 DB）。
// 依赖注入点：测试可用 fake 替换。
type BudgetAllocationRepo interface {
	UpsertAllocation(a *model.BudgetAllocation) error
	GetAllocation(scopeType, scopeID string) (*model.BudgetAllocation, error)
	GetConsumption(scopeType, scopeID, period string) (*model.BudgetConsumption, error)
	ListScoped(scopeTypes, scopeIDs []string) ([]model.BudgetAllocation, error)
	ListMemberIDsByDeptID(deptID string) ([]string, error)
	MemberInDeptIDs(memberID string, deptIDs []string) (bool, error)
	// ListDeptIDsByStudioID 返回指定工作室下所有部门的 dept_id（admin 下钻用）。
	ListDeptIDsByStudioID(studioID string) ([]string, error)
	// ListDeptStudioRows 列出「部门 - 所属工作室」组合（用于 InitDefaultBudgets）。
	ListDeptStudioRows() ([]model.DeptStudioRow, error)
	// ResolveScopeNames 批量将 scope_id 解析为可读名称（用于管理面展示）。
	ResolveScopeNames(scopeType string, scopeIDs []string) (map[string]string, error)
	// DeleteAllocation 删除指定 scope 的单独配额。
	DeleteAllocation(scopeType, scopeID string) error
}

func (s *BudgetService) ListMemberIDsByDeptID(deptID string) ([]string, error) {
	return s.repo.ListMemberIDsByDeptID(deptID)
}

func (s *BudgetService) ListDeptIDsByStudioID(studioID string) ([]string, error) {
	return s.repo.ListDeptIDsByStudioID(studioID)
}

func (s *BudgetService) MemberInDeptIDs(memberID string, deptIDs []string) (bool, error) {
	return s.repo.MemberInDeptIDs(memberID, deptIDs)
}

// DeleteMemberBudget 删除成员的单独配额，还原为共享部门预算池。
func (s *BudgetService) DeleteMemberBudget(ctx context.Context, scopeType, scopeID string) error {
	return s.repo.DeleteAllocation(scopeType, scopeID)
}

// BudgetAlertRecordRepo 告警去重表的访问接口。
// 实现者：repository.BudgetAlertRepo（真实 DB）。
// 依赖注入点：测试可用 fake 替换。
type BudgetAlertRecordRepo interface {
	ExistsForPeriod(scopeType, scopeID, period string, threshold int) bool
	Create(a *model.BudgetAlertRecord) error
	DeleteByScope(scopeType, scopeID, period string) error
}

func NewBudgetService(repo BudgetAllocationRepo, alertRepo BudgetAlertRecordRepo) *BudgetService {
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
	ScopeType       string  `json:"scope_type"`
	ScopeID         string  `json:"scope_id"`
	// ParentScopeID 仅 scope_type=member 时填充，值为所属部门的 dept_id，
	// 供前端多部门场景路由跳转使用。
	ParentScopeID   string  `json:"parent_scope_id,omitempty"`
	Label           string  `json:"label,omitempty"`
	AmountUSD       float64 `json:"amount_usd"`
	DailyAmountUSD  float64 `json:"daily_amount_usd"`  // 日限额，-1=不限
	WeeklyAmountUSD float64 `json:"weekly_amount_usd"` // 周限额，-1=不限
	Period          string  `json:"period"`
	Enabled         bool    `json:"enabled"`
	// UsedUSD/DailyUsedUSD/WeeklyUsedUSD 从 Redis 实时读取（budget:used:{scope_type}:{scope_id}:{period}）
	UsedUSD        float64 `json:"used_usd"`
	DailyUsedUSD   float64 `json:"daily_used_usd"`
	WeeklyUsedUSD  float64 `json:"weekly_used_usd"`
	UsedRatio      float64 `json:"used_ratio"`
	UpdatedBy     string  `json:"updated_by,omitempty"`      // 最后修改人 ID
	UpdatedByName string  `json:"updated_by_name,omitempty"` // 最后修改人名称
	UpdatedAt     string  `json:"updated_at,omitempty"`      // 最后修改时间
}

// SetBudget 设置/更新某层预算额度。
// 改额后自动清理该 period 的告警去重记录，使扩容后可再次告警（§H 决策）。
func (s *BudgetService) SetBudget(ctx context.Context, scopeType, scopeID string, amountUSD, dailyAmountUSD, weeklyAmountUSD float64, createdBy uuid.UUID) error {
	// -1 表示不限额（与 daily/weekly 语义一致）；< -1 无意义拒绝
	if amountUSD < -1 {
		return fmt.Errorf("amount_usd must be >= -1 (-1 means unlimited)")
	}
	a := &model.BudgetAllocation{
		ScopeType:       scopeType,
		ScopeID:         scopeID,
		AmountUSD:       amountUSD,
		DailyAmountUSD:  dailyAmountUSD,
		WeeklyAmountUSD: weeklyAmountUSD,
		Period:          "monthly",
		Enabled:         true,
		CreatedBy:       createdBy,
		UpdatedBy:       createdBy,
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

// GetBudget 查询单层预算（控制面展示，used_usd 从 Redis 实时读取）。
func (s *BudgetService) GetBudget(ctx context.Context, scopeType, scopeID string) (*BudgetItem, error) {
	alloc, err := s.repo.GetAllocation(scopeType, scopeID)
	if err != nil {
		return nil, err
	}
	item := &BudgetItem{
		ScopeType:       scopeType,
		ScopeID:         scopeID,
		Period:          "monthly",
		Enabled:         true,
		AmountUSD:       -1, // -1 表示未配置（区别于显式设为 0 的封禁状态）
		DailyAmountUSD:  -1,
		WeeklyAmountUSD: -1,
	}
	if alloc != nil {
		item.AmountUSD = alloc.AmountUSD
		item.DailyAmountUSD = alloc.DailyAmountUSD
		item.WeeklyAmountUSD = alloc.WeeklyAmountUSD
		item.Enabled = alloc.Enabled
	}
	// 从 Redis 读实时已用量（月/周/日，与网关 BudgetSettle 同源）
	now := time.Now()
	period := now.Format("2006-01")
	item.UsedUSD = s.readUsedFromRedis(ctx, scopeType, scopeID, period)
	if item.AmountUSD > 0 {
		item.UsedRatio = item.UsedUSD / item.AmountUSD
	}
	if item.DailyAmountUSD >= 0 {
		dayPeriod := now.Format("2006-01-02")
		item.DailyUsedUSD = s.readUsedFromRedis(ctx, scopeType, scopeID, dayPeriod)
	}
	if item.WeeklyAmountUSD >= 0 {
		year, week := now.ISOWeek()
		weekPeriod := fmt.Sprintf("%d-W%02d", year, week)
		item.WeeklyUsedUSD = s.readUsedFromRedis(ctx, scopeType, scopeID, weekPeriod)
	}
	return item, nil
}

// readUsedFromRedis 从 Redis 读取某 scope 某周期的已用量（USD）。
// key 格式与网关保持一致：budget:used:{scope_type}:{scope_id}:{period}
// Redis 不可用或 key 不存在时返回 0（fail-open，不影响展示加载）。
func (s *BudgetService) readUsedFromRedis(ctx context.Context, scopeType, scopeID, period string) float64 {
	if s.rdb == nil {
		return 0
	}
	key := fmt.Sprintf("budget:used:%s:%s:%s", scopeType, scopeID, period)
	val, err := s.rdb.Get(ctx, key).Float64()
	if err != nil {
		return 0 // key 不存在（未消费）或 Redis 异常，均视为 0
	}
	return val
}

// ListScoped 按管辖范围列出预算配置（管理面用）。
func (s *BudgetService) ListScoped(ctx context.Context, scopeTypes []string, scopeIDs []string) ([]BudgetItem, error) {
	allocs, err := s.repo.ListScoped(scopeTypes, scopeIDs)
	if err != nil {
		return nil, err
	}

	// 批量解析 scope_id → 可读名称
	nameMap := s.resolveNames(allocs)

	// 解析 updated_by → 名称
	updaterIDs := make([]string, 0)
	for _, a := range allocs {
		if a.UpdatedBy != (uuid.UUID{}) {
			updaterIDs = append(updaterIDs, a.UpdatedBy.String())
		}
	}
	updaterNames, _ := s.repo.ResolveScopeNames("member", updaterIDs)

	now := time.Now()
	period := now.Format("2006-01")
	items := make([]BudgetItem, 0, len(allocs))
	for _, a := range allocs {
		item := BudgetItem{
			ScopeType:       a.ScopeType,
			ScopeID:         a.ScopeID,
			Label:           nameMap[a.ScopeType+":"+a.ScopeID],
			AmountUSD:       a.AmountUSD,
			DailyAmountUSD:  a.DailyAmountUSD,
			WeeklyAmountUSD: a.WeeklyAmountUSD,
			Period:          a.Period,
			Enabled:         a.Enabled,
		}
		if a.UpdatedBy != (uuid.UUID{}) {
			item.UpdatedBy = a.UpdatedBy.String()
			if name, ok := updaterNames[a.UpdatedBy.String()]; ok {
				item.UpdatedByName = name
			}
		}
		if !a.UpdatedAt.IsZero() {
			item.UpdatedAt = a.UpdatedAt.Format("01/02 15:04")
		}
		item.UsedUSD = s.readUsedFromRedis(ctx, a.ScopeType, a.ScopeID, period)
		if item.AmountUSD > 0 {
			item.UsedRatio = item.UsedUSD / item.AmountUSD
		}
		if item.DailyAmountUSD >= 0 {
			dayPeriod := now.Format("2006-01-02")
			item.DailyUsedUSD = s.readUsedFromRedis(ctx, a.ScopeType, a.ScopeID, dayPeriod)
		}
		if item.WeeklyAmountUSD >= 0 {
			year, week := now.ISOWeek()
			weekPeriod := fmt.Sprintf("%d-W%02d", year, week)
			item.WeeklyUsedUSD = s.readUsedFromRedis(ctx, a.ScopeType, a.ScopeID, weekPeriod)
		}
		items = append(items, item)
	}
	return items, nil
}

// resolveNames 批量将 allocs 中的 scope_id 解析为可读名称。
// 返回 map key 格式为 "scope_type:scope_id" → 名称。
func (s *BudgetService) resolveNames(allocs []model.BudgetAllocation) map[string]string {
	result := make(map[string]string)

	// 按 scope_type 分组收集 IDs
	grouped := make(map[string][]string)
	for _, a := range allocs {
		if a.ScopeType == model.BudgetScopeCompany {
			result[a.ScopeType+":"+a.ScopeID] = "全公司"
			continue
		}
		grouped[a.ScopeType] = append(grouped[a.ScopeType], a.ScopeID)
	}

	// 批量解析
	for scopeType, ids := range grouped {
		names, err := s.repo.ResolveScopeNames(scopeType, ids)
		if err != nil {
			continue
		}
		for id, name := range names {
			result[scopeType+":"+id] = name
		}
	}
	return result
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
	rows, err := s.repo.ListDeptStudioRows()
	if err != nil {
		return 0, fmt.Errorf("list dept studio rows: %w", err)
	}

	// 同 studio 只创建一次（避免重复插入）
	studioSeen := make(map[string]bool)

	for _, row := range rows {
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
		if row.StudioID != "" && !studioSeen[row.StudioID] {
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
			studioSeen[row.StudioID] = true
		}
	}
	return created, nil
}

// ─── Redis 计数器重建 ────────────────────────────────────────────────────────

// RebuildResult 重建结果摘要。
type RebuildResult struct {
	KeysWritten int    `json:"keys_written"`
	MonthPeriod string `json:"month_period"`
	WeekPeriod  string `json:"week_period"`
	DayPeriod   string `json:"day_period"`
}

type callCostRow struct {
	ScopeID string
	KeyType string
	CostUSD float64
}

// RebuildCounters 从 call_logs 重算各 scope 当前月/周/日已用量，写回 Redis。
// 适用场景：Redis 数据丢失后恢复，防止预算误认为 $0 而被重复消费。
func (s *BudgetService) RebuildCounters(ctx context.Context, db *gorm.DB) (*RebuildResult, error) {
	if s.rdb == nil {
		return nil, fmt.Errorf("redis 未配置，无法重建计数器")
	}

	now := time.Now()
	month := now.Format("2006-01")
	year, week := now.ISOWeek()
	weekPeriod := fmt.Sprintf("%d-W%02d", year, week)
	day := now.Format("2006-01-02")

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	weekdayOffset := (int(now.Weekday()) + 6) % 7 // ISO: Mon=0
	weekStart := time.Date(now.Year(), now.Month(), now.Day()-weekdayOffset, 0, 0, 0, 0, now.Location())
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	type periodDef struct {
		period string
		since  time.Time
		ttl    time.Duration
	}
	periods := []periodDef{
		{month, monthStart, 70 * 24 * time.Hour},
		{weekPeriod, weekStart, 14 * 24 * time.Hour},
		{day, dayStart, 3 * 24 * time.Hour},
	}

	keysWritten := 0

	for _, p := range periods {
		// ── company ──────────────────────────────────────────────────────────
		var companyTotal float64
		if err := db.WithContext(ctx).Table("call_logs").
			Select("COALESCE(SUM(est_cost_usd), 0)").
			Where("ts >= ? AND status = 200", p.since).
			Scan(&companyTotal).Error; err != nil {
			slog.Warn("rebuild: query company total failed, skipping period", "period", p.period, "err", err)
			continue
		}
		if err := s.setRedisCounter(ctx, model.BudgetScopeCompany, model.BudgetScopeGlobal, p.period, companyTotal, p.ttl); err != nil {
			slog.Warn("rebuild: set company counter", "err", err)
		} else {
			keysWritten++
		}

		// ── studio ────────────────────────────────────────────────────────────
		var studioRows []callCostRow
		if err := db.WithContext(ctx).Table("call_logs").
			Select("studio_id AS scope_id, '' AS key_type, COALESCE(SUM(est_cost_usd), 0) AS cost_usd").
			Where("ts >= ? AND studio_id != '' AND status = 200", p.since).
			Group("studio_id").
			Scan(&studioRows).Error; err != nil {
			slog.Warn("rebuild: query studio rows failed", "period", p.period, "err", err)
		}
		for _, r := range studioRows {
			if err := s.setRedisCounter(ctx, model.BudgetScopeStudio, r.ScopeID, p.period, r.CostUSD, p.ttl); err != nil {
				slog.Warn("rebuild: set studio counter", "scope_id", r.ScopeID, "err", err)
			} else {
				keysWritten++
			}
		}

		// ── dept（区分 personal/service key）────────────────────────────────
		var deptRows []callCostRow
		if err := db.WithContext(ctx).Table("call_logs cl").
			Select("cl.dept_id AS scope_id, COALESCE(NULLIF(cl.key_type, ''), 'personal') AS key_type, COALESCE(SUM(cl.est_cost_usd), 0) AS cost_usd").
			Where("cl.ts >= ? AND cl.dept_id != '' AND cl.status = 200", p.since).
			Group("cl.dept_id, cl.key_type").
			Scan(&deptRows).Error; err != nil {
			slog.Warn("rebuild: query dept rows failed", "period", p.period, "err", err)
		}
		for _, r := range deptRows {
			scopeType := model.BudgetScopeDeptDaily
			if r.KeyType == model.KeyTypeService {
				scopeType = model.BudgetScopeDeptService
			}
			if err := s.setRedisCounter(ctx, scopeType, r.ScopeID, p.period, r.CostUSD, p.ttl); err != nil {
				slog.Warn("rebuild: set dept counter", "scope_type", scopeType, "scope_id", r.ScopeID, "err", err)
			} else {
				keysWritten++
			}
		}

		// ── member ────────────────────────────────────────────────────────────
		var memberRows []callCostRow
		if err := db.WithContext(ctx).Table("call_logs cl").
			Select("cl.user_id AS scope_id, '' AS key_type, COALESCE(SUM(cl.est_cost_usd), 0) AS cost_usd").
			Where("cl.ts >= ? AND cl.user_id != '' AND cl.status = 200 AND COALESCE(NULLIF(cl.key_type, ''), 'personal') != ?", p.since, model.KeyTypeService).
			Group("cl.user_id").
			Scan(&memberRows).Error; err != nil {
			slog.Warn("rebuild: query member rows failed", "period", p.period, "err", err)
		}
		for _, r := range memberRows {
			if err := s.setRedisCounter(ctx, model.BudgetScopeMember, r.ScopeID, p.period, r.CostUSD, p.ttl); err != nil {
				slog.Warn("rebuild: set member counter", "scope_id", r.ScopeID, "err", err)
			} else {
				keysWritten++
			}
		}
	}

	slog.Info("budget counters rebuilt", "keys_written", keysWritten)
	return &RebuildResult{
		KeysWritten: keysWritten,
		MonthPeriod: month,
		WeekPeriod:  weekPeriod,
		DayPeriod:   day,
	}, nil
}

func (s *BudgetService) setRedisCounter(ctx context.Context, scopeType, scopeID, period string, value float64, ttl time.Duration) error {
	key := fmt.Sprintf("budget:used:%s:%s:%s", scopeType, scopeID, period)
	return s.rdb.SetEx(ctx, key, value, ttl).Err()
}
