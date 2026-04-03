package azure_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/azure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAzureV1_ChatUsesOpenAIV1PathWithoutAPIVersion(t *testing.T) {
	var gotPath, gotQuery, gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAPIKey = r.Header.Get("api-key")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-1",
			"object":  "chat.completion",
			"model":   "gpt-5.5",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "pong"}, "finish_reason": "stop"}},
		})
	}))
	defer srv.Close()

	p := azure.New(providers.ProviderConfig{
		APIKey:  "test-azure-key",
		BaseURL: srv.URL, // 资源根（此处用 httptest 根）
	}, providers.ProviderOptions{})

	resp, err := p.ChatCompletion(context.Background(), &core.ChatRequest{
		Model:    "gpt-5.5",
		Messages: []core.Message{{Role: "user", Content: "ping"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "/openai/v1/chat/completions", gotPath)
	assert.Empty(t, gotQuery, "v1 GA 默认不应附带 dated api-version")
	assert.Equal(t, "test-azure-key", gotAPIKey)
}

func TestAzureV1_ResponsesUsesOpenAIV1Path(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		assert.Equal(t, "gpt-5.5", payload["model"])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "resp_1",
			"object": "response",
			"model":  "gpt-5.5",
			"status": "completed",
			"output": []any{},
		})
	}))
	defer srv.Close()

	p := azure.New(providers.ProviderConfig{
		APIKey:  "k",
		BaseURL: srv.URL + "/openai/deployments/gpt-5.5", // 旧配法应被归一化到资源根
	}, providers.ProviderOptions{})

	_, err := p.Responses(context.Background(), &core.ResponsesRequest{
		Model: "gpt-5.5",
		Input: "ping",
	})
	require.NoError(t, err)
	assert.Equal(t, "/openai/v1/responses", gotPath)
	assert.Empty(t, gotQuery)
}

func TestAzureV1_ListModelsUsesOpenAIV1Path(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "gpt-5.5", "object": "model"}},
		})
	}))
	defer srv.Close()

	p := azure.New(providers.ProviderConfig{
		APIKey:  "k",
		BaseURL: srv.URL + "/openai/responses?api-version=2025-04-01-preview",
	}, providers.ProviderOptions{})

	resp, err := p.ListModels(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "/openai/v1/models", gotPath)
}

func TestAzureV1_OptionalPreviewAPIVersionAppended(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion", "model": "gpt-5.5",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
		})
	}))
	defer srv.Close()

	p := azure.New(providers.ProviderConfig{
		APIKey:     "k",
		BaseURL:    srv.URL,
		APIVersion: "preview",
	}, providers.ProviderOptions{})

	_, err := p.ChatCompletion(context.Background(), &core.ChatRequest{
		Model:    "gpt-5.5",
		Messages: []core.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "api-version=preview", gotQuery)
}

func TestNormalizeAzureResourceRoot(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://examplewestus.openai.azure.com", "https://examplewestus.openai.azure.com"},
		{"https://examplewestus.openai.azure.com/", "https://examplewestus.openai.azure.com"},
		{"https://examplewestus.openai.azure.com/openai/v1", "https://examplewestus.openai.azure.com"},
		{"https://examplewestus.openai.azure.com/openai/v1/", "https://examplewestus.openai.azure.com"},
		{"https://examplewestus.openai.azure.com/openai/deployments/gpt-5.5", "https://examplewestus.openai.azure.com"},
		{"https://examplewestus.openai.azure.com/openai/responses?api-version=2025-04-01-preview", "https://examplewestus.openai.azure.com"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, azure.NormalizeAzureResourceRoot(tc.in))
		})
	}
}
