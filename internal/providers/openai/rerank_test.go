package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

func TestCompatibleProviderRerankSuccess(t *testing.T) {
	want := &core.RerankResponse{
		ID:    "rerank-001",
		Model: "test-rerank-model",
		Results: []core.RerankResult{
			{Index: 0, RelevanceScore: 0.95},
			{Index: 1, RelevanceScore: 0.72},
		},
		Usage: core.RerankUsage{
			TotalTokens: 100,
			SearchUnits: 2.0,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/rerank" {
			t.Errorf("expected /rerank, got %s", r.URL.Path)
		}

		var req core.RerankRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Query != "test query" {
			t.Errorf("expected query 'test query', got %q", req.Query)
		}
		if len(req.Documents) != 2 {
			t.Errorf("expected 2 documents, got %d", len(req.Documents))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	provider := NewCompatibleProviderWithHTTPClient("", srv.Client(), llmclient.Hooks{}, CompatibleProviderConfig{
		ProviderName: "test",
		BaseURL:      srv.URL,
	})

	resp, err := provider.Rerank(context.Background(), &core.RerankRequest{
		Model:     "test-rerank-model",
		Query:     "test query",
		Documents: []string{"doc1", "doc2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != want.ID {
		t.Errorf("expected ID %q, got %q", want.ID, resp.ID)
	}
	if resp.Model != want.Model {
		t.Errorf("expected Model %q, got %q", want.Model, resp.Model)
	}
	if len(resp.Results) != len(want.Results) {
		t.Fatalf("expected %d results, got %d", len(want.Results), len(resp.Results))
	}
	if resp.Results[0].RelevanceScore != want.Results[0].RelevanceScore {
		t.Errorf("expected relevance score %f, got %f", want.Results[0].RelevanceScore, resp.Results[0].RelevanceScore)
	}
}

func TestCompatibleProviderRerankNilRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for nil request")
	}))
	defer srv.Close()

	provider := NewCompatibleProviderWithHTTPClient("", srv.Client(), llmclient.Hooks{}, CompatibleProviderConfig{
		ProviderName: "test",
		BaseURL:      srv.URL,
	})

	resp, err := provider.Rerank(context.Background(), nil)

	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
	if resp != nil {
		t.Errorf("expected nil response for nil request, got %v", resp)
	}

	gatewayErr, ok := err.(*core.GatewayError)
	if !ok {
		t.Fatalf("expected *core.GatewayError, got %T", err)
	}
	if gatewayErr.Type != core.ErrorTypeInvalidRequest {
		t.Errorf("expected InvalidRequest error type, got %v", gatewayErr.Type)
	}
}

func TestCompatibleProviderRerankModelFallback(t *testing.T) {
	const requestModel = "my-rerank-model"

	// Server returns a response without the model field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&core.RerankResponse{
			ID: "rerank-no-model",
			Results: []core.RerankResult{
				{Index: 0, RelevanceScore: 0.88},
			},
			Usage: core.RerankUsage{
				TotalTokens: 50,
			},
		})
	}))
	defer srv.Close()

	provider := NewCompatibleProviderWithHTTPClient("", srv.Client(), llmclient.Hooks{}, CompatibleProviderConfig{
		ProviderName: "test",
		BaseURL:      srv.URL,
	})

	resp, err := provider.Rerank(context.Background(), &core.RerankRequest{
		Model:     requestModel,
		Query:     "fallback test",
		Documents: []string{"d1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Model != requestModel {
		t.Errorf("expected model to fall back to %q, got %q", requestModel, resp.Model)
	}
}