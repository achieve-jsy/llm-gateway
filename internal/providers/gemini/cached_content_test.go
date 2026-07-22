package gemini

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

func cacheMarker() core.UnknownJSONFields {
	return core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
		"cache_control": json.RawMessage(`{"type":"ephemeral"}`),
	})
}

func TestParseCacheMode(t *testing.T) {
	cases := map[string]cacheMode{
		"auto":   cacheModeAuto,
		"AUTO":   cacheModeAuto,
		"marker": cacheModeMarker,
		"MARKER": cacheModeMarker,
		"":       cacheModeOff,
		"off":    cacheModeOff,
		"bogus":  cacheModeOff,
	}
	for in, want := range cases {
		if got := parseCacheMode(in); got != want {
			t.Errorf("parseCacheMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVertexLocationRoot(t *testing.T) {
	input := "https://aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google"
	root, ok := vertexLocationRoot(input)
	if !ok {
		t.Fatalf("expected ok for vertex URL")
	}
	if root != "https://aiplatform.googleapis.com/v1/projects/p/locations/l" {
		t.Errorf("root = %q", root)
	}

	if _, ok := vertexLocationRoot("https://generativelanguage.googleapis.com/v1beta/models"); ok {
		t.Errorf("expected not-ok for non-vertex URL")
	}
}

func TestMessageCarriesCacheMarker(t *testing.T) {
	if !messageCarriesCacheMarker(core.Message{Role: "user", ExtraFields: cacheMarker()}) {
		t.Errorf("expected marker on message extra fields")
	}
	if !messageCarriesCacheMarker(core.Message{
		Role:          "user",
		ContentSchema: []core.ContentPart{{Type: "text", Text: "x", ExtraFields: cacheMarker()}},
	}) {
		t.Errorf("expected marker on content part")
	}
	if messageCarriesCacheMarker(core.Message{Role: "user", Content: "plain"}) {
		t.Errorf("expected no marker")
	}
}

func TestMarkerCachedMessages_Contiguous(t *testing.T) {
	msgs := []core.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c", ExtraFields: cacheMarker()},
		{Role: "user", Content: "d", ExtraFields: cacheMarker()},
		{Role: "assistant", Content: "e"},
	}
	cached, tail, ok := markerCachedMessages(msgs)
	if !ok {
		t.Fatalf("expected ok")
	}
	if len(cached) != 2 || cached[0].Content != "c" || cached[1].Content != "d" {
		t.Errorf("cached = %+v", cached)
	}
	if len(tail) != 3 || tail[0].Content != "a" || tail[1].Content != "b" || tail[2].Content != "e" {
		t.Errorf("tail = %+v", tail)
	}
}

func TestMarkerCachedMessages_DisjointBlockSkipped(t *testing.T) {
	msgs := []core.Message{
		{Role: "user", Content: "a", ExtraFields: cacheMarker()},
		{Role: "user", Content: "b"},
		{Role: "user", Content: "c", ExtraFields: cacheMarker()},
	}
	if _, _, ok := markerCachedMessages(msgs); ok {
		t.Errorf("expected skip for disjoint cached blocks")
	}
}

func TestAutoCachedMessages(t *testing.T) {
	msgs := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"},
	}
	cached, tail, ok := autoCachedMessages(msgs)
	if !ok {
		t.Fatalf("expected ok")
	}
	if len(cached) != 2 || cached[0].Role != "system" || cached[1].Role != "user" {
		t.Errorf("cached = %+v", cached)
	}
	if len(tail) != 2 || tail[0].Role != "assistant" || tail[1].Role != "user" {
		t.Errorf("tail = %+v", tail)
	}
	if !cachedBlockEndsOnSupportedTurn(cached) {
		t.Errorf("cached block should end on a user turn")
	}
}

func TestAutoCachedMessages_SingleTurnSkipped(t *testing.T) {
	msgs := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q1"},
	}
	if _, _, ok := autoCachedMessages(msgs); ok {
		t.Errorf("expected skip when there is no prefix before the single user turn")
	}
}

func TestCachedBlockEndsOnSupportedTurn(t *testing.T) {
	if !cachedBlockEndsOnSupportedTurn([]core.Message{{Role: "user", Content: "x"}}) {
		t.Errorf("user turn should be supported")
	}
	if cachedBlockEndsOnSupportedTurn([]core.Message{{Role: "assistant", Content: "x"}}) {
		t.Errorf("assistant turn should be rejected")
	}
}

func TestCacheWriteTokensFromGemini(t *testing.T) {
	if got := cacheWriteTokensFromGemini(json.RawMessage(`[{"model":"gemini-3","cacheTokensCount":123},{"model":"gemini-2","cacheTokensCount":7}]`)); got != 130 {
		t.Errorf("got %d, want 130", got)
	}
	if got := cacheWriteTokensFromGemini(nil); got != 0 {
		t.Errorf("nil should be 0, got %d", got)
	}
	if got := cacheWriteTokensFromGemini(json.RawMessage(`not-json`)); got != 0 {
		t.Errorf("bad json should be 0, got %d", got)
	}
}

func TestCachedContentCacheKey_ChangesWithInput(t *testing.T) {
	model := "gemini-3.8-flash"
	base := []core.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "q1"}}
	k1 := cachedContentCacheKey(base, model, nil)
	k2 := cachedContentCacheKey(base, model, nil)
	if k1 != k2 {
		t.Errorf("same input should yield the same key")
	}
	other := []core.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "q2"}}
	if k1 == cachedContentCacheKey(other, model, nil) {
		t.Errorf("different user content should yield a different key")
	}
}
