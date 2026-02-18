package service

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

type fakeGroupInfoRepo struct {
	groups []model.Group
}

func (r *fakeGroupInfoRepo) ListEnabledGroupsWithModels() ([]model.Group, error) {
	return r.groups, nil
}

type fakeModelInfoRepo struct {
	models map[uuid.UUID]*model.Model
}

func (r *fakeModelInfoRepo) GetByIDsWithCatalog(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return r.models, nil
}

func newModel(id uuid.UUID, catalog *model.ModelCatalog, enabled bool) *model.Model {
	m := &model.Model{
		ID:      id,
		Catalog: catalog,
	}
	m.Enabled = &enabled
	return m
}

func TestGetGroupModelInfo_NoOverride(t *testing.T) {
	modelID := uuid.New()
	m := newModel(modelID, &model.ModelCatalog{
		SupportsVision:          true,
		SupportsFunctionCalling: true,
		SupportsToolChoice:      false,
		Mode:                    "chat",
		ContextLength:           128000,
		MaxOutputTokens:         8192,
	}, true)

	groups := []model.Group{
		{
			ID:   1,
			Name: "g1",
			Models: []model.GroupModel{
				{ModelID: modelID},
			},
		},
	}

	svc := NewModelInfoService(
		&fakeGroupInfoRepo{groups: groups},
		&fakeModelInfoRepo{models: map[uuid.UUID]*model.Model{modelID: m}},
	)

	result, err := svc.GetGroupModelInfo(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := result["g1"]
	if !info.SupportsVision {
		t.Error("expected supports_vision=true")
	}
	if !info.SupportsFunctionCalling {
		t.Error("expected supports_function_calling=true")
	}
	if info.SupportsToolChoice {
		t.Error("expected supports_tool_choice=false")
	}
	if info.MaxInputTokens != 128000 {
		t.Errorf("expected max_input_tokens=128000, got %d", info.MaxInputTokens)
	}
	if info.IsCapabilityOverride {
		t.Error("expected is_capability_override=false")
	}
}

func TestGetGroupModelInfo_WithOverride(t *testing.T) {
	modelID := uuid.New()
	m := newModel(modelID, &model.ModelCatalog{
		SupportsVision:          true,
		SupportsFunctionCalling: true,
		SupportsToolChoice:      false,
		Mode:                    "chat",
		ContextLength:           128000,
		MaxOutputTokens:         8192,
		InputPricePerMillion:    1.5,
		OutputPricePerMillion:   3.0,
	}, true)

	groups := []model.Group{
		{
			ID:                     1,
			Name:                   "g1",
			IsCapabilityOverride:   true,
			OverrideSupportsVision: false,
			OverrideSupportsFC:     true,
			OverrideSupportsTC:     true,
			Models: []model.GroupModel{
				{ModelID: modelID},
			},
		},
	}

	svc := NewModelInfoService(
		&fakeGroupInfoRepo{groups: groups},
		&fakeModelInfoRepo{models: map[uuid.UUID]*model.Model{modelID: m}},
	)

	result, err := svc.GetGroupModelInfo(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := result["g1"]

	if info.Mode != "chat" {
		t.Errorf("expected mode=chat (from catalog, not overridden), got %s", info.Mode)
	}
	if info.MaxInputTokens != 128000 {
		t.Errorf("expected max_input_tokens=128000 (from catalog, not overridden), got %d", info.MaxInputTokens)
	}
	if info.MaxOutputTokens != 8192 {
		t.Errorf("expected max_output_tokens=8192 (from catalog, not overridden), got %d", info.MaxOutputTokens)
	}

	if info.SupportsVision {
		t.Error("expected supports_vision=false (override)")
	}
	if !info.SupportsFunctionCalling {
		t.Error("expected supports_function_calling=true (override)")
	}
	if !info.SupportsToolChoice {
		t.Error("expected supports_tool_choice=true (override)")
	}
	if !info.IsCapabilityOverride {
		t.Error("expected is_capability_override=true")
	}
}
