package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type BudgetLifecycleRepository interface {
	ListDueScheduleIDs(context.Context, time.Time, int) ([]uuid.UUID, error)
	ListDueOverrideIDs(context.Context, time.Time, int) ([]uuid.UUID, error)
	Transaction(context.Context, func(*gorm.DB) error) error
	GetScheduleAllocationID(context.Context, *gorm.DB, uuid.UUID) (uuid.UUID, error)
	GetOverrideAllocationID(context.Context, *gorm.DB, uuid.UUID) (uuid.UUID, error)
	LockBudgetAllocation(context.Context, *gorm.DB, uuid.UUID) error
	LockScheduleForUpdate(context.Context, *gorm.DB, uuid.UUID) (*model.BudgetLimitSchedule, error)
	LockOverrideForUpdate(context.Context, *gorm.DB, uuid.UUID) (*model.BudgetTemporaryOverride, error)
	MarkSchedule(context.Context, *gorm.DB, uuid.UUID, budgetdomain.ScheduleStatus, string) error
	MarkOverrideExpired(context.Context, *gorm.DB, uuid.UUID, time.Time) error
	MarkOverrideFailed(context.Context, *gorm.DB, uuid.UUID, string) error
}

type TotalBudgetScheduleWriter interface {
	ApplyTotalScheduledLimit(context.Context, *gorm.DB, uuid.UUID, budgetdomain.PeriodType, budgetdomain.Amount) error
}
type TierBudgetScheduleWriter interface {
	UpsertTierLimitInTx(context.Context, *gorm.DB, repository.UpsertTierLimitParams) error
	DeleteTierLimitInTx(context.Context, *gorm.DB, uuid.UUID, uuid.UUID, budgetdomain.PeriodType) error
}

type BudgetLifecycleResult struct {
	SchedulesApplied int
	SchedulesFailed  int
	SchedulesSkipped int
	OverridesExpired int
	OverridesFailed  int
	OverridesSkipped int
}
type AppliedBudgetSchedule struct {
	ID                 uuid.UUID
	BudgetAllocationID uuid.UUID
	DimensionType      budgetdomain.DimensionType
	ModelTierID        *uuid.UUID
	PeriodType         budgetdomain.PeriodType
	TargetLimitUSD     budgetdomain.Amount
	AppliedAt          time.Time
}
type ExpiredBudgetOverride struct {
	ID                 uuid.UUID
	BudgetAllocationID uuid.UUID
	DimensionType      budgetdomain.DimensionType
	ModelTierID        *uuid.UUID
	PeriodType         budgetdomain.PeriodType
	ExpiredAt          time.Time
	ExpansionRequestID *uuid.UUID
}
type FailedBudgetSchedule struct {
	ID                 uuid.UUID
	BudgetAllocationID uuid.UUID
	DimensionType      budgetdomain.DimensionType
	ModelTierID        *uuid.UUID
	PeriodType         budgetdomain.PeriodType
	FailureReason      string
	FailedAt           time.Time
}
type BudgetLifecycleObserver interface {
	OnOverrideExpired(context.Context, ExpiredBudgetOverride) error
	OnScheduleApplied(context.Context, AppliedBudgetSchedule) error
	OnScheduleFailed(context.Context, FailedBudgetSchedule) error
}
type NoopBudgetLifecycleObserver struct{}

func (NoopBudgetLifecycleObserver) OnOverrideExpired(context.Context, ExpiredBudgetOverride) error {
	return nil
}
func (NoopBudgetLifecycleObserver) OnScheduleApplied(context.Context, AppliedBudgetSchedule) error {
	return nil
}
func (NoopBudgetLifecycleObserver) OnScheduleFailed(context.Context, FailedBudgetSchedule) error {
	return nil
}

type BudgetLifecycleErrorEvent struct {
	Operation  string
	EntityType string
	EntityID   uuid.UUID
	Err        error
}

type BudgetLifecycleErrorReporter interface {
	ReportBudgetLifecycleError(context.Context, BudgetLifecycleErrorEvent)
}

type BudgetLifecycleMetrics struct {
	ErrorsTotal    uint64
	ScheduleErrors uint64
	OverrideErrors uint64
	ObserverErrors uint64
	PublishErrors  uint64
}

type budgetLifecycleCounters struct {
	errorsTotal    atomic.Uint64
	scheduleErrors atomic.Uint64
	overrideErrors atomic.Uint64
	observerErrors atomic.Uint64
	publishErrors  atomic.Uint64
}

type BudgetLifecycleExecutor struct {
	repo      BudgetLifecycleRepository
	total     TotalBudgetScheduleWriter
	tier      TierBudgetScheduleWriter
	publisher BudgetConfigPublisher
	observer  BudgetLifecycleObserver
	reporter  BudgetLifecycleErrorReporter
	counters  budgetLifecycleCounters
}

func (e *BudgetLifecycleExecutor) WithErrorReporter(reporter BudgetLifecycleErrorReporter) *BudgetLifecycleExecutor {
	e.reporter = reporter
	return e
}

func (e *BudgetLifecycleExecutor) Metrics() BudgetLifecycleMetrics {
	return BudgetLifecycleMetrics{
		ErrorsTotal: e.counters.errorsTotal.Load(), ScheduleErrors: e.counters.scheduleErrors.Load(),
		OverrideErrors: e.counters.overrideErrors.Load(), ObserverErrors: e.counters.observerErrors.Load(),
		PublishErrors: e.counters.publishErrors.Load(),
	}
}

func (e *BudgetLifecycleExecutor) reportError(ctx context.Context, operation, entityType string, entityID uuid.UUID, err error) {
	if err == nil {
		return
	}
	e.counters.errorsTotal.Add(1)
	switch entityType {
	case "schedule":
		e.counters.scheduleErrors.Add(1)
	case "override":
		e.counters.overrideErrors.Add(1)
	}
	if operation == "observer" {
		e.counters.observerErrors.Add(1)
	}
	if operation == "publish_snapshot" {
		e.counters.publishErrors.Add(1)
	}
	slog.ErrorContext(ctx, "budget lifecycle operation failed",
		"operation", operation, "entity_type", entityType, "entity_id", entityID, "error", err)
	if e.reporter != nil {
		e.reporter.ReportBudgetLifecycleError(ctx, BudgetLifecycleErrorEvent{
			Operation: operation, EntityType: entityType, EntityID: entityID, Err: err,
		})
	}
}

func NewBudgetLifecycleExecutor(repo BudgetLifecycleRepository, total TotalBudgetScheduleWriter, tier TierBudgetScheduleWriter, publisher BudgetConfigPublisher, observer BudgetLifecycleObserver) *BudgetLifecycleExecutor {
	if observer == nil {
		observer = NoopBudgetLifecycleObserver{}
	}
	return &BudgetLifecycleExecutor{repo: repo, total: total, tier: tier, publisher: publisher, observer: observer}
}

func (e *BudgetLifecycleExecutor) RunOnce(ctx context.Context, now time.Time, batchSize int) (BudgetLifecycleResult, error) {
	var result BudgetLifecycleResult
	var failures []error
	if retryer, ok := e.observer.(interface {
		RetryPending(context.Context, int) error
	}); ok {
		if err := retryer.RetryPending(ctx, batchSize); err != nil {
			failures = append(failures, err)
			e.reportError(ctx, "observer", "notification_outbox", uuid.Nil, err)
		}
	}
	scheduleIDs, err := e.repo.ListDueScheduleIDs(ctx, now, batchSize)
	if err != nil {
		e.reportError(ctx, "list_due", "schedule", uuid.Nil, err)
		return result, err
	}
	overrideIDs, err := e.repo.ListDueOverrideIDs(ctx, now, batchSize)
	if err != nil {
		e.reportError(ctx, "list_due", "override", uuid.Nil, err)
		return result, err
	}
	committed := false
	for _, id := range scheduleIDs {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		item, executeErr := e.executeSchedule(ctx, id, now)
		if executeErr != nil {
			if errors.Is(executeErr, gorm.ErrRecordNotFound) {
				result.SchedulesSkipped++
				continue
			}
			failedItem, markErr := e.markScheduleFailed(ctx, id, executeErr, now)
			if errors.Is(markErr, gorm.ErrRecordNotFound) {
				result.SchedulesSkipped++
				continue
			}
			result.SchedulesFailed++
			failures = append(failures, executeErr)
			e.reportError(ctx, "execute_schedule", "schedule", id, executeErr)
			if markErr != nil {
				failures = append(failures, markErr)
				e.reportError(ctx, "mark_failed", "schedule", id, markErr)
			} else {
				committed = true
				if observerErr := e.observer.OnScheduleFailed(ctx, failedItem); observerErr != nil {
					failures = append(failures, observerErr)
					e.reportError(ctx, "observer", "schedule", id, observerErr)
				}
			}
			continue
		}
		result.SchedulesApplied++
		committed = true
		if observerErr := e.observer.OnScheduleApplied(ctx, item); observerErr != nil {
			failures = append(failures, observerErr)
			e.reportError(ctx, "observer", "schedule", id, observerErr)
		}
	}
	for _, id := range overrideIDs {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		item, executeErr := e.executeOverride(ctx, id, now)
		if executeErr != nil {
			if errors.Is(executeErr, gorm.ErrRecordNotFound) {
				result.OverridesSkipped++
				continue
			}
			markErr := e.markOverrideFailed(ctx, id, executeErr, now)
			if errors.Is(markErr, gorm.ErrRecordNotFound) {
				result.OverridesSkipped++
				continue
			}
			result.OverridesFailed++
			failures = append(failures, executeErr)
			e.reportError(ctx, "execute_override", "override", id, executeErr)
			if markErr != nil {
				failures = append(failures, markErr)
				e.reportError(ctx, "mark_failed", "override", id, markErr)
			} else {
				committed = true
			}
			continue
		}
		result.OverridesExpired++
		committed = true
		if observerErr := e.observer.OnOverrideExpired(ctx, item); observerErr != nil {
			failures = append(failures, observerErr)
			e.reportError(ctx, "observer", "override", id, observerErr)
		}
	}
	if committed && e.publisher != nil {
		if publishErr := e.publisher.MarkDirtyAndPublish(ctx); publishErr != nil {
			failures = append(failures, publishErr)
			e.reportError(ctx, "publish_snapshot", "snapshot", uuid.Nil, publishErr)
		}
	}
	return result, errors.Join(failures...)
}

func (e *BudgetLifecycleExecutor) executeSchedule(ctx context.Context, id uuid.UUID, now time.Time) (AppliedBudgetSchedule, error) {
	var item AppliedBudgetSchedule
	err := e.repo.Transaction(ctx, func(tx *gorm.DB) error {
		schedule, err := e.lockScheduleAllocationFirst(ctx, tx, id)
		if err != nil {
			return err
		}
		if schedule == nil || schedule.Status != budgetdomain.SchedulePending || schedule.EffectiveAt.After(now) {
			return gorm.ErrRecordNotFound
		}
		if schedule.DimensionType == budgetdomain.DimensionTotal {
			err = e.total.ApplyTotalScheduledLimit(ctx, tx, schedule.BudgetAllocationID, schedule.PeriodType, schedule.TargetLimitUSD)
		} else if schedule.ModelTierID != nil {
			switch schedule.ScheduleOperation {
			case budgetdomain.ScheduleOperationClear:
				err = e.tier.DeleteTierLimitInTx(ctx, tx, schedule.BudgetAllocationID, *schedule.ModelTierID, schedule.PeriodType)
			default:
				err = e.tier.UpsertTierLimitInTx(ctx, tx, repository.UpsertTierLimitParams{
					BudgetAllocationID: schedule.BudgetAllocationID, ModelTierID: *schedule.ModelTierID,
					PeriodType: schedule.PeriodType, LimitUSD: schedule.TargetLimitUSD, EffectiveAt: schedule.EffectiveAt, Enabled: true,
				})
			}
		} else {
			err = fmt.Errorf("tier schedule missing model tier")
		}
		if err != nil {
			return err
		}
		if err := e.repo.MarkSchedule(ctx, tx, id, budgetdomain.ScheduleApplied, ""); err != nil {
			return err
		}
		item = AppliedBudgetSchedule{ID: id, BudgetAllocationID: schedule.BudgetAllocationID, DimensionType: schedule.DimensionType, ModelTierID: schedule.ModelTierID, PeriodType: schedule.PeriodType, TargetLimitUSD: schedule.TargetLimitUSD, AppliedAt: now}
		return nil
	})
	return item, err
}

func (e *BudgetLifecycleExecutor) executeOverride(ctx context.Context, id uuid.UUID, now time.Time) (ExpiredBudgetOverride, error) {
	var item ExpiredBudgetOverride
	err := e.repo.Transaction(ctx, func(tx *gorm.DB) error {
		override, err := e.lockOverrideAllocationFirst(ctx, tx, id)
		if err != nil {
			return err
		}
		if override == nil || override.Status != budgetdomain.OverrideActive || override.ExpiresAt.After(now) {
			return gorm.ErrRecordNotFound
		}
		if err := e.repo.MarkOverrideExpired(ctx, tx, id, now); err != nil {
			return err
		}
		item = ExpiredBudgetOverride{ID: id, BudgetAllocationID: override.BudgetAllocationID, DimensionType: override.DimensionType, ModelTierID: override.ModelTierID, PeriodType: override.PeriodType, ExpiredAt: now, ExpansionRequestID: override.ExpansionRequestID}
		return nil
	})
	return item, err
}

func (e *BudgetLifecycleExecutor) markScheduleFailed(ctx context.Context, id uuid.UUID, cause error, now time.Time) (FailedBudgetSchedule, error) {
	reason := truncateLifecycleReason(cause)
	var item FailedBudgetSchedule
	err := e.repo.Transaction(ctx, func(tx *gorm.DB) error {
		schedule, err := e.lockScheduleAllocationFirst(ctx, tx, id)
		if err != nil {
			return err
		}
		if schedule.Status != budgetdomain.SchedulePending || schedule.EffectiveAt.After(now) {
			return gorm.ErrRecordNotFound
		}
		item = FailedBudgetSchedule{ID: id, BudgetAllocationID: schedule.BudgetAllocationID, DimensionType: schedule.DimensionType, ModelTierID: schedule.ModelTierID, PeriodType: schedule.PeriodType, FailureReason: reason, FailedAt: now}
		return e.repo.MarkSchedule(ctx, tx, id, budgetdomain.ScheduleFailed, reason)
	})
	return item, err
}

func (e *BudgetLifecycleExecutor) markOverrideFailed(ctx context.Context, id uuid.UUID, cause error, now time.Time) error {
	return e.repo.Transaction(ctx, func(tx *gorm.DB) error {
		override, err := e.lockOverrideAllocationFirst(ctx, tx, id)
		if err != nil {
			return err
		}
		if override.Status != budgetdomain.OverrideActive || override.ExpiresAt.After(now) {
			return gorm.ErrRecordNotFound
		}
		return e.repo.MarkOverrideFailed(ctx, tx, id, truncateLifecycleReason(cause))
	})
}

func (e *BudgetLifecycleExecutor) lockScheduleAllocationFirst(
	ctx context.Context,
	tx *gorm.DB,
	id uuid.UUID,
) (*model.BudgetLimitSchedule, error) {
	allocationID, err := e.repo.GetScheduleAllocationID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := e.repo.LockBudgetAllocation(ctx, tx, allocationID); err != nil {
		return nil, err
	}
	return e.repo.LockScheduleForUpdate(ctx, tx, id)
}

func (e *BudgetLifecycleExecutor) lockOverrideAllocationFirst(
	ctx context.Context,
	tx *gorm.DB,
	id uuid.UUID,
) (*model.BudgetTemporaryOverride, error) {
	allocationID, err := e.repo.GetOverrideAllocationID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := e.repo.LockBudgetAllocation(ctx, tx, allocationID); err != nil {
		return nil, err
	}
	return e.repo.LockOverrideForUpdate(ctx, tx, id)
}

func truncateLifecycleReason(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}

func (e *BudgetLifecycleExecutor) Start(ctx context.Context, interval time.Duration) {
	if ctx.Err() != nil {
		return
	}
	_, _ = e.RunOnce(ctx, time.Now(), 100)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_, _ = e.RunOnce(ctx, now, 100)
		}
	}
}
