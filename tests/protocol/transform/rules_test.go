package transform_test

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/protocol/transform"
)

func TestApplyRulesRenameDropAndForceLiteral(t *testing.T) {
	body := map[string]any{
		"model":       "gpt-5.5",
		"max_tokens":  16,
		"temperature": 0.7,
	}

	got, err := transform.ApplyRules(body,
		transform.Rename("max_tokens", "max_completion_tokens"),
		transform.Drop("temperature"),
		transform.ForceLiteral("stream", true),
	)
	if err != nil {
		t.Fatalf("ApplyRules returned error: %v", err)
	}

	assertAbsent(t, got, "max_tokens")
	assertAbsent(t, got, "temperature")
	assertJSONNumber(t, got["max_completion_tokens"], 16)
	assertJSONBool(t, got["stream"], true)
}

func TestApplyRulesNormalizeReasoningAliases(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "camel alias",
			body: map[string]any{"reasoningEffort": "medium"},
			want: "medium",
		},
		{
			name: "nested reasoning effort",
			body: map[string]any{"reasoning": map[string]any{"effort": "high", "summary": "auto"}},
			want: "high",
		},
		{
			name: "dict reasoning_effort",
			body: map[string]any{"reasoning_effort": map[string]any{"effort": "low"}},
			want: "low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := transform.ApplyRules(tt.body, transform.NormalizeReasoningEffort())
			if err != nil {
				t.Fatalf("ApplyRules returned error: %v", err)
			}
			assertAbsent(t, got, "reasoningEffort")
			assertAbsent(t, got, "reasoning")
			assertJSONString(t, got["reasoning_effort"], tt.want)
		})
	}
}

func TestApplyRulesDropReasoningSummary(t *testing.T) {
	got, err := transform.ApplyRules(map[string]any{
		"reasoningSummary":  "auto",
		"reasoning_summary": "detailed",
		"reasoning_effort":  "medium",
	}, transform.DropReasoningSummary())
	if err != nil {
		t.Fatalf("ApplyRules returned error: %v", err)
	}

	assertAbsent(t, got, "reasoningSummary")
	assertAbsent(t, got, "reasoning_summary")
	assertJSONString(t, got["reasoning_effort"], "medium")
}

func TestApplyRulesRenamePreservesExistingTarget(t *testing.T) {
	got, err := transform.ApplyRules(map[string]any{
		"max_tokens":            16,
		"max_completion_tokens": 32,
	}, transform.Rename("max_tokens", "max_completion_tokens"))
	if err != nil {
		t.Fatalf("ApplyRules returned error: %v", err)
	}

	assertAbsent(t, got, "max_tokens")
	assertJSONNumber(t, got["max_completion_tokens"], 32)
}

func assertAbsent(t *testing.T, raw map[string]json.RawMessage, key string) {
	t.Helper()
	if _, ok := raw[key]; ok {
		t.Fatalf("field %q should be absent, got %s", key, raw[key])
	}
}

func assertJSONString(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode string %s: %v", raw, err)
	}
	if got != want {
		t.Fatalf("string = %q, want %q", got, want)
	}
}

func assertJSONBool(t *testing.T, raw json.RawMessage, want bool) {
	t.Helper()
	var got bool
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode bool %s: %v", raw, err)
	}
	if got != want {
		t.Fatalf("bool = %v, want %v", got, want)
	}
}

func assertJSONNumber(t *testing.T, raw json.RawMessage, want float64) {
	t.Helper()
	var got float64
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode number %s: %v", raw, err)
	}
	if got != want {
		t.Fatalf("number = %v, want %v", got, want)
	}
}
