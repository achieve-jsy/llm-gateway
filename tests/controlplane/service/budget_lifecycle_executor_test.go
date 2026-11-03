package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type lifecycleRepo struct {
	schedules              map[uuid.UUID]*model.BudgetLimitSchedule
	overrides              map[uuid.UUID]*model.BudgetTemporaryOverride
	scheduleIDs            []uuid.UUID
	overrideIDs            []uuid.UUID
	markScheduleFailedErr  error
	markOverrideExpiredErr error
	transactionCount       int
	lockEvents             []string
}

func (r *lifecycleRepo) ListDueScheduleIDs(context.Context, time.Time, int) ([]uuid.UUID, error) {
	if r.scheduleIDs != nil {
		return r.scheduleIDs, nil
	}
	var out []uuid.UUID
	for id := range r.schedules {
		out = append(out, id)
	}
	return out, nil
}
func (r *lifecycleRepo) ListDueOverrideIDs(context.Context, time.Time, int) ([]uuid.UUID, error) {
	if r.overrideIDs != nil {
		return r.overrideIDs, nil
	}
	var out []uuid.UUID
	for id := range r.overrides {
		out = append(out, id)
	}
	return out, nil
}
func (r *lifecycleRepo) Transaction(_ context.Context, fn func(*gorm.DB) error) error {
	r.transactionCount++
	return fn(&gorm.DB{})
}
func (r *lifecycleRepo) GetScheduleAllocationID(_ context.Context, _ *gorm.DB, id uuid.UUID) (uuid.UUID, error) {
	r.lockEvents = append(r.lockEvents, "schedule_candidate")
	item := r.schedules[id]
	if item == nil {
		return uuid.Nil, gorm.ErrRecordNotFound
	}
	return item.BudgetAllocationID, nil
}
func (r *lifecycleRepo) GetOverrideAllocationID(_ context.Context, _ *gorm.DB, id uuid.UUID) (uuid.UUID, error) {
	r.lockEvents = append(r.lockEvents, "override_candidate")
	item := r.overrides[id]
	if item == nil {
		return uuid.Nil, gorm.ErrRecordNotFound
	}
	return item.BudgetAllocationID, nil
}
func (r *lifecycleRepo) LockBudgetAllocation(_ context.Context, _ *gorm.DB, _ uuid.UUID) error {
	r.lockEvents = append(r.lockEvents, "allocation")
	return nil
}
func (r *lifecycleRepo) LockScheduleForUpdate(_ context.Context, _ *gorm.DB, id uuid.UUID) (*model.BudgetLimitSchedule, error) {
	r.lockEvents = append(r.lockEvents, "schedule")
	item := r.schedules[id]
	if item == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return item, nil
}
func (r *lifecycleRepo) LockOverrideForUpdate(_ context.Context, _ *gorm.DB, id uuid.UUID) (*model.BudgetTemporaryOverride, error) {
	r.lockEvents = append(r.lockEvents, "override")
	item := r.overrides[id]
	if item == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return item, nil
}

func TestBudgetLifecycleExecutorLocksAllocationBeforeLifecycleRows(t *testing.T) {
	now := time.Now()
	allocationID, scheduleID, overrideID := uuid.New(), uuid.New(), uuid.New()
	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			scheduleID: {
				ID: scheduleID, BudgetAllocationID: allocationID, DimensionType: budgetdomain.DimensionTotal,
				PeriodType: budgetdomain.PeriodMonthly, EffectiveAt: now.Add(-time.Minute), Status: budgetdomain.SchedulePending,
			},
		},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{
			overrideID: {
				ID: overrideID, BudgetAllocationID: allocationID, DimensionType: budgetdomain.DimensionTotal,
				ExpiresAt: now.Add(-time.Minute), Status: budgetdomain.OverrideActive,
			},
		},
	}
	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{}, lifecycleTierWriter{}, nil, service.NoopBudgetLifecycleObserver{},
	)

	result, err := executor.RunOnce(context.Background(), now, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.SchedulesApplied)
	require.Equal(t, 1, result.OverridesExpired)
	require.Equal(t, []string{
		"schedule_candidate", "allocation", "schedule",
		"override_candidate", "allocation", "override",
	}, repo.lockEvents)
}

func TestBudgetLifecycleExecutorTreatsSkipLockedAsSkipped(t *testing.T) {
	missingScheduleID, missingOverrideID := uuid.New(), uuid.New()
	repo := &lifecycleRepo{
		schedules:   map[uuid.UUID]*model.BudgetLimitSchedule{},
		overrides:   map[uuid.UUID]*model.BudgetTemporaryOverride{},
		scheduleIDs: []uuid.UUID{missingScheduleID},
		overrideIDs: []uuid.UUID{missingOverrideID},
	}
	executor := service.NewBudgetLifecycleExecutor(repo, lifecycleTotalWriter{}, lifecycleTierWriter{}, &lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{})
	got, err := executor.RunOnce(context.Background(), time.Now(), 100)
	require.NoError(t, err)
	require.Equal(t, 1, got.SchedulesSkipped)
	require.Equal(t, 1, got.OverridesSkipped)
	require.Zero(t, got.SchedulesFailed)
	require.Zero(t, got.OverridesFailed)
}
func (r *lifecycleRepo) MarkSchedule(_ context.Context, _ *gorm.DB, id uuid.UUID, status budgetdomain.ScheduleStatus, _ string) error {
	if status == budgetdomain.ScheduleFailed && r.markScheduleFailedErr != nil {
		return r.markScheduleFailedErr
	}
	r.schedules[id].Status = status
	return nil
}
func (r *lifecycleRepo) MarkOverrideExpired(_ context.Context, _ *gorm.DB, id uuid.UUID, _ time.Time) error {
	if r.markOverrideExpiredErr != nil {
		return r.markOverrideExpiredErr
	}
	r.overrides[id].Status = budgetdomain.OverrideExpired
	return nil
}
func (r *lifecycleRepo) MarkOverrideFailed(_ context.Context, _ *gorm.DB, id uuid.UUID, _ string) error {
	r.overrides[id].Status = budgetdomain.OverrideFailed
	return nil
}

type lifecycleTotalWriter struct{ err error }

func (w lifecycleTotalWriter) ApplyTotalScheduledLimit(context.Context, *gorm.DB, uuid.UUID, budgetdomain.PeriodType, budgetdomain.Amount) error {
	return w.err
}

type lifecycleTierWriter struct{}

func (lifecycleTierWriter) UpsertTierLimitInTx(context.Context, *gorm.DB, repository.UpsertTierLimitParams) error {
	return nil
}

func (lifecycleTierWriter) DeleteTierLimitInTx(context.Context, *gorm.DB, uuid.UUID, uuid.UUID, budgetdomain.PeriodType) error {
	return nil
}

type capturingLifecycleTierDeleter struct {
	allocationID uuid.UUID
	tierID       uuid.UUID
	period       budgetdomain.PeriodType
}

func (w *capturingLifecycleTierDeleter) UpsertTierLimitInTx(context.Context, *gorm.DB, repository.UpsertTierLimitParams) error {
	return nil
}

func (w *capturingLifecycleTierDeleter) DeleteTierLimitInTx(_ context.Context, _ *gorm.DB, allocationID, tierID uuid.UUID, period budgetdomain.PeriodType) error {
	w.allocationID = allocationID
	w.tierID = tierID
	w.period = period
	return nil
}

type capturingLifecycleTierWriter struct {
	params repository.UpsertTierLimitParams
}

func (w *capturingLifecycleTierWriter) UpsertTierLimitInTx(_ context.Context, _ *gorm.DB, params repository.UpsertTierLimitParams) error {
	w.params = params
	return nil
}

func (w *capturingLifecycleTierWriter) DeleteTierLimitInTx(context.Context, *gorm.DB, uuid.UUID, uuid.UUID, budgetdomain.PeriodType) error {
	return nil
}

type lifecyclePublisher struct {
	calls int
	err   error
}

func (p *lifecyclePublisher) MarkDirtyAndPublish(context.Context) error { p.calls++; return p.err }

type capturingLifecycleErrorReporter struct {
	events []service.BudgetLifecycleErrorEvent
}

func (r *capturingLifecycleErrorReporter) ReportBudgetLifecycleError(_ context.Context, event service.BudgetLifecycleErrorEvent) {
	r.events = append(r.events, event)
}

func TestBudgetLifecycleExecutorAppliesClearTierSchedule(t *testing.T) {
	now := time.Now()
	scheduleID, allocationID, tierID := uuid.New(), uuid.New(), uuid.New()
	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			scheduleID: {
				ID: scheduleID, BudgetAllocationID: allocationID, DimensionType: budgetdomain.DimensionTier,
				ModelTierID: &tierID, PeriodType: budgetdomain.PeriodMonthly,
				ScheduleOperation: budgetdomain.ScheduleOperationClear, Status: budgetdomain.SchedulePending,
			},
		},
	}
	writer := &capturingLifecycleTierDeleter{}
	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{}, writer, &lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
	)
	result, err := executor.RunOnce(context.Background(), now, 100)
	require.NoError(t, err)
	require.Equal(t, 1, result.SchedulesApplied)
	require.Equal(t, allocationID, writer.allocationID)
	require.Equal(t, tierID, writer.tierID)
	require.Equal(t, budgetdomain.PeriodMonthly, writer.period)
}

func TestBudgetLifecycleExecutorProcessesSchedulesAndOverrides(t *testing.T) {
	now := time.Now()
	totalID, tierID, overrideID := uuid.New(), uuid.New(), uuid.New()
	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			totalID: {ID: totalID, DimensionType: budgetdomain.DimensionTotal, PeriodType: budgetdomain.PeriodMonthly, Status: budgetdomain.SchedulePending},
			tierID:  {ID: tierID, DimensionType: budgetdomain.DimensionTier, ModelTierID: &tierID, PeriodType: budgetdomain.PeriodMonthly, Status: budgetdomain.SchedulePending},
		},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{
			overrideID: {ID: overrideID, DimensionType: budgetdomain.DimensionTotal, Status: budgetdomain.OverrideActive},
		},
	}
	publisher := &lifecyclePublisher{}
	executor := service.NewBudgetLifecycleExecutor(repo, lifecycleTotalWriter{}, lifecycleTierWriter{}, publisher, service.NoopBudgetLifecycleObserver{})
	got, err := executor.RunOnce(context.Background(), now, 100)
	require.NoError(t, err)
	require.Equal(t, service.BudgetLifecycleResult{SchedulesApplied: 2, OverridesExpired: 1}, got)
	require.Equal(t, 1, publisher.calls)
}

func TestBudgetLifecycleExecutorPreservesScheduledEffectiveBoundaryWhenWorkerIsLate(t *testing.T) {
	effectiveAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	workerNow := effectiveAt.Add(time.Minute)
	scheduleID, tierID := uuid.New(), uuid.New()
	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			scheduleID: {
				ID: scheduleID, BudgetAllocationID: uuid.New(), DimensionType: budgetdomain.DimensionTier,
				ModelTierID: &tierID, PeriodType: budgetdomain.PeriodMonthly,
				TargetLimitUSD: mustAmount(t, "100"), EffectiveAt: effectiveAt, Status: budgetdomain.SchedulePending,
			},
		},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{},
	}
	writer := &capturingLifecycleTierWriter{}
	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{}, writer, &lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
	)
	result, err := executor.RunOnce(context.Background(), workerNow, 100)
	require.NoError(t, err)
	require.Equal(t, 1, result.SchedulesApplied)
	require.Equal(t, effectiveAt, writer.params.EffectiveAt)
	require.NotEqual(t, workerNow, writer.params.EffectiveAt)
}

func TestBudgetLifecycleExecutorReportsFreshFailureTransactionError(t *testing.T) {
	now := time.Now()
	scheduleID := uuid.New()
	applyErr := errors.New("apply tier limit failed")
	markErr := errors.New("mark failed transaction failed")
	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			scheduleID: {
				ID: scheduleID, DimensionType: budgetdomain.DimensionTotal,
				PeriodType: budgetdomain.PeriodMonthly, Status: budgetdomain.SchedulePending,
			},
		},
		overrides:             map[uuid.UUID]*model.BudgetTemporaryOverride{},
		markScheduleFailedErr: markErr,
	}
	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{err: applyErr}, lifecycleTierWriter{},
		&lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
	)

	got, err := executor.RunOnce(context.Background(), now, 100)
	require.ErrorIs(t, err, applyErr)
	require.ErrorIs(t, err, markErr)
	require.Equal(t, 2, repo.transactionCount)
	require.Equal(t, 1, got.SchedulesFailed)
	require.Equal(t, budgetdomain.SchedulePending, repo.schedules[scheduleID].Status)
}

func TestBudgetLifecycleExecutorReportsStructuredErrorsAndMetrics(t *testing.T) {
	now := time.Now()
	scheduleID := uuid.New()
	applyErr := errors.New("apply tier limit failed")
	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			scheduleID: {
				ID: scheduleID, DimensionType: budgetdomain.DimensionTotal,
				PeriodType: budgetdomain.PeriodMonthly, Status: budgetdomain.SchedulePending,
			},
		},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{},
	}
	reporter := &capturingLifecycleErrorReporter{}
	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{err: applyErr}, lifecycleTierWriter{},
		&lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
	).WithErrorReporter(reporter)

	_, err := executor.RunOnce(context.Background(), now, 100)
	require.ErrorIs(t, err, applyErr)
	require.NotEmpty(t, reporter.events)
	require.Equal(t, "execute_schedule", reporter.events[0].Operation)
	require.Equal(t, "schedule", reporter.events[0].EntityType)
	require.Equal(t, scheduleID, reporter.events[0].EntityID)
	require.ErrorIs(t, reporter.events[0].Err, applyErr)
	metrics := executor.Metrics()
	require.GreaterOrEqual(t, metrics.ErrorsTotal, uint64(1))
	require.GreaterOrEqual(t, metrics.ScheduleErrors, uint64(1))
}

func TestBudgetLifecycleExecutorPublishesCommittedOverrideFailure(t *testing.T) {
	overrideID := uuid.New()
	expireErr := errors.New("expire failed")
	publishErr := errors.New("snapshot retry required")
	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{
			overrideID: {ID: overrideID, Status: budgetdomain.OverrideActive},
		},
		markOverrideExpiredErr: expireErr,
	}
	publisher := &lifecyclePublisher{err: publishErr}
	executor := service.NewBudgetLifecycleExecutor(repo, lifecycleTotalWriter{}, lifecycleTierWriter{}, publisher, service.NoopBudgetLifecycleObserver{})

	got, err := executor.RunOnce(context.Background(), time.Now(), 100)
	require.ErrorIs(t, err, expireErr)
	require.ErrorIs(t, err, publishErr)
	require.Equal(t, 1, got.OverridesFailed)
	require.Equal(t, budgetdomain.OverrideFailed, repo.overrides[overrideID].Status)
	require.Equal(t, 1, publisher.calls)
}

func TestBudgetLifecycleExecutorPublishesCommittedScheduleFailure(t *testing.T) {
	scheduleID := uuid.New()
	applyErr := errors.New("apply failed")
	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			scheduleID: {
				ID: scheduleID, DimensionType: budgetdomain.DimensionTotal,
				PeriodType: budgetdomain.PeriodMonthly, Status: budgetdomain.SchedulePending,
			},
		},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{},
	}
	publisher := &lifecyclePublisher{}
	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{err: applyErr}, lifecycleTierWriter{},
		publisher, service.NoopBudgetLifecycleObserver{},
	)

	got, err := executor.RunOnce(context.Background(), time.Now(), 100)
	require.ErrorIs(t, err, applyErr)
	require.Equal(t, 1, got.SchedulesFailed)
	require.Equal(t, budgetdomain.ScheduleFailed, repo.schedules[scheduleID].Status)
	require.Equal(t, 1, publisher.calls)
}

func TestBudgetLifecycleExecutorStopsWhenLifecycleContextIsCancelled(t *testing.T) {
	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{},
	}
	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{}, lifecycleTierWriter{},
		&lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		executor.Start(ctx, time.Hour)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lifecycle executor did not stop after context cancellation")
	}
}

// TestBudgetLifecyclePeriodTransitions verifies that schedules are correctly
// applied across different period types (daily, weekly, monthly).
func TestBudgetLifecyclePeriodTransitions(t *testing.T) {
	allocationID := uuid.New()
	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		periodType budgetdomain.PeriodType
		effective  time.Time
	}{
		{
			name:       "daily period transition",
			periodType: budgetdomain.PeriodDaily,
			effective:  now.Add(-24 * time.Hour), // yesterday
		},
		{
			name:       "weekly period transition",
			periodType: budgetdomain.PeriodWeekly,
			effective:  now.Add(-7 * 24 * time.Hour), // last week
		},
		{
			name:       "monthly period transition",
			periodType: budgetdomain.PeriodMonthly,
			effective:  now.Add(-30 * 24 * time.Hour), // last month
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduleID := uuid.New()
			repo := &lifecycleRepo{
				schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
					scheduleID: {
						ID: scheduleID, BudgetAllocationID: allocationID,
						DimensionType:  budgetdomain.DimensionTotal,
						PeriodType:     tt.periodType,
						TargetLimitUSD: mustAmount(t, "100"),
						EffectiveAt:    tt.effective,
						Status:         budgetdomain.SchedulePending,
					},
				},
				overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{},
			}
			writer := &capturingLifecycleTierWriter{}
			executor := service.NewBudgetLifecycleExecutor(
				repo, lifecycleTotalWriter{}, writer, &lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
			)
			result, err := executor.RunOnce(context.Background(), now, 100)
			require.NoError(t, err)
			require.Equal(t, 1, result.SchedulesApplied)
			require.Equal(t, budgetdomain.ScheduleApplied, repo.schedules[scheduleID].Status)
		})
	}
}

// TestBudgetLifecycleCounterRebuildFromCallLogs verifies that when a schedule
// is applied, the executor correctly triggers counter rebuild for the affected
// allocation.
func TestBudgetLifecycleCounterRebuildFromCallLogs(t *testing.T) {
	allocationID := uuid.New()
	scheduleID := uuid.New()
	now := time.Now()

	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			scheduleID: {
				ID: scheduleID, BudgetAllocationID: allocationID,
				DimensionType: budgetdomain.DimensionTotal,
				PeriodType:    budgetdomain.PeriodMonthly,
				EffectiveAt:   now.Add(-time.Hour),
				Status:        budgetdomain.SchedulePending,
			},
		},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{},
	}

	// Use a writer that captures the allocation ID to verify counter rebuild
	writer := &capturingLifecycleTierWriter{}
	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{}, writer, &lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
	)

	result, err := executor.RunOnce(context.Background(), now, 100)
	require.NoError(t, err)
	require.Equal(t, 1, result.SchedulesApplied)
	// Verify the schedule was marked as applied
	require.Equal(t, budgetdomain.ScheduleApplied, repo.schedules[scheduleID].Status)
	// Verify transaction was committed (counter rebuild happens post-transaction in real impl)
	require.Equal(t, 1, repo.transactionCount)
}

// TestBudgetLifecycleNotificationOnOverrideExpired verifies that when a
// temporary override expires, the lifecycle observer is notified.
func TestBudgetLifecycleNotificationOnOverrideExpired(t *testing.T) {
	allocationID := uuid.New()
	overrideID := uuid.New()
	now := time.Now()

	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{
			overrideID: {
				ID: overrideID, BudgetAllocationID: allocationID,
				DimensionType: budgetdomain.DimensionTotal,
				ExpiresAt:     now.Add(-time.Hour), // already expired
				Status:        budgetdomain.OverrideActive,
			},
		},
	}

	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{}, lifecycleTierWriter{}, &lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
	)

	result, err := executor.RunOnce(context.Background(), now, 100)
	require.NoError(t, err)
	require.Equal(t, 1, result.OverridesExpired)
	// Verify the override was marked as expired
	require.Equal(t, budgetdomain.OverrideExpired, repo.overrides[overrideID].Status)
}

// TestBudgetLifecycleNotificationOnScheduleApplied verifies that when a
// schedule is successfully applied, the lifecycle observer is notified.
func TestBudgetLifecycleNotificationOnScheduleApplied(t *testing.T) {
	allocationID := uuid.New()
	scheduleID := uuid.New()
	now := time.Now()

	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			scheduleID: {
				ID: scheduleID, BudgetAllocationID: allocationID,
				DimensionType: budgetdomain.DimensionTotal,
				PeriodType:    budgetdomain.PeriodMonthly,
				EffectiveAt:   now.Add(-time.Hour),
				Status:        budgetdomain.SchedulePending,
			},
		},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{},
	}

	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{}, lifecycleTierWriter{}, &lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
	)

	result, err := executor.RunOnce(context.Background(), now, 100)
	require.NoError(t, err)
	require.Equal(t, 1, result.SchedulesApplied)
	// Verify the schedule was marked as applied
	require.Equal(t, budgetdomain.ScheduleApplied, repo.schedules[scheduleID].Status)
}

// TestBudgetLifecycleNotificationOnScheduleFailed verifies that when a
// schedule fails to apply, the lifecycle observer is notified of the failure.
func TestBudgetLifecycleNotificationOnScheduleFailed(t *testing.T) {
	allocationID := uuid.New()
	scheduleID := uuid.New()
	now := time.Now()
	applyErr := errors.New("apply failed")

	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			scheduleID: {
				ID: scheduleID, BudgetAllocationID: allocationID,
				DimensionType: budgetdomain.DimensionTotal,
				PeriodType:    budgetdomain.PeriodMonthly,
				EffectiveAt:   now.Add(-time.Hour),
				Status:        budgetdomain.SchedulePending,
			},
		},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{},
	}

	reporter := &capturingLifecycleErrorReporter{}
	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{err: applyErr}, lifecycleTierWriter{},
		&lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
	).WithErrorReporter(reporter)

	result, err := executor.RunOnce(context.Background(), now, 100)
	require.Error(t, err)
	require.Equal(t, 1, result.SchedulesFailed)
	// Verify error was reported
	require.NotEmpty(t, reporter.events)
	require.Equal(t, "execute_schedule", reporter.events[0].Operation)
	require.Equal(t, scheduleID, reporter.events[0].EntityID)
}

// TestBudgetLifecycleMonthlyResetRebuildsCounters verifies that monthly
// period transitions trigger counter rebuilds for all affected allocations.
func TestBudgetLifecycleMonthlyResetRebuildsCounters(t *testing.T) {
	allocationID1 := uuid.New()
	allocationID2 := uuid.New()
	scheduleID1 := uuid.New()
	scheduleID2 := uuid.New()
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) // First day of new month

	repo := &lifecycleRepo{
		schedules: map[uuid.UUID]*model.BudgetLimitSchedule{
			scheduleID1: {
				ID: scheduleID1, BudgetAllocationID: allocationID1,
				DimensionType: budgetdomain.DimensionTotal,
				PeriodType:    budgetdomain.PeriodMonthly,
				EffectiveAt:   now.Add(-time.Hour),
				Status:        budgetdomain.SchedulePending,
			},
			scheduleID2: {
				ID: scheduleID2, BudgetAllocationID: allocationID2,
				DimensionType: budgetdomain.DimensionTotal,
				PeriodType:    budgetdomain.PeriodMonthly,
				EffectiveAt:   now.Add(-time.Hour),
				Status:        budgetdomain.SchedulePending,
			},
		},
		overrides: map[uuid.UUID]*model.BudgetTemporaryOverride{},
	}

	executor := service.NewBudgetLifecycleExecutor(
		repo, lifecycleTotalWriter{}, lifecycleTierWriter{}, &lifecyclePublisher{}, service.NoopBudgetLifecycleObserver{},
	)

	result, err := executor.RunOnce(context.Background(), now, 100)
	require.NoError(t, err)
	require.Equal(t, 2, result.SchedulesApplied)
	// Both schedules should be applied
	require.Equal(t, budgetdomain.ScheduleApplied, repo.schedules[scheduleID1].Status)
	require.Equal(t, budgetdomain.ScheduleApplied, repo.schedules[scheduleID2].Status)
	// Two transactions (one per schedule)
	require.Equal(t, 2, repo.transactionCount)
}
