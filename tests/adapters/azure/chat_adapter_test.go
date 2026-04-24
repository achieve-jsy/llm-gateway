package azure_test

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/adapters"
	azureadapter "github.com/example/llm-gateway/internal/adapters/azure"
)

func TestChatAdapterReusesOpenAIReasoningTransform(t *testing.T) {
	adapter := azureadapter.NewChatAdapter()
	body := map[string]any{
		"model":           "deployment-without-model-family-name",
		"messages":        []any{map[string]any{"role": "user", "content": "hi"}},
		"max_tokens":      24,
		"temperature":     0.3,
		"reasoningEffort": "low",
	}

	got, err := adapter.TransformChatBody(adapters.StaticPlan{ParamProfile: adapters.ParamProfileOpenAIReasoningChat}, body)
	if err != nil {
		t.Fatalf("TransformChatBody returned error: %v", err)
	}

	assertNoField(t, got, "max_tokens")
	assertNoField(t, got, "temperature")
	assertNoField(t, got, "reasoningEffort")
	assertNumber(t, got["max_completion_tokens"], 24)
	assertEqual(t, got["reasoning_effort"], "low")
}

func TestChatAdapterSanitizesAzureIllegalFields(t *testing.T) {
	adapter := azureadapter.NewChatAdapter()
	body := map[string]any{
		"model":             "gpt-4.1",
		"messages":          []any{map[string]any{"role": "user", "content": "hi"}},
		"reasoningSummary":  "auto",
		"reasoning_summary": "detailed",
		"reasoningEffort":   "medium",
		"reasoning":         map[string]any{"effort": "high"},
		"temperature":       0.7,
	}

	got, err := adapter.TransformChatBody(adapters.StaticPlan{ParamProfile: adapters.ParamProfileOpenAIStandard}, body)
	if err != nil {
		t.Fatalf("TransformChatBody returned error: %v", err)
	}

	assertNoField(t, got, "reasoningSummary")
	assertNoField(t, got, "reasoning_summary")
	assertNoField(t, got, "reasoningEffort")
	assertNoField(t, got, "reasoning")
	assertNumber(t, got["temperature"], 0.7)
}

func TestChatEndpointAppendsAPIVersion(t *testing.T) {
	got := azureadapter.ChatEndpoint("2026-01-01-preview")
	if !strings.HasPrefix(got, "/chat/completions?") || !strings.Contains(got, "api-version=2026-01-01-preview") {
		t.Fatalf("ChatEndpoint = %q", got)
	}
	if got := azureadapter.ChatEndpoint(" "); got != "/chat/completions" {
		t.Fatalf("ChatEndpoint empty = %q, want /chat/completions", got)
	}
}

func assertNoField(t *testing.T, got map[string]any, key string) {
	t.Helper()
	if _, ok := got[key]; ok {
		t.Fatalf("field %q should be absent, got %#v", key, got[key])
	}
}

func assertEqual(t *testing.T, got any, want any) {
	t.Helper()
	if got != want {
		t.Fatalf("value = %#v, want %#v", got, want)
	}
}

func assertNumber(t *testing.T, got any, want float64) {
	t.Helper()
	n, ok := got.(float64)
	if !ok {
		t.Fatalf("value = %#v (%T), want number %v", got, got, want)
	}
	if n != want {
		t.Fatalf("number = %v, want %v", n, want)
	}
}
