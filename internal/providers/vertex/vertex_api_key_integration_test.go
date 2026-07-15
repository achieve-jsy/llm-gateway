//go:build integration

package vertex

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// TestVertexAPIKeyIntegration_Gemini35Flash drives the REAL gateway provider path
// (vertex.New -> newProvider -> validateConfig -> authHTTPClient -> nativeClient with
// x-goog-api-key -> generateContent) against the live Vertex AI surface. It is gated
// behind the "integration" build tag and skips unless the key/project env vars are set,
// so it never runs in the default unit-test pass and never ships a secret.
func TestVertexAPIKeyIntegration_Gemini35Flash(t *testing.T) {
	key := os.Getenv("GEMINI_VERTEX_API_KEY")
	project := os.Getenv("GEMINI_VERTEX_PROJECT")
	if key == "" || project == "" {
		t.Skip("GEMINI_VERTEX_API_KEY / GEMINI_VERTEX_PROJECT not set; skipping live Vertex test")
	}

	model := "gemini-3.5-flash"
	location := os.Getenv("GEMINI_VERTEX_LOCATION")
	if location == "" {
		location = "asia-southeast1"
	}

	cfg := providers.ProviderConfig{
		Type:           "vertex",
		APIKey:         key,
		AuthType:       "api_key",
		Backend:        "vertex",
		VertexProject:  project,
		VertexLocation: location,
		Models:         []string{model},
	}

	p := New(cfg, providers.ProviderOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := p.ChatCompletion(ctx, &core.ChatRequest{
		Model: model,
		Messages: []core.Message{
			{Role: "user", Content: "Say OK"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatalf("empty response: %+v", resp)
	}
	content, _ := resp.Choices[0].Message.Content.(string)
	fmt.Fprintf(os.Stderr, "[E2E] vertex api_key ChatCompletion OK: model=%s text=%q finish_reason=%s usage=%+v\n",
		resp.Model, content, resp.Choices[0].FinishReason, resp.Usage)
	if content == "" {
		t.Fatalf("empty content: %+v", resp.Choices[0].Message.Content)
	}
}

// TestVertexAPIKeyGatewayScenario mirrors the GATEWAY orchestration path exactly:
// the control-plane snapshot delivers only type + api_key + base_url (empty AuthType),
// so the vertex provider must auto-detect api_key and propagate it to the gemini
// sub-provider (otherwise gemini defaults to gcp_adc and sends no x-goog-api-key).
func TestVertexAPIKeyGatewayScenario(t *testing.T) {
	key := os.Getenv("GEMINI_VERTEX_API_KEY")
	project := os.Getenv("GEMINI_VERTEX_PROJECT")
	if key == "" || project == "" {
		t.Skip("GEMINI_VERTEX_API_KEY / GEMINI_VERTEX_PROJECT not set; skipping live Vertex test")
	}
	model := "gemini-3.5-flash"
	location := os.Getenv("GEMINI_VERTEX_LOCATION")
	if location == "" {
		location = "asia-southeast1"
	}
	// AuthType intentionally empty (as the snapshot delivers), no VertexProject/Location:
	// the project+location ride on the base_url exactly like a provider endpoint.
	cfg := providers.ProviderConfig{
		Type:    "vertex",
		APIKey:  key,
		BaseURL: "https://aiplatform.googleapis.com/v1/projects/" + project + "/locations/" + location + "/publishers/google",
		Models:  []string{model},
	}
	p := New(cfg, providers.ProviderOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := p.ChatCompletion(ctx, &core.ChatRequest{
		Model: model,
		Messages: []core.Message{
			{Role: "user", Content: "Say OK"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error (gateway scenario, empty auth_type): %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatalf("empty response: %+v", resp)
	}
	content, _ := resp.Choices[0].Message.Content.(string)
	fmt.Fprintf(os.Stderr, "[E2E] gateway-scenario (empty auth_type) ChatCompletion OK: model=%s text=%q\n", resp.Model, content)
	if content == "" {
		t.Fatalf("empty content: %+v", resp.Choices[0].Message.Content)
	}
}
