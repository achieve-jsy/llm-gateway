package core

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestContentPartErrorCarriesIndex 验证片段错误在穿越 encoding/json 与 NormalizeMessageContent
// 之后仍能拿到片段下标与原始 type（HTTP 层据此产出 param）。
func TestContentPartErrorCarriesIndex(t *testing.T) {
	var msg Message
	err := json.Unmarshal([]byte(`{"role":"user","content":[{"type":"text","text":"a"},{"type":"pdf","pdf":{}}]}`), &msg)
	if err == nil {
		t.Fatal("expected error for unsupported part")
	}
	var cpe *ContentPartError
	if !errors.As(err, &cpe) {
		t.Fatalf("expected ContentPartError, got %T: %v", err, err)
	}
	if cpe.Index != 1 {
		t.Fatalf("index = %d, want 1", cpe.Index)
	}
	if cpe.PartType != "pdf" {
		t.Fatalf("part type = %q, want pdf", cpe.PartType)
	}

	_, err = NormalizeMessageContent([]any{
		map[string]any{"type": "video_url", "video_url": map[string]any{}},
	})
	if !errors.As(err, &cpe) {
		t.Fatalf("expected ContentPartError for empty video_url, got %T: %v", err, err)
	}
	if cpe.Index != 0 || cpe.Field != "video_url.url" {
		t.Fatalf("index/field = %d/%q, want 0 and video_url.url", cpe.Index, cpe.Field)
	}
}
