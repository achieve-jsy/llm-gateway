package service_test

import (
	"context"
	"sync"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/require"
)

type memSettingsRepo struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemSettingsRepo() *memSettingsRepo {
	return &memSettingsRepo{data: map[string]string{}}
}

func (r *memSettingsRepo) GetValue(key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data[key], nil
}

func (r *memSettingsRepo) GetMultiple(keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := r.data[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func (r *memSettingsRepo) SetMultiple(settings map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, v := range settings {
		r.data[k] = v
	}
	return nil
}

func (r *memSettingsRepo) GetAll() ([]model.SystemConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.SystemConfig, 0, len(r.data))
	for k, v := range r.data {
		out = append(out, model.SystemConfig{Key: k, Value: v})
	}
	return out, nil
}

type fakeEnforcementPublisher struct {
	mu         sync.Mutex
	flags      service.BudgetEnforcementFlags
	publishN   int
	accounting bool
}

func (p *fakeEnforcementPublisher) UpdateEnforcement(flags service.BudgetEnforcementFlags) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flags = flags
	if flags.PositiveEnforcementEnabled || flags.ShadowEnabled {
		p.accounting = true
	}
}

func (p *fakeEnforcementPublisher) MarkDirtyAndPublish(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.publishN++
	return nil
}

func TestSeedBudgetEnforcementDefaultsOnlyFillsMissingKeys(t *testing.T) {
	repo := newMemSettingsRepo()
	_ = repo.SetMultiple(map[string]string{service.SettingKeyBudgetPositiveEnforcement: "true"})
	svc := service.NewSettingsService(repo)
	svc.SeedBudgetEnforcementDefaults(service.BudgetEnforcementSeed{
		MasterEnabled:              true,
		AccountingEnabled:          false,
		PositiveEnforcementEnabled: false,
		ZeroBlockEnabled:           true,
		ShadowEnabled:              false,
		V2TotalEnforcementEnabled:  true,
	})

	got, err := svc.GetSystemSettings(context.Background())
	require.NoError(t, err)
	require.True(t, got.BudgetPositiveEnforcementEnabled, "existing DB value must win")
	require.True(t, got.BudgetZeroBlockEnabled)
	require.True(t, got.BudgetV2TotalEnforcementEnabled)
}

func TestUpdateSettingsRejectsShadowAndPositive(t *testing.T) {
	repo := newMemSettingsRepo()
	pub := &fakeEnforcementPublisher{}
	svc := service.NewSettingsService(repo).WithBudgetEnforcement(pub)

	err := svc.UpdateSettings(context.Background(), &model.SystemSettings{
		SiteName:                         "LLM Gateway",
		BudgetPositiveEnforcementEnabled: true,
		BudgetShadowEnabled:              true,
	})
	require.ErrorIs(t, err, service.ErrBudgetShadowAndPositive)
	require.Equal(t, 0, pub.publishN)
}

func TestUpdateSettingsRejectsWhenMasterDisabled(t *testing.T) {
	repo := newMemSettingsRepo()
	pub := &fakeEnforcementPublisher{}
	svc := service.NewSettingsService(repo).WithBudgetEnforcement(pub)

	err := svc.UpdateSettings(context.Background(), &model.SystemSettings{
		SiteName:                         "LLM Gateway",
		BudgetMasterEnabled:              false,
		BudgetPositiveEnforcementEnabled: true,
	})
	require.ErrorIs(t, err, service.ErrBudgetMasterDisabled)
	require.Equal(t, 0, pub.publishN)
}

func TestUpdateSettingsPublishesEnforcementFlags(t *testing.T) {
	repo := newMemSettingsRepo()
	pub := &fakeEnforcementPublisher{}
	svc := service.NewSettingsService(repo).WithBudgetEnforcement(pub)

	err := svc.UpdateSettings(context.Background(), &model.SystemSettings{
		SiteName:                         "LLM Gateway",
		BudgetMasterEnabled:              true,
		BudgetPositiveEnforcementEnabled: true,
		BudgetZeroBlockEnabled:           true,
		BudgetV2TotalEnforcementEnabled:  true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, pub.publishN)
	require.True(t, pub.flags.PositiveEnforcementEnabled)
	require.True(t, pub.flags.ZeroBlockEnabled)
	require.True(t, pub.flags.V2TotalEnforcementEnabled)
	require.False(t, pub.flags.ShadowEnabled)
	require.True(t, pub.accounting)

	got, err := svc.GetSystemSettings(context.Background())
	require.NoError(t, err)
	require.True(t, got.BudgetPositiveEnforcementEnabled)
	require.True(t, got.BudgetZeroBlockEnabled)
	require.True(t, got.BudgetV2TotalEnforcementEnabled)
}

func TestPublisherUpdateEnforcementEnablesAccounting(t *testing.T) {
	p := service.NewBudgetTierSnapshotPublisher(
		nil, nil, nil,
		service.BudgetTierSnapshotSettings{MasterEnabled: true, AccountingEnabled: false},
		nil,
	)
	p.UpdateEnforcement(service.BudgetEnforcementFlags{PositiveEnforcementEnabled: true})
	require.True(t, p.CurrentSettings().AccountingEnabled)
	require.True(t, p.CurrentSettings().PositiveEnforcementEnabled)
}
