package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// BudgetService 处理预算额度的 CRUD 与告警去重记录管理。
type BudgetService struct {
	repo      BudgetAllocationRepo
	alertRepo BudgetAlertRecordRepo
	publisher EventPublisher
	rdb       *redis.Client
	scheduler interface {
		PutPendingSchedule(context.Context, *model.BudgetLimitSchedule, bool) (*model.BudgetLimitSchedule, error)
	}
}

type BudgetRiskConfirmation struct {
	ConfirmBelowUsed       bool   `json:"confirm_below_used"`
	ConfirmZeroBlock       bool   `json:"confirm_zero_block"`
	ConfirmStale           bool   `json:"confirm_stale"`
	ConfirmReplaceSchedule bool   `json:"confirm_replace_schedule"`
	PreviewVersion         string `json:"preview_version"`
}

type BudgetAdjustmentInput struct {
	ScopeType      string
	ScopeID        string
	PeriodType     budgetdomain.PeriodType
	TargetLimitUSD budgetdomain.Amount
	EffectiveMode  EffectiveMode
	Reason         string
	ActorID        uuid.UUID
	PreviewOnly    bool
	BudgetRiskConfirmation
}

type BudgetAdjustmentImpact struct {
	BeforeLimitUSD          string          `json:"before_limit_usd"`
	AfterLimitUSD           string          `json:"after_limit_usd"`
	CurrentUsedUSD          string          `json:"current_used_usd"`
	AffectedMembers         int             `json:"affected_members"`
	AffectedGroups          []AffectedGroup `json:"affected_groups"`
	WillBlock               bool            `json:"will_block"`
	StreamPolicy            string          `json:"stream_policy"`
	DataAsOf                time.Time       `json:"data_as_of"`
	Stale                   bool            `json:"stale"`
	BelowUsed               bool            `json:"below_used"`
	ZeroBlock               bool            `json:"zero_block"`
	ReplacesPendingSchedule bool            `json:"replaces_pending_schedule"`
	PendingScheduleID       string          `json:"pending_schedule_id,omitempty"`
	PendingScheduleVersion  string          `json:"pending_schedule_version,omitempty"`
	PreviewVersion          string          `json:"preview_version"`
}

func (s *BudgetService) SetLifecycleScheduler(scheduler interface {
	PutPendingSchedule(context.Context, *model.BudgetLimitSchedule, bool) (*model.BudgetLimitSchedule, error)
}) {
	s.scheduler = scheduler
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

type budgetAllocationCASRepo interface {
	CompareAndSwapAllocationLimits(context.Context, model.BudgetAllocation, float64, float64, float64, uuid.UUID) (bool, error)
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

func (s *BudgetService) ResolveMemberDeptID(memberID string) (string, error) {
	resolver, ok := s.repo.(interface {
		ResolveMemberDeptID(string) (string, error)
	})
	if !ok {
		return "", nil
	}
	return resolver.ResolveMemberDeptID(memberID)
}

// MapMemberDeptIDs 批量解析成员 → SSO dept_id。
func (s *BudgetService) MapMemberDeptIDs(memberIDs []string) (map[string]string, error) {
	mapper, ok := s.repo.(interface {
		MapMemberDeptIDs([]string) (map[string]string, error)
	})
	if !ok {
		out := make(map[string]string, len(memberIDs))
		for _, id := range memberIDs {
			deptID, err := s.ResolveMemberDeptID(id)
			if err != nil {
				return nil, err
			}
			if deptID != "" {
				out[id] = deptID
			}
		}
		return out, nil
	}
	return mapper.MapMemberDeptIDs(memberIDs)
}

// ResolveUserNames 批量将 user_id 解析为姓名（审批通知展示申请人/审批人用）。
// repo 不支持时返回空 map，调用方应容忍并回退。
func (s *BudgetService) ResolveUserNames(userIDs []string) (map[string]string, error) {
	resolver, ok := s.repo.(interface {
		ResolveUserNames([]string) (map[string]string, error)
	})
	if !ok {
		return map[string]string{}, nil
	}
	return resolver.ResolveUserNames(userIDs)
}

func (s *BudgetService) ResolveMyBudgetChain(ctx context.Context, userID string) ([]repository.MyBudgetScopeAllocation, error) {
	reader, ok := s.repo.(interface {
		ResolveMyBudgetChain(context.Context, string) ([]repository.MyBudgetScopeAllocation, error)
	})
	if !ok {
		return nil, fmt.Errorf("budget chain resolver not configured")
	}
	return reader.ResolveMyBudgetChain(ctx, userID)
}

func (s *BudgetService) ResolveBudgetChain(ctx context.Context, scopeType, scopeID string) ([]repository.MyBudgetScopeAllocation, error) {
	reader, ok := s.repo.(interface {
		ResolveBudgetChain(context.Context, string, string) ([]repository.MyBudgetScopeAllocation, error)
	})
	if !ok {
		return nil, fmt.Errorf("budget chain resolver not configured")
	}
	return reader.ResolveBudgetChain(ctx, scopeType, scopeID)
}

// DeleteMemberBudget 删除成员的单独配额，还原为共享部门预算池。
func (s *BudgetService) DeleteMemberBudget(ctx context.Context, scopeType, scopeID string) error {
	return s.repo.DeleteAllocation(scopeType, scopeID)
}

// BudgetAlertRecordRepo 告警去重表的访问接口。
// 实现者：repository.BudgetAlertRepo（真实 DB）。
// 依赖注入点：测试可用 fake 替换。
type BudgetAlertRecordRepo interface {
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

func (s *BudgetService) AdjustBudget(
	ctx context.Context,
	input BudgetAdjustmentInput,
) (*BudgetAdjustmentImpact, error) {
	if strings.TrimSpace(input.Reason) == "" {
		return nil, ErrBudgetReasonRequired
	}
	if !input.PeriodType.Valid() {
		return nil, ErrInvalidBudgetPeriod
	}
	allocation, err := s.repo.GetAllocation(input.ScopeType, input.ScopeID)
	if err != nil {
		return nil, err
	}
	creating := allocation == nil
	if creating {
		// 首次为成员等 scope 配置独立额度：允许立即生效创建；下周期排期需要已有行。
		if input.EffectiveMode == EffectiveNextPeriod {
			return nil, ErrNextPeriodRequiresAllocation
		}
		allocation = &model.BudgetAllocation{
			ScopeType:       input.ScopeType,
			ScopeID:         input.ScopeID,
			AmountUSD:       -1,
			DailyAmountUSD:  -1,
			WeeklyAmountUSD: -1,
			Period:          "monthly",
			Enabled:         true,
			CreatedBy:       input.ActorID,
			UpdatedBy:       input.ActorID,
		}
	}
	before := totalAllocationLimit(*allocation, input.PeriodType)
	window, err := budgetperiod.Resolve(time.Now(), input.PeriodType, budgetperiod.Config{
		Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
	})
	if err != nil {
		return nil, err
	}
	used := decimal.Zero
	stale := s.rdb == nil
	if s.rdb != nil {
		key := fmt.Sprintf("budget:v2:used:%s:%s:%s", input.ScopeType, input.ScopeID, window.Key)
		value, readErr := s.rdb.Get(ctx, key).Int64()
		switch {
		case readErr == nil:
			used, stale = decimal.New(value, -6), false
		case errors.Is(readErr, redis.Nil):
			stale = false
		default:
			stale = true
		}
	}
	target := input.TargetLimitUSD.Decimal
	unlimited := input.TargetLimitUSD.IsUnlimited()
	if unlimited && input.EffectiveMode == EffectiveNextPeriod {
		// 排期表 Amount 不允许 -1；不限仅支持立即生效
		return nil, fmt.Errorf("unlimited (-1) only supports immediate effective mode")
	}
	impact := &BudgetAdjustmentImpact{
		BeforeLimitUSD: before.Round(6).StringFixed(6),
		AfterLimitUSD:  target.Round(6).StringFixed(6),
		CurrentUsedUSD: used.Round(6).StringFixed(6),
		AffectedGroups: []AffectedGroup{}, WillBlock: !unlimited && target.LessThanOrEqual(used),
		StreamPolicy: "进行中的流式请求不中断，完成后记账并阻断后续请求",
		DataAsOf:     time.Now().UTC(), Stale: stale,
		BelowUsed: !unlimited && target.LessThan(used), ZeroBlock: !unlimited && target.IsZero(),
	}
	if input.EffectiveMode == EffectiveNextPeriod {
		finder, ok := s.scheduler.(interface {
			FindPendingSchedule(context.Context, uuid.UUID, string, budgetdomain.PeriodType) (*model.BudgetLimitSchedule, error)
		})
		if !ok {
			return nil, ErrInvalidEffectiveMode
		}
		pending, findErr := finder.FindPendingSchedule(ctx, allocation.ID, "total", input.PeriodType)
		if findErr != nil {
			return nil, findErr
		}
		if pending != nil {
			impact.ReplacesPendingSchedule = true
			impact.PendingScheduleID = pending.ID.String()
			impact.PendingScheduleVersion = pending.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	if projector, ok := s.repo.(interface {
		ResolveBudgetImpact(context.Context, string, string, *string, int) (int, []repository.BudgetAffectedGroup, error)
	}); ok {
		count, groups, projectErr := projector.ResolveBudgetImpact(ctx, input.ScopeType, input.ScopeID, nil, 20)
		if projectErr != nil {
			return nil, projectErr
		}
		impact.AffectedMembers = count
		for _, group := range groups {
			impact.AffectedGroups = append(impact.AffectedGroups, AffectedGroup{ID: group.ID, Name: group.Name})
		}
	} else {
		return nil, fmt.Errorf("budget impact projector not configured")
	}
	versionPayload := strings.Join([]string{
		input.ScopeType, input.ScopeID, string(input.PeriodType),
		before.Round(6).StringFixed(6), used.Round(6).StringFixed(6),
		input.TargetLimitUSD.String(), string(input.EffectiveMode),
		impact.PendingScheduleID, impact.PendingScheduleVersion,
	}, "|")
	versionSum := sha256.Sum256([]byte(versionPayload))
	impact.PreviewVersion = hex.EncodeToString(versionSum[:])
	if input.PreviewOnly {
		return impact, nil
	}
	if input.PreviewVersion == "" || input.PreviewVersion != impact.PreviewVersion {
		return nil, ErrBudgetPreviewStale
	}
	if stale {
		return nil, ErrBudgetUsageUnavailable
	}
	if !unlimited && target.IsZero() && !input.ConfirmZeroBlock {
		return nil, ErrZeroBlockConfirmationRequired
	}
	if !unlimited && target.LessThan(used) && !input.ConfirmBelowUsed {
		return nil, ErrBelowUsedConfirmationRequired
	}
	if impact.ReplacesPendingSchedule && !input.ConfirmReplaceSchedule {
		return nil, ErrPendingScheduleExists
	}
	switch input.EffectiveMode {
	case EffectiveImmediate:
		monthly, daily, weekly := allocation.AmountUSD, allocation.DailyAmountUSD, allocation.WeeklyAmountUSD
		value, _ := target.Float64()
		switch input.PeriodType {
		case budgetdomain.PeriodDaily:
			daily = value
		case budgetdomain.PeriodWeekly:
			weekly = value
		default:
			monthly = value
		}
		if creating {
			if err := s.SetBudget(ctx, input.ScopeType, input.ScopeID, monthly, daily, weekly, input.ActorID); err != nil {
				return nil, err
			}
		} else if casRepo, ok := s.repo.(budgetAllocationCASRepo); ok {
			updated, updateErr := casRepo.CompareAndSwapAllocationLimits(
				ctx, *allocation, monthly, daily, weekly, input.ActorID,
			)
			if updateErr != nil {
				return nil, updateErr
			}
			if !updated {
				return nil, ErrBudgetPreviewStale
			}
			s.afterBudgetWrite(input.ScopeType, input.ScopeID, monthly)
		} else if err := s.SetBudget(ctx, input.ScopeType, input.ScopeID, monthly, daily, weekly, input.ActorID); err != nil {
			return nil, err
		}
	case EffectiveNextPeriod:
		if s.scheduler == nil {
			return nil, ErrInvalidEffectiveMode
		}
		schedule := &model.BudgetLimitSchedule{
			ID: uuid.New(), BudgetAllocationID: allocation.ID,
			DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
			PeriodType: input.PeriodType, TargetLimitUSD: input.TargetLimitUSD,
			ScheduleOperation: budgetdomain.ScheduleOperationSet,
			EffectiveAt:       window.ResetAt, Reason: input.Reason, CreatedBy: input.ActorID,
			Status: budgetdomain.SchedulePending,
		}
		if _, err := s.scheduler.PutPendingSchedule(ctx, schedule, input.ConfirmReplaceSchedule); err != nil {
			if errors.Is(err, repository.ErrPendingScheduleExists) {
				return nil, ErrPendingScheduleExists
			}
			return nil, err
		}
	default:
		return nil, ErrInvalidEffectiveMode
	}
	return impact, nil
}

// MultiPeriodAdjustmentInput 多周期批量调整输入，一次调用处理 monthly/weekly/daily。
type MultiPeriodAdjustmentInput struct {
	ScopeType     string
	ScopeID       string
	ActorID       uuid.UUID
	Reason        string
	EffectiveMode EffectiveMode
	PreviewOnly   bool
	// 各周期的目标额度，nil 表示本次不修改该周期。
	Monthly *MultiPeriodItem
	Weekly  *MultiPeriodItem
	Daily   *MultiPeriodItem
}

// MultiPeriodItem 单个周期的调整参数。
type MultiPeriodItem struct {
	TargetLimitUSD budgetdomain.Amount
	BudgetRiskConfirmation
}

// MultiPeriodAdjustmentResult 每个周期的 impact，nil 表示未修改。
type MultiPeriodAdjustmentResult struct {
	Monthly *BudgetAdjustmentImpact `json:"monthly,omitempty"`
	Weekly  *BudgetAdjustmentImpact `json:"weekly,omitempty"`
	Daily   *BudgetAdjustmentImpact `json:"daily,omitempty"`
}

// AdjustBudgetMultiPeriod 批量调整多个周期的预算额度。
// preview_only=true 时返回所有周期的预览信息，不写库。
// preview_only=false 时依次 apply，任意周期失败则立即返回错误（已成功的周期不回滚，
// 因为 repo 层不支持跨周期事务；handler 层应在出错时提示用户重试）。
func (s *BudgetService) AdjustBudgetMultiPeriod(
	ctx context.Context,
	input MultiPeriodAdjustmentInput,
) (*MultiPeriodAdjustmentResult, error) {
	result := &MultiPeriodAdjustmentResult{}
	periods := []struct {
		period budgetdomain.PeriodType
		item   *MultiPeriodItem
		out    **BudgetAdjustmentImpact
	}{
		{budgetdomain.PeriodMonthly, input.Monthly, &result.Monthly},
		{budgetdomain.PeriodWeekly, input.Weekly, &result.Weekly},
		{budgetdomain.PeriodDaily, input.Daily, &result.Daily},
	}
	for _, p := range periods {
		if p.item == nil {
			continue
		}
		impact, err := s.AdjustBudget(ctx, BudgetAdjustmentInput{
			ScopeType:              input.ScopeType,
			ScopeID:                input.ScopeID,
			PeriodType:             p.period,
			TargetLimitUSD:         p.item.TargetLimitUSD,
			EffectiveMode:          input.EffectiveMode,
			Reason:                 input.Reason,
			ActorID:                input.ActorID,
			PreviewOnly:            input.PreviewOnly,
			BudgetRiskConfirmation: p.item.BudgetRiskConfirmation,
		})
		if err != nil {
			return nil, fmt.Errorf("period %s: %w", p.period, err)
		}
		*p.out = impact
	}
	return result, nil
}

// BudgetItem 预算展示项（含实时 used_usd 占位，网关侧从 Redis 读）。
type BudgetItem struct {
	AllocationID string `json:"allocation_id,omitempty"`
	ScopeType    string `json:"scope_type"`
	ScopeID      string `json:"scope_id"`
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
	UsedUSD       float64 `json:"used_usd"`
	DailyUsedUSD  float64 `json:"daily_used_usd"`
	WeeklyUsedUSD float64 `json:"weekly_used_usd"`
	UsedRatio     float64 `json:"used_ratio"`
	UpdatedBy     string  `json:"updated_by,omitempty"`      // 最后修改人 ID
	UpdatedByName string  `json:"updated_by_name,omitempty"` // 最后修改人名称
	UpdatedAt     string  `json:"updated_at,omitempty"`      // 最后修改时间
}

// SetBudget 设置/更新某层预算额度。
// 改额后自动清理该 period 的告警去重记录，使扩容后可再次告警（§H 决策）。
func (s *BudgetService) SetBudget(ctx context.Context, scopeType, scopeID string, amountUSD, dailyAmountUSD, weeklyAmountUSD float64, createdBy uuid.UUID) error {
	// -1 表示不限额（与 daily/weekly 语义一致）；< -1 无意义拒绝
	if amountUSD < -1 {
		return fmt.Errorf("amount_usd must be >= 0 or exactly -1 (unlimited)")
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
	s.afterBudgetWrite(scopeType, scopeID, amountUSD)
	return nil
}

func (s *BudgetService) afterBudgetWrite(scopeType, scopeID string, amountUSD float64) {
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
		item.AllocationID = alloc.ID.String()
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
// 与网关强制执行口径保持一致：读 v2 计数（budget:v2:used:{scope_type}:{scope_id}:{period}），
// 其值以 microUSD 存储，读回后换算为 USD。
// 说明：legacy 计数（budget:used:*）已不再作为展示事实源，
// 以避免仪表盘「已用/百分比」与网关真实拦截数分叉（内容技术部个人 Key 池即因二者分叉而误报 95%）。
// Redis 不可用或 key 不存在时返回 0（fail-open，不影响展示加载）。
func (s *BudgetService) readUsedFromRedis(ctx context.Context, scopeType, scopeID, period string) float64 {
	if s.rdb == nil {
		return 0
	}
	key := fmt.Sprintf("budget:v2:used:%s:%s:%s", scopeType, scopeID, period)
	micro, err := s.rdb.Get(ctx, key).Int64()
	if err != nil {
		return 0 // key 不存在（未消费）或 Redis 异常，均视为 0
	}
	return float64(micro) / 1_000_000
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
			AllocationID:    a.ID.String(),
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
