package service

import (
	"encoding/json"
	"testing"
)

// TestParseCatalogVideoInput 验证 catalog 采集器不再丢弃 supports_video_input，
// 未声明的条目保持 false（与既有能力位一致）。
func TestParseCatalogVideoInput(t *testing.T) {
	raw := map[string]json.RawMessage{
		"gemini/gemini-3-pro": json.RawMessage(`{"mode":"chat","supports_video_input":true,"supports_vision":true}`),
		"openai/gpt-5":        json.RawMessage(`{"mode":"chat","supports_vision":true}`),
	}

	entries, err := ParseLiteLLMJSON(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	byID := make(map[string]*ModelCatalogParsed, len(entries))
	for _, e := range entries {
		byID[e.SourceModelID] = e
	}

	if e := byID["gemini/gemini-3-pro"]; e == nil || !e.SupportsVideoInput {
		t.Fatalf("gemini entry SupportsVideoInput = %v, want true", e)
	}
	if e := byID["openai/gpt-5"]; e == nil || e.SupportsVideoInput {
		t.Fatalf("openai entry SupportsVideoInput = %v, want false", e)
	}
}
