package service

import (
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

type plannerFieldModelRepo struct {
	stored *model.Model
}

func (r *plannerFieldModelRepo) Create(m *model.Model) error { r.stored = m; return nil }
func (r *plannerFieldModelRepo) GetByID(id uuid.UUID) (*model.Model, error) {
	if r.stored == nil {
		return nil, errors.New("not found")
	}
	return r.stored, nil
}
func (r *plannerFieldModelRepo) GetByIDs(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return nil, nil
}
func (r *plannerFieldModelRepo) List(filter repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error) {
	return nil, 0, nil
}
func (r *plannerFieldModelRepo) Update(m *model.Model) error { r.stored = m; return nil }
func (r *plannerFieldModelRepo) Delete(id uuid.UUID) error   { return nil }
func (r *plannerFieldModelRepo) ExistsByName(name string, providerID uuid.UUID) (bool, error) {
	return false, nil
}
func (r *plannerFieldModelRepo) ExistsByNameExcluding(name string, providerID uuid.UUID, excludeID uuid.UUID) (bool, error) {
	return false, nil
}
func (r *plannerFieldModelRepo) CountByProviderID(providerID uuid.UUID) (int64, error) { return 0, nil }
func (r *plannerFieldModelRepo) BatchCountByProviderID(providerIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	return nil, nil
}
func (r *plannerFieldModelRepo) ListAvailable() ([]model.Model, error) { return nil, nil }
func (r *plannerFieldModelRepo) UpdateEnabledByProviderID(providerID uuid.UUID, enabled bool) error {
	return nil
}
func (r *plannerFieldModelRepo) BatchDelete(ids []uuid.UUID) (int64, error) { return 0, nil }
func (r *plannerFieldModelRepo) BatchUpdateEnabled(ids []uuid.UUID, enabled bool) (int64, error) {
	return 0, nil
}
func (r *plannerFieldModelRepo) DeleteModelTagRelations(modelID uuid.UUID) error { return nil }
func (r *plannerFieldModelRepo) GetHealthByNames(names []string) (map[string]repository.ModelHealthSummary, error) {
	return nil, nil
}
func (r *plannerFieldModelRepo) UpdateEnabled(id uuid.UUID, enabled *bool) error { return nil }
func (r *plannerFieldModelRepo) UpdateStatus(id uuid.UUID, enabled bool, status int) error {
	return nil
}
func (r *plannerFieldModelRepo) AutoDisableModel(id uuid.UUID) error              { return nil }
func (r *plannerFieldModelRepo) AutoEnableModel(id uuid.UUID) error               { return nil }
func (r *plannerFieldModelRepo) IncrementModelFailures(id uuid.UUID) (int, error) { return 0, nil }
func (r *plannerFieldModelRepo) ResetModelFailures(id uuid.UUID) error            { return nil }
func (r *plannerFieldModelRepo) UpdateModelCheckResult(id uuid.UUID, responseTime int, checkError string) error {
	return nil
}
func (r *plannerFieldModelRepo) IncrementRecoverySuccesses(id uuid.UUID) error { return nil }
func (r *plannerFieldModelRepo) ResetRecoverySuccesses(id uuid.UUID) error     { return nil }

type plannerFieldProviderRepo struct{ provider *model.Provider }

func (r *plannerFieldProviderRepo) Create(provider *model.Provider) error { return nil }
func (r *plannerFieldProviderRepo) GetByID(id uuid.UUID) (*model.Provider, error) {
	return r.provider, nil
}
func (r *plannerFieldProviderRepo) List(filter repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
	return nil, 0, nil
}
func (r *plannerFieldProviderRepo) Update(provider *model.Provider) error         { return nil }
func (r *plannerFieldProviderRepo) UpdateStatus(id uuid.UUID, enabled bool) error { return nil }
func (r *plannerFieldProviderRepo) Delete(id uuid.UUID) error                     { return nil }
func (r *plannerFieldProviderRepo) ExistsByName(name string) (bool, error)        { return false, nil }
func (r *plannerFieldProviderRepo) ExistsByNameExcluding(name string, excludeID uuid.UUID) (bool, error) {
	return false, nil
}
func (r *plannerFieldProviderRepo) UpdateResponseTime(id uuid.UUID, ms int) error { return nil }

func TestModelServicePlannerFields_CreateAndReset(t *testing.T) {
	providerID := uuid.New()
	mr := &plannerFieldModelRepo{}
	pr := &plannerFieldProviderRepo{provider: &model.Provider{ID: providerID, Enabled: true}}
	svc := NewModelService(mr, pr, nil)

	created, err := svc.CreateModel(CreateModelInput{
		Name:                   "deployment-a",
		ProviderID:             providerID.String(),
		ModelType:              "chat",
		BaseModel:              "gpt-5",
		ProtocolMode:           model.ProtocolModeOpenAIResponses,
		ParamProfile:           model.ParamProfileOpenAIResponses,
		UnsupportedParamPolicy: model.UnsupportedParamPolicyDrop,
		SupportedParamsOverride: model.PlannerJSONMap{
			"temperature": true,
		},
		TransformOverrides: model.PlannerJSONMap{
			"drop": []any{"temperature"},
		},
	})
	if err != nil {
		t.Fatalf("CreateModel returned error: %v", err)
	}
	if created.BaseModel != "gpt-5" || created.ProtocolMode != model.ProtocolModeOpenAIResponses || created.ParamProfile != model.ParamProfileOpenAIResponses || created.UnsupportedParamPolicy != model.UnsupportedParamPolicyDrop {
		t.Fatalf("planner fields not copied on create: %+v", created)
	}
	if got := created.SupportedParamsOverride["temperature"]; got != true {
		t.Fatalf("supported_params_override not copied, got %#v", got)
	}

	auto := model.ProtocolModeAuto
	profileAuto := model.ParamProfileAuto
	preserve := model.UnsupportedParamPolicyPreserve
	emptyBase := ""
	emptyMap := model.PlannerJSONMap{}
	updated, err := svc.UpdateModel(created.ID, UpdateModelInput{
		BaseModel:               &emptyBase,
		ProtocolMode:            &auto,
		ParamProfile:            &profileAuto,
		UnsupportedParamPolicy:  &preserve,
		SupportedParamsOverride: &emptyMap,
		TransformOverrides:      &emptyMap,
	})
	if err != nil {
		t.Fatalf("UpdateModel returned error: %v", err)
	}
	if updated.BaseModel != "" || updated.ProtocolMode != model.ProtocolModeAuto || updated.ParamProfile != model.ParamProfileAuto || updated.UnsupportedParamPolicy != model.UnsupportedParamPolicyPreserve {
		t.Fatalf("planner fields not resettable: %+v", updated)
	}
	if len(updated.SupportedParamsOverride) != 0 || len(updated.TransformOverrides) != 0 {
		t.Fatalf("planner maps not reset to empty: supported=%v transform=%v", updated.SupportedParamsOverride, updated.TransformOverrides)
	}
}

func TestModelServicePlannerFields_InvalidEnum(t *testing.T) {
	providerID := uuid.New()
	mr := &plannerFieldModelRepo{}
	pr := &plannerFieldProviderRepo{provider: &model.Provider{ID: providerID, Enabled: true}}
	svc := NewModelService(mr, pr, nil)

	_, err := svc.CreateModel(CreateModelInput{
		Name:         "bad",
		ProviderID:   providerID.String(),
		ModelType:    "chat",
		ProtocolMode: "invalid_protocol",
	})
	if !errors.Is(err, ErrModelInvalidPlanningConfig) {
		t.Fatalf("expected ErrModelInvalidPlanningConfig, got %v", err)
	}
}
