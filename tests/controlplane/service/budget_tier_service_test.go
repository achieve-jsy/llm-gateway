package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeBudgetTierStore struct {
	existing        *model.BudgetTierLimit
	createdSchedule *model.BudgetLimitSchedule
	scheduleErr     error
	confirmReplace  bool
}

func (f *fakeBudgetTierStore) ListTierLimits(context.Context, uuid.UUID) ([]model.BudgetTierLimit, error) {
	if f.existing == nil {
		return nil, nil
	}
	return []model.BudgetTierLimit{*f.existing}, nil
}
func (f *fakeBudgetTierStore) FindTierLimit(context.Context, uuid.UUID, uuid.UUID, budgetdomain.PeriodType) (*model.BudgetTierLimit, error) {
	return f.existing, nil
}
func (f *fakeBudgetTierStore) UpsertTierLimit(_ context.Context, params repository.UpsertTierLimitParams) (*model.BudgetTierLimit, error) {
	if f.existing == nil {
		f.existing = &model.BudgetTierLimit{ID: uuid.New(), EffectiveAt: params.EffectiveAt}
	}
	f.existing.BudgetAllocationID = params.BudgetAllocationID
	f.existing.ModelTierID = params.ModelTierID
	f.existing.PeriodType = params.PeriodType
	f.existing.LimitUSD = params.LimitUSD
	f.existing.Enabled = params.Enabled
	return f.existing, nil
}
func (f *fakeBudgetTierStore) ListActiveOverrides(context.Context, uuid.UUID, time.Time) ([]model.BudgetTemporaryOverride, error) {
	return nil, nil
}
func (f *fakeBudgetTierStore) FindPendingSchedule(context.Context, uuid.UUID, string, budgetdomain.PeriodType) (*model.BudgetLimitSchedule, error) {
	return nil, nil
}
func (f *fakeBudgetTierStore) ReplacePendingSchedule(_ context.Context, schedule *model.BudgetLimitSchedule) (*model.BudgetLimitSchedule, error) {
	f.createdSchedule = schedule
	return schedule, nil
}
func (f *fakeBudgetTierStore) PutPendingSchedule(_ context.Context, schedule *model.BudgetLimitSchedule, confirmReplace bool) (*model.BudgetLimitSchedule, error) {
	f.createdSchedule = schedule
	f.confirmReplace = confirmReplace
	return schedule, f.scheduleErr
}

type fakeAllocationReader struct{ allocation *model.BudgetAllocation }

func (f fakeAllocationReader) GetAllocationByID(context.Context, uuid.UUID) (*model.BudgetAllocation, error) {
	return f.allocation, nil
}

func TestBudgetTierServiceImmediatePreservesEffectiveAt(t *testing.T) {
	cycleStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	now := cycleStart.Add(10 * time.Hour)
	store := &fakeBudgetTierStore{existing: &model.BudgetTierLimit{
		ID: uuid.New(), EffectiveAt: cycleStart, LimitUSD: mustAmount(t, "80"), Enabled: true,
	}}
	tierID := uuid.New()
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Name: "高级", Enabled: true}
	svc := service.NewBudgetTierService(store, fakeAllocationReader{&model.BudgetAllocation{ID: uuid.New()}}, tierRepo, func() time.Time { return now })

	got, err := svc.SetTierLimit(context.Background(), service.SetTierLimitInput{
		AllocationID: uuid.New(), TierID: tierID, PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustAmount(t, "100"), Enabled: true, EffectiveMode: service.EffectiveImmediate,
		Reason: "test",
	})
	require.NoError(t, err)
	require.True(t, got.EffectiveAt.Equal(cycleStart))
}

func TestBudgetTierDirectWriteRequiresNonEmptyReason(t *testing.T) {
	tierID := uuid.New()
	repo := &fakeBudgetTierStore{}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(repo, fakeAllocationReader{}, tierRepo, time.Now)

	_, err := svc.SetTierLimit(context.Background(), service.SetTierLimitInput{
		AllocationID: uuid.New(), TierID: tierID, PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustAmount(t, "100"), Enabled: true, EffectiveMode: service.EffectiveImmediate,
	})
	require.ErrorIs(t, err, service.ErrBudgetReasonRequired)
	require.Nil(t, repo.existing)
}

func TestBudgetTierListMarksDisabledModelTierAsDisabled(t *testing.T) {
	allocationID, tierID := uuid.New(), uuid.New()
	repo := &fakeBudgetTierStore{existing: &model.BudgetTierLimit{
		ID: uuid.New(), BudgetAllocationID: allocationID, ModelTierID: tierID,
		ModelTier:  model.ModelTier{ID: tierID, Name: "高级", Enabled: false},
		PeriodType: budgetdomain.PeriodMonthly, LimitUSD: mustAmount(t, "100"), Enabled: true,
		EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}}
	svc := service.NewBudgetTierService(
		repo,
		fakeAllocationReader{&model.BudgetAllocation{ID: allocationID}},
		newFakeModelTierRepo(),
		time.Now,
	)

	got, err := svc.ListTierLimits(context.Background(), allocationID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.False(t, got[0].Enabled)
}

func TestBudgetTierDirectWriteReturnsTypedEffectiveModeError(t *testing.T) {
	tierID := uuid.New()
	repo := &fakeBudgetTierStore{}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(repo, fakeAllocationReader{&model.BudgetAllocation{ID: uuid.New()}}, tierRepo, time.Now)
	_, err := svc.SetTierLimit(context.Background(), service.SetTierLimitInput{
		AllocationID: uuid.New(), TierID: tierID, PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustAmount(t, "100"), Enabled: true, EffectiveMode: "later", Reason: "test",
	})
	require.ErrorIs(t, err, service.ErrInvalidEffectiveMode)
}

func TestBudgetTierNextPeriodUsesAtomicPendingScheduleWrite(t *testing.T) {
	tierID := uuid.New()
	store := &fakeBudgetTierStore{scheduleErr: repository.ErrPendingScheduleExists}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(
		store,
		fakeAllocationReader{&model.BudgetAllocation{ID: uuid.New()}},
		tierRepo,
		time.Now,
	)
	_, err := svc.SetTierLimit(context.Background(), service.SetTierLimitInput{
		AllocationID: uuid.New(), TierID: tierID, PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustAmount(t, "100"), Enabled: true, EffectiveMode: service.EffectiveNextPeriod,
		Reason: "next cycle", ConfirmReplaceSchedule: false,
	})
	require.ErrorIs(t, err, service.ErrPendingScheduleExists)
	require.False(t, store.confirmReplace)
	require.NotNil(t, store.createdSchedule)
}

func mustAmount(t *testing.T, raw string) budgetdomain.Amount {
	t.Helper()
	amount, err := budgetdomain.ParseAmount(raw)
	require.NoError(t, err)
	return amount
}
