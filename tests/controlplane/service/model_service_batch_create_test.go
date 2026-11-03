package service_test

import (
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// 精简 fake：仅覆盖 BatchCreate 所需方法，其余返回零值/nil。

type batchFakeModelRepo struct {
	created   []*model.Model
	exists    map[string]bool // name|providerID
	createErr error
}

func (f *batchFakeModelRepo) key(name string, providerID uuid.UUID) string {
	return name + "|" + providerID.String()
}

func (f *batchFakeModelRepo) Create(m *model.Model) error {
	if f.createErr != nil {
		return f.createErr
	}
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	f.created = append(f.created, m)
	if f.exists == nil {
		f.exists = map[string]bool{}
	}
	f.exists[f.key(m.Name, m.ProviderID)] = true
	return nil
}
func (f *batchFakeModelRepo) GetByID(id uuid.UUID) (*model.Model, error) {
	return nil, errors.New("not found")
}
func (f *batchFakeModelRepo) GetByIDs(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return nil, nil
}
func (f *batchFakeModelRepo) List(filter repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error) {
	return nil, 0, nil
}
func (f *batchFakeModelRepo) Update(m *model.Model) error { return nil }
func (f *batchFakeModelRepo) Delete(id uuid.UUID) error   { return nil }
func (f *batchFakeModelRepo) ExistsByName(name string, providerID uuid.UUID) (bool, error) {
	return f.exists[f.key(name, providerID)], nil
}
func (f *batchFakeModelRepo) ExistsByNameExcluding(name string, providerID uuid.UUID, excludeID uuid.UUID) (bool, error) {
	return false, nil
}
func (f *batchFakeModelRepo) CountByProviderID(providerID uuid.UUID) (int64, error) { return 0, nil }
func (f *batchFakeModelRepo) BatchCountByProviderID(ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	return map[uuid.UUID]int64{}, nil
}
func (f *batchFakeModelRepo) ListAvailable() ([]model.Model, error) { return nil, nil }
func (f *batchFakeModelRepo) UpdateEnabledByProviderID(providerID uuid.UUID, enabled bool) error {
	return nil
}
func (f *batchFakeModelRepo) BatchDelete(ids []uuid.UUID) (int64, error) { return 0, nil }
func (f *batchFakeModelRepo) BatchUpdateEnabled(ids []uuid.UUID, enabled bool) (int64, error) {
	return 0, nil
}
func (f *batchFakeModelRepo) DeleteModelTagRelations(modelID uuid.UUID) error { return nil }
func (f *batchFakeModelRepo) GetHealthByNames(names []string) (map[string]repository.ModelHealthSummary, error) {
	return nil, nil
}
func (f *batchFakeModelRepo) UpdateEnabled(id uuid.UUID, enabled *bool) error { return nil }
func (f *batchFakeModelRepo) UpdateStatus(id uuid.UUID, enabled bool, status int) error {
	return nil
}
func (f *batchFakeModelRepo) AutoDisableModel(id uuid.UUID) error              { return nil }
func (f *batchFakeModelRepo) AutoEnableModel(id uuid.UUID) error               { return nil }
func (f *batchFakeModelRepo) IncrementModelFailures(id uuid.UUID) (int, error) { return 0, nil }
func (f *batchFakeModelRepo) ResetModelFailures(id uuid.UUID) error            { return nil }
func (f *batchFakeModelRepo) UpdateModelCheckResult(id uuid.UUID, responseTime int, checkError string) error {
	return nil
}
func (f *batchFakeModelRepo) IncrementRecoverySuccesses(id uuid.UUID) error { return nil }
func (f *batchFakeModelRepo) ResetRecoverySuccesses(id uuid.UUID) error     { return nil }

type batchFakeProviderRepo struct {
	providers map[uuid.UUID]*model.Provider
}

func (f *batchFakeProviderRepo) Create(provider *model.Provider) error { return nil }
func (f *batchFakeProviderRepo) GetByID(id uuid.UUID) (*model.Provider, error) {
	p, ok := f.providers[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return p, nil
}
func (f *batchFakeProviderRepo) List(filter repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
	return nil, 0, nil
}
func (f *batchFakeProviderRepo) Update(provider *model.Provider) error         { return nil }
func (f *batchFakeProviderRepo) UpdateStatus(id uuid.UUID, enabled bool) error { return nil }
func (f *batchFakeProviderRepo) Delete(id uuid.UUID) error                     { return nil }
func (f *batchFakeProviderRepo) ExistsByName(name string) (bool, error)        { return false, nil }
func (f *batchFakeProviderRepo) ExistsByNameExcluding(name string, excludeID uuid.UUID) (bool, error) {
	return false, nil
}
func (f *batchFakeProviderRepo) UpdateResponseTime(id uuid.UUID, ms int) error { return nil }

type batchFakeCatalogLookup struct {
	byID map[uuid.UUID]*model.ModelCatalog
}

func (f *batchFakeCatalogLookup) GetByID(id uuid.UUID) (*model.ModelCatalog, error) {
	return f.byID[id], nil
}
func (f *batchFakeCatalogLookup) GetByIDs(ids []uuid.UUID) (map[uuid.UUID]*model.ModelCatalog, error) {
	return nil, nil
}

func TestBatchCreateModels_AllSuccessAndDedupe(t *testing.T) {
	p1, p2 := uuid.New(), uuid.New()
	modelRepo := &batchFakeModelRepo{}
	providerRepo := &batchFakeProviderRepo{providers: map[uuid.UUID]*model.Provider{
		p1: {ID: p1, Name: "a", Enabled: true},
		p2: {ID: p2, Name: "b", Enabled: true},
	}}
	svc := service.NewModelService(modelRepo, providerRepo, nil)

	res, err := svc.BatchCreateModels(service.BatchCreateModelsInput{
		Name:        "gpt-4o",
		ProviderIDs: []string{p1.String(), p1.String(), p2.String()},
		ModelType:   "chat",
	})
	require.NoError(t, err)
	require.Equal(t, 2, res.SuccessCount)
	require.Equal(t, 0, res.SkipCount)
	require.Equal(t, 0, res.FailCount)
	require.Len(t, res.Results, 2)
	require.Len(t, modelRepo.created, 2)
}

func TestBatchCreateModels_PartialSkipAndFail(t *testing.T) {
	pOK, pExists, pMissing := uuid.New(), uuid.New(), uuid.New()
	modelRepo := &batchFakeModelRepo{exists: map[string]bool{
		"gpt-4o|" + pExists.String(): true,
	}}
	providerRepo := &batchFakeProviderRepo{providers: map[uuid.UUID]*model.Provider{
		pOK:     {ID: pOK, Name: "ok", Enabled: true},
		pExists: {ID: pExists, Name: "exists", Enabled: true},
	}}
	svc := service.NewModelService(modelRepo, providerRepo, nil)

	res, err := svc.BatchCreateModels(service.BatchCreateModelsInput{
		Name:        "gpt-4o",
		ProviderIDs: []string{pOK.String(), pExists.String(), pMissing.String()},
		ModelType:   "chat",
	})
	require.NoError(t, err)
	require.Equal(t, 1, res.SuccessCount)
	require.Equal(t, 1, res.SkipCount)
	require.Equal(t, 1, res.FailCount)
	require.Equal(t, service.BatchCreateStatusCreated, res.Results[0].Status)
	require.Equal(t, service.BatchCreateStatusSkipped, res.Results[1].Status)
	require.Equal(t, service.BatchCreateStatusFailed, res.Results[2].Status)
}

func TestBatchCreateModels_EmptyProviderIDs(t *testing.T) {
	svc := service.NewModelService(&batchFakeModelRepo{}, &batchFakeProviderRepo{}, nil)
	_, err := svc.BatchCreateModels(service.BatchCreateModelsInput{
		Name:        "x",
		ProviderIDs: nil,
		ModelType:   "chat",
	})
	require.ErrorIs(t, err, service.ErrEmptyProviderIDs)
}

func TestBatchCreateModelsFromCatalog_CatalogMissing(t *testing.T) {
	p1 := uuid.New()
	svc := service.NewModelService(&batchFakeModelRepo{}, &batchFakeProviderRepo{
		providers: map[uuid.UUID]*model.Provider{p1: {ID: p1, Enabled: true}},
	}, nil)
	svc.SetCatalogLookup(&batchFakeCatalogLookup{byID: map[uuid.UUID]*model.ModelCatalog{}})

	_, err := svc.BatchCreateModelsFromCatalog(service.BatchCreateFromCatalogInput{
		CatalogID:   uuid.New(),
		ProviderIDs: []string{p1.String()},
		Name:        "gpt-4o",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestBatchCreateModelsFromCatalog_PartialSuccess(t *testing.T) {
	pOK, pExists := uuid.New(), uuid.New()
	catID := uuid.New()
	modelRepo := &batchFakeModelRepo{exists: map[string]bool{
		"gpt-4o|" + pExists.String(): true,
	}}
	providerRepo := &batchFakeProviderRepo{providers: map[uuid.UUID]*model.Provider{
		pOK:     {ID: pOK, Enabled: true},
		pExists: {ID: pExists, Enabled: true},
	}}
	svc := service.NewModelService(modelRepo, providerRepo, nil)
	svc.SetCatalogLookup(&batchFakeCatalogLookup{byID: map[uuid.UUID]*model.ModelCatalog{
		catID: {
			ID:                    catID,
			SourceModelID:         "gpt-4o",
			DisplayName:           "GPT-4o",
			Mode:                  "chat",
			ContextLength:         128000,
			MaxOutputTokens:       4096,
			InputPricePerMillion:  2.5,
			OutputPricePerMillion: 10,
		},
	}})

	res, err := svc.BatchCreateModelsFromCatalog(service.BatchCreateFromCatalogInput{
		CatalogID:   catID,
		ProviderIDs: []string{pOK.String(), pExists.String()},
		Name:        "gpt-4o",
	})
	require.NoError(t, err)
	require.Equal(t, 1, res.SuccessCount)
	require.Equal(t, 1, res.SkipCount)
	require.Equal(t, 0, res.FailCount)
	require.Len(t, modelRepo.created, 1)
	require.NotNil(t, modelRepo.created[0].CatalogID)
	require.Equal(t, catID, *modelRepo.created[0].CatalogID)
}
