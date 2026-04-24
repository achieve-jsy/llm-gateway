package service

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/google/uuid"
)

func TestBuildProviderEntriesIncludesPlannerFieldsInModelsV2(t *testing.T) {
	crypto.InitForTesting()
	providerID := uuid.New()
	secret, err := crypto.Encrypt(`{"api_key":"sk-test"}`)
	if err != nil {
		t.Fatalf("encrypt credentials: %v", err)
	}

	entries, err := buildProviderEntries([]model.Model{{
		Name:                   "azure-gpt5-deployment",
		ProviderID:             providerID,
		UpstreamModelID:        "deployment-001",
		BaseModel:              "gpt-5",
		ProtocolMode:           model.ProtocolModeOpenAIResponses,
		ParamProfile:           model.ParamProfileOpenAIResponses,
		UnsupportedParamPolicy: model.UnsupportedParamPolicyDrop,
		SupportedParamsOverride: model.PlannerJSONMap{
			"reasoning": true,
		},
		TransformOverrides: model.PlannerJSONMap{
			"drop": []any{"temperature"},
		},
		Provider: model.Provider{
			ID:                   providerID,
			Name:                 "azure-main",
			Type:                 "azure",
			Endpoint:             "https://example.openai.azure.com",
			EncryptedCredentials: secret,
			Enabled:              true,
		},
	}})
	if err != nil {
		t.Fatalf("buildProviderEntries returned error: %v", err)
	}
	if len(entries) != 1 || len(entries[0].ModelsV2) != 1 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	m := entries[0].ModelsV2[0]
	if m.Name != "azure-gpt5-deployment" || m.UpstreamModelID != "deployment-001" || m.BaseModel != "gpt-5" {
		t.Fatalf("model identity fields missing: %+v", m)
	}
	if m.ProtocolMode != model.ProtocolModeOpenAIResponses || m.ParamProfile != model.ParamProfileOpenAIResponses || m.UnsupportedParamPolicy != model.UnsupportedParamPolicyDrop {
		t.Fatalf("planner enum fields missing: %+v", m)
	}
	if m.SupportedParamsOverride["reasoning"] != true || len(m.TransformOverrides) == 0 {
		t.Fatalf("planner override maps missing: %+v", m)
	}
}
