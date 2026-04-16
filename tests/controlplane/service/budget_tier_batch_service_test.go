package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type sharedMemberAllocationStore struct {
	member     *model.BudgetAllocation
	upserts    int
	convertErr error
	tierParams repository.UpsertTierLimitParams
	risk       repository.BatchTierRiskSnapshot
}

type countingBudgetPublisher struct {
	mu    sync.Mutex
	calls int
	err   error
}

type atomicBudgetTierStore struct {
	fakeBudgetTierStore
	beforeTx func()
	writes   int
}

func (s *atomicBudgetTierStore) Transaction(_ context.Context, fn func(*gorm.DB) error) error {
	if s.beforeTx != nil {
		s.beforeTx()
	}
	return fn(nil)
}

func (s *atomicBudgetTierStore) FindTierLimitForUpdate(context.Context, *gorm.DB, uuid.UUID, uuid.UUID, budgetdomain.PeriodType) (*model.BudgetTierLimit, error) {
	return s.existing, nil
}

func (s *atomicBudgetTierStore) UpsertTierLimitInTx(_ context.Context, _ *gorm.DB, params repository.UpsertTierLimitParams) error {
	s.writes++
	_, err := s.UpsertTierLimit(context.Background(), params)
	return err
}

func (s *atomicBudgetTierStore) PutPendingScheduleInTx(context.Context, *gorm.DB, *model.BudgetLimitSchedule, bool) error {
	return nil
}

type atomicAllocationStore struct {
	allocation *model.BudgetAllocation
	risk       repository.BatchTierRiskSnapshot
}

func (s *atomicAllocationStore) GetAllocationByID(context.Context, uuid.UUID) (*model.BudgetAllocation, error) {
	return s.allocation, nil
}

func (s *atomicAllocationStore) GetAllocationByIDInTx(context.Context, *gorm.DB, uuid.UUID) (*model.BudgetAllocation, error) {
	return s.allocation, nil
}

func (s *atomicAllocationStore) GetAllocationInTx(context.Context, *gorm.DB, string, string) (*model.BudgetAllocation, error) {
	return s.allocation, nil
}

func (s *atomicAllocationStore) GetBatchTierRiskSnapshot(context.Context, model.BudgetAllocation, uuid.UUID, budgetdomain.PeriodType) (repository.BatchTierRiskSnapshot, error) {
	return s.risk, nil
}

func (s *atomicAllocationStore) GetBatchTierRiskSnapshotInTx(context.Context, *gorm.DB, model.BudgetAllocation, uuid.UUID, budgetdomain.PeriodType) (repository.BatchTierRiskSnapshot, error) {
	return s.risk, nil
}

func (s *atomicAllocationStore) CreatePersonalAllocationWithTierLimitInTx(context.Context, *gorm.DB, *model.BudgetAllocation, repository.UpsertTierLimitParams) error {
	return nil
}

func (p *countingBudgetPublisher) MarkDirtyAndPublish(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.err
}

func (p *countingBudgetPublisher) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (s *sharedMemberAllocationStore) GetAllocationByID(context.Context, uuid.UUID) (*model.BudgetAllocation, error) {
	return s.member, nil
}

func (s *sharedMemberAllocationStore) GetAllocation(_, _ string) (*model.BudgetAllocation, error) {
	return s.member, nil
}

func (s *sharedMemberAllocationStore) UpsertAllocation(allocation *model.BudgetAllocation) error {
	s.member = allocation
	s.upserts++
	return nil
}
func (s *sharedMemberAllocationStore) CreatePersonalAllocationWithTierLimit(_ context.Context, allocation *model.BudgetAllocation, params repository.UpsertTierLimitParams) error {
	if s.convertErr != nil {
		return s.convertErr
	}
	s.member = allocation
	s.tierParams = params
	s.upserts++
	return nil
}

func (s *sharedMemberAllocationStore) MemberInDeptIDs(string, []string) (bool, error) {
	return true, nil
}

func (s *sharedMemberAllocationStore) GetBatchTierRiskSnapshot(context.Context, model.BudgetAllocation, uuid.UUID, budgetdomain.PeriodType) (repository.BatchTierRiskSnapshot, error) {
	return s.risk, nil
}

func TestBatchTierLimitPreviewReturnsValidItemWithoutWriting(t *testing.T) {
	allocationID, tierID := uuid.New(), uuid.New()
	store := &fakeBudgetTierStore{}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(
		store,
		fakeAllocationReader{&model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany}},
		tierRepo,
		func() time.Time { return time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC) },
	)
	preview, err := svc.PreviewBatch(context.Background(), rbac.Actor{IsGlobal: true}, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed,
		Items: []service.BatchTierLimitItem{{
			ItemID: "row-1", AllocationID: allocationID, TierID: tierID,
			PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "100"),
		}},
		Reason: "统一高级额度",
	})
	require.NoError(t, err)
	require.Len(t, preview.Items, 1)
	require.Equal(t, "valid", preview.Items[0].Status)
	require.NotEmpty(t, preview.PreviewToken)
	require.Nil(t, store.existing)
	require.Equal(t, nowPlus(t, 5*time.Minute), preview.ExpiresAt)
	require.Equal(t, "100.000000", preview.Items[0].NewLimitUSD.String())
	require.Equal(t, "100.000000", preview.Items[0].DeltaUSD)
}

func TestBatchPreviewReturnsRecognizableValidationErrors(t *testing.T) {
	svc := service.NewBudgetTierService(nil, nil, nil, time.Now)
	validItem := service.BatchTierLimitItem{ItemID: "row-1"}
	tests := map[string]service.BatchTierLimitInput{
		"empty items":       {Operation: service.BatchSetFixed, Reason: "batch"},
		"invalid operation": {Operation: "multiply", Reason: "batch", Items: []service.BatchTierLimitItem{validItem}},
		"missing item id":   {Operation: service.BatchSetFixed, Reason: "batch", Items: []service.BatchTierLimitItem{{}}},
		"duplicate item id": {Operation: service.BatchSetFixed, Reason: "batch", Items: []service.BatchTierLimitItem{validItem, validItem}},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc.PreviewBatch(context.Background(), rbac.Actor{IsGlobal: true}, input)
			require.ErrorIs(t, err, service.ErrInvalidBatchRequest)
			var validationErr *service.BatchValidationError
			require.ErrorAs(t, err, &validationErr)
			require.NotEmpty(t, validationErr.Field)
		})
	}
}

func TestBatchTierLimitDTOUsesFrozenFieldNames(t *testing.T) {
	raw, err := json.Marshal(service.BatchTierLimitItem{
		ItemID: "row-1", AllocationID: uuid.New(), TierID: uuid.New(),
		PeriodType: budgetdomain.PeriodMonthly, Direction: "increase", Value: mustAmount(t, "10"),
	})
	require.NoError(t, err)
	require.Contains(t, string(raw), `"model_tier_id"`)
	require.NotContains(t, string(raw), `"tier_id"`)
}

func TestBatchTierLimitApplyWritesOnlyConfirmedValidItems(t *testing.T) {
	allocationID, tierID := uuid.New(), uuid.New()
	store := &fakeBudgetTierStore{existing: &model.BudgetTierLimit{
		ID: uuid.New(), BudgetAllocationID: allocationID, ModelTierID: tierID,
		PeriodType: budgetdomain.PeriodMonthly, LimitUSD: mustAmount(t, "80"), Enabled: true,
		UpdatedAt: time.Date(2026, 7, 11, 7, 0, 0, 0, time.UTC),
	}}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(store, fakeAllocationReader{&model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeCompany,
	}}, tierRepo, func() time.Time { return time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC) })
	preview, err := svc.PreviewBatch(context.Background(), rbac.Actor{UserID: "admin", IsGlobal: true}, service.BatchTierLimitInput{
		Operation: service.BatchAdjustPercent,
		Items: []service.BatchTierLimitItem{{
			ItemID: "row-1", AllocationID: allocationID, TierID: tierID, PeriodType: budgetdomain.PeriodMonthly,
			Direction: "increase", Value: mustAmount(t, "10"),
		}},
		Reason: "统一调整",
	})
	require.NoError(t, err)
	got, err := svc.ApplyBatch(context.Background(), rbac.Actor{UserID: "admin", IsGlobal: true}, service.ApplyBatchTierLimitInput{
		PreviewToken:   preview.PreviewToken,
		ConfirmItemIDs: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "skipped", got.Items[0].Status)
	require.Equal(t, "80.000000", store.existing.LimitUSD.String())
}

func TestStudioManagerCanApplyTierLimitToOwnStudioAllocation(t *testing.T) {
	studioID := uuid.NewString()
	allocationID, tierID := uuid.New(), uuid.New()
	store := &fakeBudgetTierStore{}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(store, fakeAllocationReader{&model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeStudio, ScopeID: studioID,
	}}, tierRepo, func() time.Time { return time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC) })
	actor := rbac.Actor{UserID: uuid.NewString(), StudioIDs: []string{studioID}}

	preview, err := svc.PreviewBatch(context.Background(), actor, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed,
		Items: []service.BatchTierLimitItem{{
			ItemID: "studio-premium-monthly", AllocationID: allocationID, TierID: tierID,
			PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "120"),
		}},
		Reason: "事业部配置高级等级子预算",
	})
	require.NoError(t, err)
	require.Equal(t, "valid", preview.Items[0].Status)

	result, err := svc.ApplyBatch(context.Background(), actor, service.ApplyBatchTierLimitInput{
		PreviewToken: preview.PreviewToken, ConfirmItemIDs: []string{"studio-premium-monthly"},
	})
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Equal(t, allocationID, store.existing.BudgetAllocationID)
	require.Equal(t, tierID, store.existing.ModelTierID)
	require.Equal(t, "120.000000", store.existing.LimitUSD.String())
}

func TestStudioManagerCannotApplyTierLimitToOtherStudioAllocation(t *testing.T) {
	allocationID, tierID := uuid.New(), uuid.New()
	store := &fakeBudgetTierStore{}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(store, fakeAllocationReader{&model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeStudio, ScopeID: uuid.NewString(),
	}}, tierRepo, time.Now)
	actor := rbac.Actor{UserID: uuid.NewString(), StudioIDs: []string{uuid.NewString()}}

	preview, err := svc.PreviewBatch(context.Background(), actor, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed,
		Items: []service.BatchTierLimitItem{{
			ItemID: "other-studio", AllocationID: allocationID, TierID: tierID,
			PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "120"),
		}},
		Reason: "不能配置其他事业部",
	})
	require.NoError(t, err)
	require.Equal(t, "forbidden", preview.Items[0].Status)
	require.Equal(t, "ORG_FORBIDDEN", preview.Items[0].ErrorCode)
}

func TestBatchTierLimitApplyRejectsStaleUsageWithoutConfirmationBypass(t *testing.T) {
	allocationID, tierID := uuid.New(), uuid.New()
	store := &fakeBudgetTierStore{}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(store, fakeAllocationReader{&model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
	}}, tierRepo, time.Now).WithRedis(redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1", DialTimeout: 10 * time.Millisecond, MaxRetries: 0,
	}))
	preview, err := svc.PreviewBatch(context.Background(), rbac.Actor{UserID: "admin", IsGlobal: true}, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed, EffectiveMode: service.EffectiveImmediate, Reason: "stale must retry",
		Items: []service.BatchTierLimitItem{{
			ItemID: "row-stale", AllocationID: allocationID, TierID: tierID,
			PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "50"),
		}},
	})
	require.NoError(t, err)
	require.True(t, preview.Stale)

	_, err = svc.ApplyBatch(context.Background(), rbac.Actor{UserID: "admin", IsGlobal: true}, service.ApplyBatchTierLimitInput{
		PreviewToken: preview.PreviewToken, ConfirmItemIDs: []string{"row-stale"},
	})
	require.ErrorIs(t, err, service.ErrBatchPreviewStale)
}

func TestBatchFixedLimitConvertsSharedMemberToPersonalAllocation(t *testing.T) {
	tierID := uuid.New()
	store := &fakeBudgetTierStore{}
	allocations := &sharedMemberAllocationStore{}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	svc := service.NewBudgetTierService(store, allocations, tierRepo, func() time.Time { return now })
	actor := rbac.Actor{UserID: uuid.NewString(), DeptIDs: []string{"dept-a"}}

	preview, err := svc.PreviewBatch(context.Background(), actor, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed,
		Items: []service.BatchTierLimitItem{{
			ItemID: "member-a", ScopeType: model.BudgetScopeMember, ScopeID: "member-a",
			TierID: tierID, PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "50"),
		}},
		Reason: "为共享池成员设置个人额度",
	})
	require.NoError(t, err)
	require.True(t, preview.Items[0].ConvertToPersonal)
	require.Zero(t, allocations.upserts)

	result, err := svc.ApplyBatch(context.Background(), actor, service.ApplyBatchTierLimitInput{
		PreviewToken: preview.PreviewToken, ConfirmItemIDs: []string{"member-a"},
	})
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Equal(t, 1, allocations.upserts)
	require.Equal(t, model.BudgetScopeMember, allocations.member.ScopeType)
	require.Equal(t, "member-a", allocations.member.ScopeID)
	require.Equal(t, allocations.member.ID, allocations.tierParams.BudgetAllocationID)
	require.Equal(t, "50.000000", allocations.tierParams.LimitUSD.String())
}

func TestBatchSharedMemberConversionDoesNotPersistAllocationWhenTierWriteFails(t *testing.T) {
	tierID := uuid.New()
	allocations := &sharedMemberAllocationStore{convertErr: errors.New("db exploded")}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(&fakeBudgetTierStore{}, allocations, tierRepo, time.Now)
	actor := rbac.Actor{UserID: uuid.NewString(), DeptIDs: []string{"dept-a"}}
	preview, err := svc.PreviewBatch(context.Background(), actor, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed, Reason: "personal limit",
		Items: []service.BatchTierLimitItem{{
			ItemID: "member-a", ScopeType: model.BudgetScopeMember, ScopeID: "member-a",
			TierID: tierID, PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "50"),
		}},
	})
	require.NoError(t, err)
	result, err := svc.ApplyBatch(context.Background(), actor, service.ApplyBatchTierLimitInput{
		PreviewToken: preview.PreviewToken, ConfirmItemIDs: []string{"member-a"},
	})
	require.NoError(t, err)
	require.Equal(t, "failed", result.Items[0].Status)
	require.Nil(t, allocations.member)
	require.Zero(t, allocations.upserts)
	require.NotEqual(t, "db exploded", result.Items[0].ErrorCode)
}

func TestBatchApplyMarksPersistedItemsWhenSnapshotSyncIsPending(t *testing.T) {
	allocationID, tierID := uuid.New(), uuid.New()
	store := &fakeBudgetTierStore{}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(
		store,
		fakeAllocationReader{&model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany}},
		tierRepo,
		time.Now,
	).WithSnapshotPublisher(failingBudgetPublisher{})
	actor := rbac.Actor{UserID: uuid.NewString(), IsGlobal: true}
	preview, err := svc.PreviewBatch(context.Background(), actor, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed,
		Items: []service.BatchTierLimitItem{{
			ItemID: "row-1", AllocationID: allocationID, TierID: tierID,
			PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "100"),
		}},
		Reason: "persist before snapshot",
	})
	require.NoError(t, err)

	result, err := svc.ApplyBatch(context.Background(), actor, service.ApplyBatchTierLimitInput{
		PreviewToken: preview.PreviewToken, ConfirmItemIDs: []string{"row-1"},
	})
	require.NoError(t, err)
	require.True(t, result.SyncPending)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Equal(t, "BUDGET_CONFIG_SYNC_PENDING", result.Items[0].ErrorCode)
	require.NotEmpty(t, result.AuditAfter)
	require.Equal(t, "100.000000", store.existing.LimitUSD.String())
}

func TestBatchApplyPublishesSnapshotOnceAfterAllWrites(t *testing.T) {
	allocationID, tierID := uuid.New(), uuid.New()
	store := &fakeBudgetTierStore{}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	publisher := &countingBudgetPublisher{}
	svc := service.NewBudgetTierService(
		store,
		fakeAllocationReader{&model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany}},
		tierRepo,
		time.Now,
	).WithSnapshotPublisher(publisher)
	actor := rbac.Actor{UserID: uuid.NewString(), IsGlobal: true}
	preview, err := svc.PreviewBatch(context.Background(), actor, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed, Reason: "single snapshot",
		Items: []service.BatchTierLimitItem{
			{ItemID: "daily", AllocationID: allocationID, TierID: tierID, PeriodType: budgetdomain.PeriodDaily, Value: mustAmount(t, "10")},
			{ItemID: "monthly", AllocationID: allocationID, TierID: tierID, PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "100")},
		},
	})
	require.NoError(t, err)
	result, err := svc.ApplyBatch(context.Background(), actor, service.ApplyBatchTierLimitInput{
		PreviewToken: preview.PreviewToken, ConfirmItemIDs: []string{"daily", "monthly"},
	})
	require.NoError(t, err)
	require.Equal(t, "applied", result.Items[0].Status)
	require.Equal(t, "applied", result.Items[1].Status)
	require.Equal(t, 1, publisher.Calls())
}

func TestBatchTierLimitPreviewUsesCanonicalSharedPoolConversionField(t *testing.T) {
	raw, err := json.Marshal(service.BatchTierLimitPreviewItem{
		ItemID:            "row-1",
		ConvertToPersonal: true,
	})
	require.NoError(t, err)
	require.Contains(t, string(raw), `"converts_shared_member_to_independent":true`)
	require.NotContains(t, string(raw), `"convert_to_personal"`)
}

func TestBatchPreviewUsesParentTierLimitAndBindsParentVersion(t *testing.T) {
	allocationID, tierID := uuid.New(), uuid.New()
	allocations := &sharedMemberAllocationStore{
		member: &model.BudgetAllocation{
			ID: allocationID, ScopeType: model.BudgetScopeMember, ScopeID: "member-a",
			AmountUSD: 300, UpdatedAt: time.Date(2026, 7, 11, 7, 0, 0, 0, time.UTC),
		},
		risk: repository.BatchTierRiskSnapshot{
			ParentLimitUSD:       amountPtr(t, "100"),
			SiblingLimitTotalUSD: mustAmount(t, "90"),
			ParentVersion:        "parent-v1",
		},
	}
	store := &fakeBudgetTierStore{existing: &model.BudgetTierLimit{
		ID: uuid.New(), BudgetAllocationID: allocationID, ModelTierID: tierID,
		PeriodType: budgetdomain.PeriodMonthly, LimitUSD: mustAmount(t, "20"), Enabled: true,
		UpdatedAt: time.Date(2026, 7, 11, 7, 0, 0, 0, time.UTC),
	}}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	svc := service.NewBudgetTierService(store, allocations, tierRepo, func() time.Time { return now })
	actor := rbac.Actor{UserID: uuid.NewString(), DeptIDs: []string{"dept-a"}}

	preview, err := svc.PreviewBatch(context.Background(), actor, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed, Reason: "increase",
		Items: []service.BatchTierLimitItem{{
			ItemID: "row-1", AllocationID: allocationID, TierID: tierID,
			PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "40"),
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "1.100000", preview.Items[0].OverAllocationRatio)

	allocations.risk.ParentVersion = "parent-v2"
	applied, err := svc.ApplyBatch(context.Background(), actor, service.ApplyBatchTierLimitInput{
		PreviewToken: preview.PreviewToken, ConfirmItemIDs: []string{"row-1"},
	})
	require.NoError(t, err)
	require.Equal(t, "failed", applied.Items[0].Status)
	require.Equal(t, "BUDGET_PREVIEW_STALE", applied.Items[0].ErrorCode)
	require.Equal(t, "20.000000", store.existing.LimitUSD.String())
}

func TestBatchApplyRechecksVersionUnderWriteTransactionLock(t *testing.T) {
	allocationID, tierID := uuid.New(), uuid.New()
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	store := &atomicBudgetTierStore{fakeBudgetTierStore: fakeBudgetTierStore{existing: &model.BudgetTierLimit{
		ID: uuid.New(), BudgetAllocationID: allocationID, ModelTierID: tierID,
		PeriodType: budgetdomain.PeriodMonthly, LimitUSD: mustAmount(t, "20"), Enabled: true, UpdatedAt: now.Add(-time.Hour),
	}}}
	allocations := &atomicAllocationStore{allocation: &model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeCompany, Enabled: true, UpdatedAt: now.Add(-time.Hour),
	}}
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(store, allocations, tierRepo, func() time.Time { return now })
	actor := rbac.Actor{UserID: "admin", IsGlobal: true}
	preview, err := svc.PreviewBatch(context.Background(), actor, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed, Reason: "race",
		Items: []service.BatchTierLimitItem{{
			ItemID: "row-1", AllocationID: allocationID, TierID: tierID,
			PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "40"),
		}},
	})
	require.NoError(t, err)
	store.beforeTx = func() {
		store.existing.LimitUSD = mustAmount(t, "30")
		store.existing.UpdatedAt = now
	}
	result, err := svc.ApplyBatch(context.Background(), actor, service.ApplyBatchTierLimitInput{
		PreviewToken: preview.PreviewToken, ConfirmItemIDs: []string{"row-1"},
	})
	require.NoError(t, err)
	require.Equal(t, "failed", result.Items[0].Status)
	require.Equal(t, "BUDGET_PREVIEW_STALE", result.Items[0].ErrorCode)
	require.Zero(t, store.writes)
}

func TestBatchPreviewCacheExpiresAndEvictsOldestDeterministically(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	allocationID, tierID := uuid.New(), uuid.New()
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(
		&fakeBudgetTierStore{},
		fakeAllocationReader{&model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany}},
		tierRepo,
		func() time.Time { return now },
	).WithPreviewCacheLimits(2, time.Minute)
	actor := rbac.Actor{UserID: "admin", IsGlobal: true}
	preview := func(itemID string) *service.BatchTierLimitPreview {
		got, err := svc.PreviewBatch(context.Background(), actor, service.BatchTierLimitInput{
			Operation: service.BatchSetFixed, Reason: "cache",
			Items: []service.BatchTierLimitItem{{
				ItemID: itemID, AllocationID: allocationID, TierID: tierID,
				PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "10"),
			}},
		})
		require.NoError(t, err)
		return got
	}
	first := preview("first")
	now = now.Add(time.Second)
	_ = preview("second")
	now = now.Add(time.Second)
	_ = preview("third")
	require.Equal(t, 2, svc.PreviewCacheSize())
	_, err := svc.ApplyBatch(context.Background(), actor, service.ApplyBatchTierLimitInput{PreviewToken: first.PreviewToken})
	require.ErrorIs(t, err, service.ErrBatchPreviewInvalid)

	expiring := preview("expiring")
	now = now.Add(2 * time.Minute)
	_ = preview("cleanup-trigger")
	_, err = svc.ApplyBatch(context.Background(), actor, service.ApplyBatchTierLimitInput{PreviewToken: expiring.PreviewToken})
	require.ErrorIs(t, err, service.ErrBatchPreviewInvalid)
}

func TestBatchPreviewCacheCapacityIsRaceSafe(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	allocationID, tierID := uuid.New(), uuid.New()
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(
		&fakeBudgetTierStore{},
		fakeAllocationReader{&model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany}},
		tierRepo,
		func() time.Time { return now },
	).WithPreviewCacheLimits(10, time.Minute)
	actor := rbac.Actor{UserID: "admin", IsGlobal: true}
	var wg sync.WaitGroup
	for index := 0; index < 100; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := svc.PreviewBatch(context.Background(), actor, service.BatchTierLimitInput{
				Operation: service.BatchSetFixed, Reason: "cache",
				Items: []service.BatchTierLimitItem{{
					ItemID: fmt.Sprintf("item-%03d", index), AllocationID: allocationID, TierID: tierID,
					PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "10"),
				}},
			})
			require.NoError(t, err)
		}(index)
	}
	wg.Wait()
	require.LessOrEqual(t, svc.PreviewCacheSize(), 10)
}

func TestBatchPreviewCacheJanitorProactivelyRemovesExpiredEntries(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	allocationID, tierID := uuid.New(), uuid.New()
	tierRepo := newFakeModelTierRepo()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: true}
	svc := service.NewBudgetTierService(
		&fakeBudgetTierStore{},
		fakeAllocationReader{&model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany}},
		tierRepo,
		func() time.Time { return now },
	).WithPreviewCacheLimits(10, time.Minute)
	_, err := svc.PreviewBatch(context.Background(), rbac.Actor{UserID: "admin", IsGlobal: true}, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed, Reason: "janitor",
		Items: []service.BatchTierLimitItem{{
			ItemID: "expired", AllocationID: allocationID, TierID: tierID,
			PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount(t, "10"),
		}},
	})
	require.NoError(t, err)
	require.Equal(t, 1, svc.CachedPreviewCount())
	now = now.Add(2 * time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.StartPreviewCacheCleanup(ctx, 5*time.Millisecond)
	require.Eventually(t, func() bool { return svc.CachedPreviewCount() == 0 }, time.Second, 10*time.Millisecond)
}

func amountPtr(t *testing.T, value string) *budgetdomain.Amount {
	t.Helper()
	amount := mustAmount(t, value)
	return &amount
}

func nowPlus(t *testing.T, duration time.Duration) time.Time {
	t.Helper()
	return time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC).Add(duration)
}
