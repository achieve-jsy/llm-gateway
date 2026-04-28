package openai_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/adapters"
	openaiadapter "github.com/example/llm-gateway/internal/adapters/openai"
)

func TestChatAdapterReasoningProfileTransformsUnsupportedParams(t *testing.T) {
	adapter := openaiadapter.NewChatAdapter()
	body := map[string]any{
		"model":            "gpt-5.5",
		"messages":         []any{map[string]any{"role": "user", "content": "hi"}},
		"max_tokens":       16,
		"temperature":      0.8,
		"reasoningEffort":  "medium",
		"reasoningSummary": "auto",
	}

	got, err := adapter.TransformChatBody(adapters.StaticPlan{ParamProfile: adapters.ParamProfileOpenAIReasoningChat}, body)
	if err != nil {
		t.Fatalf("TransformChatBody returned error: %v", err)
	}

	assertNoField(t, got, "max_tokens")
	assertNoField(t, got, "temperature")
	assertNoField(t, got, "reasoningEffort")
	assertNoField(t, got, "reasoningSummary")
	assertNumber(t, got["max_completion_tokens"], 16)
	assertEqual(t, got["reasoning_effort"], "medium")
}

func TestChatAdapterReasoningProfileNormalizesNestedReasoning(t *testing.T) {
	adapter := openaiadapter.NewChatAdapter()
	body := map[string]any{
		"model":     "deployment-name-without-gpt",
		"messages":  []any{map[string]any{"role": "user", "content": "hi"}},
		"reasoning": map[string]any{"effort": "high", "summary": "detailed"},
	}

	got, err := adapter.TransformChatBody(adapters.StaticPlan{ParamProfile: adapters.ParamProfileOpenAIReasoningChat}, body)
	if err != nil {
		t.Fatalf("TransformChatBody returned error: %v", err)
	}

	assertNoField(t, got, "reasoning")
	assertEqual(t, got["reasoning_effort"], "high")
}

func TestChatAdapterStandardProfilePassesThrough(t *testing.T) {
	adapter := openaiadapter.NewChatAdapter()
	body := map[string]any{
		"model":            "gpt-5.5",
		"messages":         []any{map[string]any{"role": "user", "content": "hi"}},
		"max_tokens":       16,
		"temperature":      0.8,
		"reasoningEffort":  "medium",
		"reasoningSummary": "auto",
	}

	got, err := adapter.TransformChatBody(adapters.StaticPlan{ParamProfile: adapters.ParamProfileOpenAIStandard}, body)
	if err != nil {
		t.Fatalf("TransformChatBody returned error: %v", err)
	}

	assertNumber(t, got["max_tokens"], 16)
	assertNumber(t, got["temperature"], 0.8)
	assertEqual(t, got["reasoningEffort"], "medium")
	assertEqual(t, got["reasoningSummary"], "auto")
	assertNoField(t, got, "max_completion_tokens")
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
