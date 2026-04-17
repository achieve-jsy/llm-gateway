package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type failingBudgetPublisher struct{}

func (failingBudgetPublisher) MarkDirtyAndPublish(context.Context) error {
	return errors.New("redis unavailable")
}

type fakeModelTierRepo struct {
	tiers      map[uuid.UUID]*model.ModelTier
	references map[uuid.UUID]repository.ModelTierReferenceCounts
	createErr  error
}

func TestModelTierCreateReturnsSyncPendingAfterDatabaseWrite(t *testing.T) {
	repo := newFakeModelTierRepo()
	tier, err := service.NewModelTierService(repo).WithSnapshotPublisher(failingBudgetPublisher{}).
		Create(context.Background(), service.CreateModelTierInput{Name: "高级", Color: "#7C3AED", Reason: "test"})
	require.ErrorIs(t, err, service.ErrBudgetConfigSyncPending)
	require.NotNil(t, tier)
	require.Contains(t, repo.tiers, tier.ID)
}

func newFakeModelTierRepo() *fakeModelTierRepo {
	return &fakeModelTierRepo{
		tiers:      map[uuid.UUID]*model.ModelTier{},
		references: map[uuid.UUID]repository.ModelTierReferenceCounts{},
	}
}

func (f *fakeModelTierRepo) Create(_ context.Context, tier *model.ModelTier) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.tiers[tier.ID] = tier
	return nil
}
func (f *fakeModelTierRepo) FindByID(_ context.Context, id uuid.UUID) (*model.ModelTier, error) {
	return f.tiers[id], nil
}
func (f *fakeModelTierRepo) List(_ context.Context, _ bool) ([]model.ModelTier, error) {
	out := make([]model.ModelTier, 0, len(f.tiers))
	for _, tier := range f.tiers {
		out = append(out, *tier)
	}
	return out, nil
}
func (f *fakeModelTierRepo) Update(_ context.Context, tier *model.ModelTier) error {
	f.tiers[tier.ID] = tier
	return nil
}
func (f *fakeModelTierRepo) Reorder(_ context.Context, _ []uuid.UUID) error { return nil }
func (f *fakeModelTierRepo) ReferenceCounts(_ context.Context, id uuid.UUID) (repository.ModelTierReferenceCounts, error) {
	return f.references[id], nil
}
func (f *fakeModelTierRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(f.tiers, id)
	return nil
}

func TestModelTierServiceDeleteReferencedTierReturnsConflict(t *testing.T) {
	repo := newFakeModelTierRepo()
	id := uuid.New()
	repo.tiers[id] = &model.ModelTier{ID: id, Name: "高级", Enabled: true}
	repo.references[id] = repository.ModelTierReferenceCounts{Groups: 1}

	err := service.NewModelTierService(repo).Delete(context.Background(), id, "test")
	require.ErrorIs(t, err, service.ErrModelTierReferenced)
}

func TestModelTierServiceDisableReferencedTierReturnsConflict(t *testing.T) {
	repo := newFakeModelTierRepo()
	id := uuid.New()
	repo.tiers[id] = &model.ModelTier{ID: id, Name: "高级", Enabled: true}
	repo.references[id] = repository.ModelTierReferenceCounts{Groups: 1}

	_, err := service.NewModelTierService(repo).SetEnabled(context.Background(), id, false, "test")
	require.ErrorIs(t, err, service.ErrModelTierReferenced)
	require.True(t, repo.tiers[id].Enabled)
}

func TestModelTierServiceEnableReferencedTierSucceeds(t *testing.T) {
	repo := newFakeModelTierRepo()
	id := uuid.New()
	repo.tiers[id] = &model.ModelTier{ID: id, Name: "高级", Enabled: false}
	repo.references[id] = repository.ModelTierReferenceCounts{Groups: 1}

	tier, err := service.NewModelTierService(repo).SetEnabled(context.Background(), id, true, "test")
	require.NoError(t, err)
	require.True(t, tier.Enabled)
}

func TestModelTierListIncludesReferenceAndHistoryCounts(t *testing.T) {
	repo := newFakeModelTierRepo()
	id := uuid.New()
	repo.tiers[id] = &model.ModelTier{ID: id, Name: "高级", Enabled: true}
	repo.references[id] = repository.ModelTierReferenceCounts{
		Groups: 2, Limits: 3, Overrides: 1, Schedules: 1, Alerts: 4, Expansions: 5, CallLogs: 6,
	}
	items, err := service.NewModelTierService(repo).List(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.EqualValues(t, 2, items[0].GroupCount)
	require.EqualValues(t, 5, items[0].BudgetCount)
	require.EqualValues(t, 15, items[0].HistoryCount)
	require.EqualValues(t, 22, items[0].ReferenceCount)
}

func TestModelTierServiceMapsUniqueWriteRaceToStableConflict(t *testing.T) {
	repo := newFakeModelTierRepo()
	repo.createErr = repository.ErrUniqueConflict
	_, err := service.NewModelTierService(repo).Create(context.Background(), service.CreateModelTierInput{
		Name: "高级", Color: "#7C3AED", Reason: "race",
	})
	require.ErrorIs(t, err, service.ErrModelTierNameExists)
}

func TestModelTierServiceValidatesAndArchives(t *testing.T) {
	repo := newFakeModelTierRepo()
	svc := service.NewModelTierService(repo)
	_, err := svc.Create(context.Background(), service.CreateModelTierInput{Name: " ", Color: "#7C3AED", Reason: "test"})
	require.ErrorIs(t, err, service.ErrInvalidModelTier)
	_, err = svc.Create(context.Background(), service.CreateModelTierInput{Name: "高级", Color: "purple", Reason: "test"})
	require.ErrorIs(t, err, service.ErrInvalidModelTier)

	tier, err := svc.Create(context.Background(), service.CreateModelTierInput{Name: " 高级 ", Color: "#7C3AED", Reason: "test"})
	require.NoError(t, err)
	archived, err := svc.Archive(context.Background(), tier.ID, "test")
	require.NoError(t, err)
	require.False(t, archived.Enabled)
	require.NotNil(t, archived.ArchivedAt)
}

func TestModelTierServiceReorderRequiresExactActiveSet(t *testing.T) {
	repo := newFakeModelTierRepo()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	repo.tiers[a] = &model.ModelTier{ID: a}
	repo.tiers[b] = &model.ModelTier{ID: b}
	repo.tiers[c] = &model.ModelTier{ID: c}

	err := service.NewModelTierService(repo).Reorder(context.Background(), []uuid.UUID{a, a, c}, "test")
	require.ErrorIs(t, err, service.ErrInvalidTierOrder)
}

func TestModelTierWritesRequireNonEmptyReason(t *testing.T) {
	repo := newFakeModelTierRepo()
	svc := service.NewModelTierService(repo)

	_, err := svc.Create(context.Background(), service.CreateModelTierInput{Name: "高级", Color: "#7C3AED"})
	require.ErrorIs(t, err, service.ErrReasonRequired)
	require.Empty(t, repo.tiers)
}
