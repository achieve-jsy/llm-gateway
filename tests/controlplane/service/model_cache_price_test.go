package service_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cachePriceModelRepo struct {
	model *model.Model
}

func (r *cachePriceModelRepo) Create(*model.Model) error        { return nil }
func (r *cachePriceModelRepo) BatchCreate([]*model.Model) error { return nil }
func (r *cachePriceModelRepo) GetByID(uuid.UUID) (*model.Model, error) {
	clone := *r.model
	return &clone, nil
}
func (r *cachePriceModelRepo) GetByIDs([]uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return nil, nil
}
func (r *cachePriceModelRepo) List(repository.ModelListFilter, int, int) ([]model.Model, int64, error) {
	return nil, 0, nil
}
func (r *cachePriceModelRepo) Update(m *model.Model) error                  { r.model = m; return nil }
func (r *cachePriceModelRepo) Delete(uuid.UUID) error                       { return nil }
func (r *cachePriceModelRepo) ExistsByName(string, uuid.UUID) (bool, error) { return false, nil }
func (r *cachePriceModelRepo) ExistsByNameExcluding(string, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (r *cachePriceModelRepo) CountByProviderID(uuid.UUID) (int64, error) { return 0, nil }
func (r *cachePriceModelRepo) BatchCountByProviderID([]uuid.UUID) (map[uuid.UUID]int64, error) {
	return nil, nil
}
func (r *cachePriceModelRepo) ListAvailable() ([]model.Model, error)               { return nil, nil }
func (r *cachePriceModelRepo) UpdateEnabledByProviderID(uuid.UUID, bool) error     { return nil }
func (r *cachePriceModelRepo) BatchDelete([]uuid.UUID) (int64, error)              { return 0, nil }
func (r *cachePriceModelRepo) BatchUpdateEnabled([]uuid.UUID, bool) (int64, error) { return 0, nil }
func (r *cachePriceModelRepo) DeleteModelTagRelations(uuid.UUID) error             { return nil }
func (r *cachePriceModelRepo) GetHealthByNames([]string) (map[string]repository.ModelHealthSummary, error) {
	return nil, nil
}
func (r *cachePriceModelRepo) UpdateEnabled(uuid.UUID, *bool) error                { return nil }
func (r *cachePriceModelRepo) UpdateStatus(uuid.UUID, bool, int) error             { return nil }
func (r *cachePriceModelRepo) AutoDisableModel(uuid.UUID) error                    { return nil }
func (r *cachePriceModelRepo) AutoEnableModel(uuid.UUID) error                     { return nil }
func (r *cachePriceModelRepo) IncrementModelFailures(uuid.UUID) (int, error)       { return 0, nil }
func (r *cachePriceModelRepo) ResetModelFailures(uuid.UUID) error                  { return nil }
func (r *cachePriceModelRepo) UpdateModelCheckResult(uuid.UUID, int, string) error { return nil }
func (r *cachePriceModelRepo) IncrementRecoverySuccesses(uuid.UUID) error          { return nil }
func (r *cachePriceModelRepo) ResetRecoverySuccesses(uuid.UUID) error              { return nil }

type cachePriceProviderRepo struct{}

func (r *cachePriceProviderRepo) Create(*model.Provider) error { return nil }
func (r *cachePriceProviderRepo) GetByID(uuid.UUID) (*model.Provider, error) {
	return &model.Provider{Enabled: true}, nil
}
func (r *cachePriceProviderRepo) List(repository.ProviderListFilter, int, int) ([]model.Provider, int64, error) {
	return nil, 0, nil
}
func (r *cachePriceProviderRepo) Update(*model.Provider) error       { return nil }
func (r *cachePriceProviderRepo) UpdateStatus(uuid.UUID, bool) error { return nil }
func (r *cachePriceProviderRepo) Delete(uuid.UUID) error             { return nil }
func (r *cachePriceProviderRepo) ExistsByName(string) (bool, error)  { return false, nil }
func (r *cachePriceProviderRepo) ExistsByNameExcluding(string, uuid.UUID) (bool, error) {
	return false, nil
}
func (r *cachePriceProviderRepo) UpdateResponseTime(uuid.UUID, int) error { return nil }

func TestUpdateModel_ClearsCachePricesWhenNullProvided(t *testing.T) {
	read := 0.1
	write := 0.2
	modelID := uuid.New()
	repo := &cachePriceModelRepo{model: &model.Model{
		ID: modelID, Name: "m", ProviderID: uuid.New(), ModelType: "chat",
		PriceMode: "manual", InputPrice: 1, OutputPrice: 2,
		CacheReadPrice: &read, CacheWritePrice: &write,
	}}
	svc := service.NewModelService(repo, &cachePriceProviderRepo{}, nil)

	input := service.UpdateModelInput{PriceMode: "manual"}
	input.CacheReadPrice.Set = true
	input.CacheWritePrice.Set = true

	updated, err := svc.UpdateModel(modelID, input)
	require.NoError(t, err)
	assert.Nil(t, updated.CacheReadPrice)
	assert.Nil(t, updated.CacheWritePrice)
}
