package scheduler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConfigSyncerFetchConfigPlannerFieldsFromControlPlaneData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != configSyncEndpoint {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [{
				"id": 1,
				"name": "planner-group",
				"strategy": "priority",
				"enabled": true,
				"models": [{
					"id": "model-uuid",
					"name": "deployment-001",
					"model_type": "chat",
					"provider_type": "azure",
					"provider_name": "azure-main",
					"endpoint": "https://example.openai.azure.com",
					"base_model": "gpt-5",
					"upstream_model_id": "deployment-001",
					"protocol_mode": "openai_responses",
					"param_profile": "openai_responses",
					"unsupported_param_policy": "drop",
					"supported_params_override": {"reasoning": true},
					"transform_overrides": {"drop": ["temperature"]}
				}]
			}]
		}`))
	}))
	defer srv.Close()

	cs := NewConfigSyncer(srv.URL, "", NewRouteTable(), NewHealthStore(1, 1, 1, time.Second), time.Second)
	cfg, err := cs.fetchConfig()
	if err != nil {
		t.Fatalf("fetchConfig returned error: %v", err)
	}
	if len(cfg.Models) != 1 {
		t.Fatalf("expected one model, got %+v", cfg.Models)
	}
	m := cfg.Models[0]
	if m.BaseModel != "gpt-5" || m.ProtocolMode != "openai_responses" || m.ParamProfile != "openai_responses" || m.UnsupportedParamPolicy != "drop" {
		t.Fatalf("planner fields not propagated from CP data: %+v", m)
	}
	if m.SupportedParamsOverride["reasoning"] != true || len(m.TransformOverrides) == 0 {
		t.Fatalf("planner override maps not propagated: %+v", m)
	}
}
