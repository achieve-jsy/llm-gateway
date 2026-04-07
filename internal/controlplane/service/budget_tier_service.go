package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

var (
	ErrBudgetAllocationNotFound      = errors.New("budget allocation not found")
	ErrInvalidBudgetPeriod           = errors.New("invalid budget period")
	ErrPendingScheduleExists         = errors.New("pending budget schedule exists")
	ErrBudgetReasonRequired          = errors.New("budget change reason is required")
	ErrBudgetForbidden               = errors.New("budget operation forbidden")
	ErrInvalidEffectiveMode          = errors.New("invalid budget effective mode")
	ErrBelowUsedConfirmationRequired = errors.New("confirm_below_used is required")
	ErrZeroBlockConfirmationRequired = errors.New("confirm_zero_block is required")
	ErrBudgetPreviewStale            = errors.New("budget adjustment preview is stale")
)

type EffectiveMode string

const (
	EffectiveImmediate  EffectiveMode = "immediate"
	EffectiveNextPeriod EffectiveMode = "next_period"
)

type BudgetTierStore interface {
	ListTierLimits(context.Context, uuid.UUID) ([]model.BudgetTierLimit, error)
	FindTierLimit(context.Context, uuid.UUID, uuid.UUID, budgetdomain.PeriodType) (*model.BudgetTierLimit, error)
	UpsertTierLimit(context.Context, repository.UpsertTierLimitParams) (*model.BudgetTierLimit, error)
	ListActiveOverrides(context.Context, uuid.UUID, time.Time) ([]model.BudgetTemporaryOverride, error)
	FindPendingSchedule(context.Context, uuid.UUID, string, budgetdomain.PeriodType) (*model.BudgetLimitSchedule, error)
	PutPendingSchedule(context.Context, *model.BudgetLimitSchedule, bool) (*model.BudgetLimitSchedule, error)
}

type BudgetAllocationReader interface {
	GetAllocationByID(context.Context, uuid.UUID) (*model.BudgetAllocation, error)
}

type pendingTierScheduleLister interface {
	ListPendingSchedules(context.Context, uuid.UUID) ([]model.BudgetLimitSchedule, error)
}

type SetTierLimitInput struct {
	AllocationID           uuid.UUID
	TierID                 uuid.UUID
	PeriodType             budgetdomain.PeriodType
	LimitUSD               budgetdomain.Amount
	Enabled                bool
	EffectiveMode          EffectiveMode
	Reason                 string
	ActorID                uuid.UUID
	ConfirmReplaceSchedule bool
	ConfirmBelowUsed       bool
	ConfirmZeroBlock       bool
}

type LimitSourceSummary struct {
	SourceType      string     `json:"source_type"`
	TemporaryActive bool       `json:"temporary_active"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

type ScheduleView struct {
	ID             uuid.UUID                   `json:"id"`
	TargetLimitUSD budgetdomain.Amount         `json:"target_limit_usd"`
	EffectiveAt    time.Time                   `json:"effective_at"`
	Status         budgetdomain.ScheduleStatus `json:"status"`
}

type TierLimitView struct {
	ID                 uuid.UUID               `json:"id"`
	BudgetAllocationID uuid.UUID               `json:"budget_allocation_id"`
	ModelTierID        uuid.UUID               `json:"model_tier_id"`
	ModelTierName      string                  `json:"model_tier_name"`
	PeriodType         budgetdomain.PeriodType `json:"period_type"`
	BaseLimitUSD       budgetdomain.Amount     `json:"base_limit_usd"`
	TemporaryTargetUSD *budgetdomain.Amount    `json:"temporary_target_usd,omitempty"`
	EffectiveLimitUSD  budgetdomain.Amount     `json:"effective_limit_usd"`
	SourceSummary      LimitSourceSummary      `json:"source_summary"`
	Enabled            bool                    `json:"enabled"`
	EffectiveAt        time.Time               `json:"effective_at"`
	OverrideExpiresAt  *time.Time              `json:"override_expires_at,omitempty"`
	PendingSchedule    *ScheduleView           `json:"pending_schedule,omitempty"`
}

type BudgetTierService struct {
	repo              BudgetTierStore
	allocations       BudgetAllocationReader
	tiers             ModelTierRepository
	clock             func() time.Time
	periodConfig      budgetperiod.Config
	previewMu         sync.Mutex
	previews          map[string]batchPreviewState
	previewCacheMax   int
	previewCacheTTL   time.Duration
	usageRepo         TierUsageRepository
	snapshotPublisher BudgetConfigPublisher
	rdb               *redis.Client
}

func (s *BudgetTierService) WithRedis(client *redis.Client) *BudgetTierService {
	s.rdb = client
	return s
}

func (s *BudgetTierService) WithSnapshotPublisher(p BudgetConfigPublisher) *BudgetTierService {
	s.snapshotPublisher = p
	return s
}

func (s *BudgetTierService) publishBudgetSnapshot(ctx context.Context, allocationID uuid.UUID) error {
	if s.snapshotPublisher != nil {
		if err := s.snapshotPublisher.MarkDirtyAndPublish(ctx); err != nil {
			slog.ErrorContext(ctx, "budget snapshot publish failed after tier limit write",
				"budget_allocation_id", allocationID, "error", err)
			return fmt.Errorf("%w: %v", ErrBudgetConfigSyncPending, err)
		}
	}
	return nil
}

func NewBudgetTierService(repo BudgetTierStore, allocations BudgetAllocationReader, tiers ModelTierRepository, clock func() time.Time) *BudgetTierService {
	if clock == nil {
		clock = time.Now
	}
	return &BudgetTierService{
		repo: repo, allocations: allocations, tiers: tiers, clock: clock,
		periodConfig: budgetperiod.Config{Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart},
		previews:     make(map[string]batchPreviewState), previewCacheMax: 1000, previewCacheTTL: 5 * time.Minute,
	}
}

func (s *BudgetTierService) WithPeriodConfig(config budgetperiod.Config) *BudgetTierService {
	if config.Timezone != "" {
		s.periodConfig.Timezone = config.Timezone
	}
	if config.WeekStart != "" {
		s.periodConfig.WeekStart = config.WeekStart
	}
	return s
}

func (s *BudgetTierService) ListTierLimits(ctx context.Context, allocationID uuid.UUID) ([]TierLimitView, error) {
	if _, err := s.getAllocation(ctx, allocationID); err != nil {
		return nil, err
	}
	limits, err := s.repo.ListTierLimits(ctx, allocationID)
	if err != nil {
		return nil, mapBudgetRepositoryError(err)
	}
	now := s.clock()
	overrides, err := s.repo.ListActiveOverrides(ctx, allocationID, now)
	if err != nil {
		return nil, mapBudgetRepositoryError(err)
	}
	views := make([]TierLimitView, 0, len(limits))
	pendingByKey := make(map[string]*model.BudgetLimitSchedule)
	if lister, ok := s.repo.(pendingTierScheduleLister); ok {
		schedules, listErr := lister.ListPendingSchedules(ctx, allocationID)
		if listErr != nil {
			return nil, mapBudgetRepositoryError(listErr)
		}
		for index := range schedules {
			schedule := &schedules[index]
			pendingByKey[schedule.DimensionKey+":"+string(schedule.PeriodType)] = schedule
		}
	}
	for _, limit := range limits {
		view := buildTierLimitView(limit, overrides)
		key := "tier:" + limit.ModelTierID.String() + ":" + string(limit.PeriodType)
		pending := pendingByKey[key]
		if _, batched := s.repo.(pendingTierScheduleLister); !batched {
			pending, err = s.repo.FindPendingSchedule(ctx, allocationID, "tier:"+limit.ModelTierID.String(), limit.PeriodType)
			if err != nil {
				return nil, mapBudgetRepositoryError(err)
			}
		}
		if pending != nil {
			view.PendingSchedule = &ScheduleView{ID: pending.ID, TargetLimitUSD: pending.TargetLimitUSD, EffectiveAt: pending.EffectiveAt, Status: pending.Status}
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *BudgetTierService) SetTierLimit(ctx context.Context, input SetTierLimitInput) (*TierLimitView, error) {
	return s.setTierLimit(ctx, input, true)
}

func (s *BudgetTierService) setTierLimit(ctx context.Context, input SetTierLimitInput, publish bool) (*TierLimitView, error) {
	if strings.TrimSpace(input.Reason) == "" {
		return nil, ErrBudgetReasonRequired
	}
	if !input.PeriodType.Valid() {
		return nil, ErrInvalidBudgetPeriod
	}
	if _, err := s.getAllocation(ctx, input.AllocationID); err != nil {
		return nil, err
	}
	tier, err := s.tiers.FindByID(ctx, input.TierID)
	if err != nil || tier == nil || tier.ArchivedAt != nil {
		return nil, ErrModelTierNotFound
	}
	if !tier.Enabled {
		return nil, ErrModelTierDisabled
	}
	now := s.clock()
	used, stale := s.currentTierUsage(ctx, input, now)
	if stale && s.rdb != nil {
		return nil, ErrBudgetUsageUnavailable
	}
	if input.LimitUSD.Decimal.IsZero() && !input.ConfirmZeroBlock {
		return nil, ErrZeroBlockConfirmationRequired
	}
	if input.LimitUSD.Decimal.LessThan(used.Decimal) && !input.ConfirmBelowUsed {
		return nil, ErrBelowUsedConfirmationRequired
	}
	switch input.EffectiveMode {
	case EffectiveImmediate:
		limit, err := s.repo.UpsertTierLimit(ctx, repository.UpsertTierLimitParams{
			BudgetAllocationID: input.AllocationID, ModelTierID: input.TierID,
			PeriodType: input.PeriodType, LimitUSD: input.LimitUSD, EffectiveAt: now, Enabled: input.Enabled,
		})
		if err != nil {
			return nil, mapBudgetRepositoryError(err)
		}
		limit.ModelTier = *tier
		overrides, err := s.repo.ListActiveOverrides(ctx, input.AllocationID, now)
		if err != nil {
			return nil, mapBudgetRepositoryError(err)
		}
		view := buildTierLimitView(*limit, overrides)
		if publish {
			return &view, s.publishBudgetSnapshot(ctx, input.AllocationID)
		}
		return &view, nil
	case EffectiveNextPeriod:
		if input.Reason == "" {
			return nil, ErrBudgetReasonRequired
		}
		key := "tier:" + input.TierID.String()
		period, err := budgetperiod.Resolve(now, input.PeriodType, s.periodConfig)
		if err != nil {
			return nil, err
		}
		schedule := &model.BudgetLimitSchedule{
			ID: uuid.New(), BudgetAllocationID: input.AllocationID,
			DimensionType: budgetdomain.DimensionTier, DimensionKey: key, ModelTierID: &input.TierID,
			PeriodType: input.PeriodType, TargetLimitUSD: input.LimitUSD,
			EffectiveAt: period.ResetAt, Reason: input.Reason, CreatedBy: input.ActorID,
			Status: budgetdomain.SchedulePending,
		}
		if _, err := s.repo.PutPendingSchedule(ctx, schedule, input.ConfirmReplaceSchedule); err != nil {
			return nil, mapBudgetRepositoryError(err)
		}
		existing, err := s.repo.FindTierLimit(ctx, input.AllocationID, input.TierID, input.PeriodType)
		if err != nil {
			return nil, mapBudgetRepositoryError(err)
		}
		if existing == nil {
			existing = &model.BudgetTierLimit{
				BudgetAllocationID: input.AllocationID, ModelTierID: input.TierID,
				PeriodType: input.PeriodType, LimitUSD: input.LimitUSD, EffectiveAt: period.ResetAt,
			}
		}
		existing.ModelTier = *tier
		view := buildTierLimitView(*existing, nil)
		view.PendingSchedule = &ScheduleView{ID: schedule.ID, TargetLimitUSD: schedule.TargetLimitUSD, EffectiveAt: schedule.EffectiveAt, Status: schedule.Status}
		if publish {
			return &view, s.publishBudgetSnapshot(ctx, input.AllocationID)
		}
		return &view, nil
	default:
		return nil, ErrInvalidEffectiveMode
	}
}

func (s *BudgetTierService) currentTierUsage(
	ctx context.Context,
	input SetTierLimitInput,
	now time.Time,
) (budgetdomain.Amount, bool) {
	if s.rdb == nil {
		return budgetdomain.Amount{}, true
	}
	window, err := budgetperiod.Resolve(now, input.PeriodType, s.periodConfig)
	if err != nil {
		return budgetdomain.Amount{}, true
	}
	key := fmt.Sprintf(
		"budget:v2:tier:used:%s:%s:%s:%s",
		"allocation", input.AllocationID.String(), input.TierID.String(), window.Key,
	)
	// Snapshot keys are scope-based. Allocation-only lookup is unavailable here,
	// so resolve the real scope before reading Redis.
	allocation, err := s.allocations.GetAllocationByID(ctx, input.AllocationID)
	if err != nil || allocation == nil {
		return budgetdomain.Amount{}, true
	}
	key = fmt.Sprintf("budget:v2:tier:used:%s:%s:%s:%s",
		allocation.ScopeType, allocation.ScopeID, input.TierID, window.Key)
	value, err := s.rdb.Get(ctx, key).Int64()
	if errors.Is(err, redis.Nil) {
		return budgetdomain.Amount{}, false
	}
	if err != nil {
		return budgetdomain.Amount{}, true
	}
	return budgetdomain.Amount{Decimal: decimal.New(value, -6)}, false
}

func (s *BudgetTierService) getAllocation(ctx context.Context, id uuid.UUID) (*model.BudgetAllocation, error) {
	allocation, err := s.allocations.GetAllocationByID(ctx, id)
	if errors.Is(err, repository.ErrForbidden) {
		return nil, ErrBudgetForbidden
	}
	if err != nil || allocation == nil {
		return nil, ErrBudgetAllocationNotFound
	}
	return allocation, nil
}

func mapBudgetRepositoryError(err error) error {
	if errors.Is(err, repository.ErrForbidden) {
		return ErrBudgetForbidden
	}
	if errors.Is(err, repository.ErrPendingScheduleExists) {
		return ErrPendingScheduleExists
	}
	return err
}

func (s *BudgetTierService) GetAllocation(ctx context.Context, id uuid.UUID) (*model.BudgetAllocation, error) {
	return s.getAllocation(ctx, id)
}

func buildTierLimitView(limit model.BudgetTierLimit, overrides []model.BudgetTemporaryOverride) TierLimitView {
	effective := limit.LimitUSD
	view := TierLimitView{
		ID: limit.ID, BudgetAllocationID: limit.BudgetAllocationID,
		ModelTierID: limit.ModelTierID, ModelTierName: limit.ModelTier.Name,
		PeriodType: limit.PeriodType, BaseLimitUSD: limit.LimitUSD,
		EffectiveLimitUSD: effective, Enabled: limit.Enabled, EffectiveAt: limit.EffectiveAt,
		SourceSummary: LimitSourceSummary{SourceType: "base"},
	}
	for index := range overrides {
		override := &overrides[index]
		if override.DimensionType != budgetdomain.DimensionTier || override.ModelTierID == nil ||
			*override.ModelTierID != limit.ModelTierID || override.PeriodType != limit.PeriodType {
			continue
		}
		target := override.TargetLimitUSD
		view.TemporaryTargetUSD = &target
		view.OverrideExpiresAt = &override.ExpiresAt
		view.SourceSummary = LimitSourceSummary{SourceType: "temporary_override", TemporaryActive: true, ExpiresAt: &override.ExpiresAt}
		if target.Decimal.GreaterThan(effective.Decimal) {
			effective = target
			view.EffectiveLimitUSD = target
		}
	}
	return view
}
