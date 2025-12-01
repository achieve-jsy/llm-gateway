package orchestrator_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/gateway/orchestrator"
)

// ---------------------------------------------------------------------------
// ParseOpenAIChunk
// ---------------------------------------------------------------------------

func TestParseOpenAIChunk_FullUsage(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":20},"completion_tokens_details":{"reasoning_tokens":30}}}`)

	u := p.ParseOpenAIChunk(data)
	if u == nil {
		t.Fatal("expected non-nil StreamUsage")
	}
	if u.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", u.PromptTokens)
	}
	if u.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d, want 50", u.CompletionTokens)
	}
	if u.ReasoningTokens != 30 {
		t.Errorf("ReasoningTokens = %d, want 30", u.ReasoningTokens)
	}
	if u.CachedTokens != 20 {
		t.Errorf("CachedTokens = %d, want 20", u.CachedTokens)
	}
}

func TestParseOpenAIChunk_PartialUsage(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":20},"completion_tokens_details":{"reasoning_tokens":30}}}`)

	u := p.ParseOpenAIChunk(data)
	if u == nil {
		t.Fatal("expected non-nil StreamUsage")
	}
	if u.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", u.PromptTokens)
	}
	if u.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d, want 50", u.CompletionTokens)
	}
	if u.ReasoningTokens != 30 {
		t.Errorf("ReasoningTokens = %d, want 30", u.ReasoningTokens)
	}
	if u.CachedTokens != 20 {
		t.Errorf("CachedTokens = %d, want 20", u.CachedTokens)
	}
}

func TestParseOpenAIChunk_NoUsage(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"choices":[{"delta":{"content":"hello"}}]}`)

	u := p.ParseOpenAIChunk(data)
	if u != nil {
		t.Fatal("expected nil when no usage field")
	}
}

func TestParseOpenAIChunk_EmptyObject(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{}`)

	u := p.ParseOpenAIChunk(data)
	if u != nil {
		t.Fatal("expected nil for empty JSON")
	}
}

func TestParseOpenAIChunk_NonJSON(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`[DONE]`)

	u := p.ParseOpenAIChunk(data)
	if u != nil {
		t.Fatal("expected nil for non-JSON input")
	}
}

func TestParseOpenAIChunk_NilBytes(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()

	u := p.ParseOpenAIChunk(nil)
	if u != nil {
		t.Fatal("expected nil for nil bytes")
	}
}

// ---------------------------------------------------------------------------
// ParseAnthropicEvent — message_start
// ---------------------------------------------------------------------------

func TestParseAnthropicEvent_MessageStartFull(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"type":"message_start","message":{"usage":{"input_tokens":200,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}}`)

	u := p.ParseAnthropicEvent("message_start", data)
	if u == nil {
		t.Fatal("expected non-nil StreamUsage")
	}
	if u.PromptTokens != 200 {
		t.Errorf("PromptTokens = %d, want 200", u.PromptTokens)
	}
	// CachedTokens = cache_read + cache_creation
	if u.CachedTokens != 15 {
		t.Errorf("CachedTokens = %d, want 15 (10+5)", u.CachedTokens)
	}
	// message_start does NOT provide completion/reasoning
	if u.CompletionTokens != 0 {
		t.Errorf("CompletionTokens = %d, want 0", u.CompletionTokens)
	}
	if u.ReasoningTokens != 0 {
		t.Errorf("ReasoningTokens = %d, want 0", u.ReasoningTokens)
	}
}

func TestParseAnthropicEvent_MessageStartNoCache(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"type":"message_start","message":{"usage":{"input_tokens":50,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`)

	u := p.ParseAnthropicEvent("message_start", data)
	if u == nil {
		t.Fatal("expected non-nil StreamUsage")
	}
	if u.PromptTokens != 50 {
		t.Errorf("PromptTokens = %d, want 50", u.PromptTokens)
	}
	if u.CachedTokens != 0 {
		t.Errorf("CachedTokens = %d, want 0", u.CachedTokens)
	}
}

func TestParseAnthropicEvent_MessageStartNilMessage(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"type":"message_start"}`)

	u := p.ParseAnthropicEvent("message_start", data)
	if u != nil {
		t.Fatal("expected nil when message field is missing")
	}
}

// ---------------------------------------------------------------------------
// ParseAnthropicEvent — message_delta
// ---------------------------------------------------------------------------

func TestParseAnthropicEvent_MessageDelta(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"type":"message_delta","usage":{"output_tokens":80}}`)

	u := p.ParseAnthropicEvent("message_delta", data)
	if u == nil {
		t.Fatal("expected non-nil StreamUsage")
	}
	if u.CompletionTokens != 80 {
		t.Errorf("CompletionTokens = %d, want 80", u.CompletionTokens)
	}
	// message_delta does NOT provide prompt/cached
	if u.PromptTokens != 0 {
		t.Errorf("PromptTokens = %d, want 0", u.PromptTokens)
	}
	if u.CachedTokens != 0 {
		t.Errorf("CachedTokens = %d, want 0", u.CachedTokens)
	}
	if u.ReasoningTokens != 0 {
		t.Errorf("ReasoningTokens = %d, want 0", u.ReasoningTokens)
	}
}

func TestParseAnthropicEvent_MessageDeltaWithThinking(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"type":"message_delta","usage":{"output_tokens":80,"thinking_tokens":25}}`)

	u := p.ParseAnthropicEvent("message_delta", data)
	if u == nil {
		t.Fatal("expected non-nil StreamUsage")
	}
	if u.CompletionTokens != 80 {
		t.Errorf("CompletionTokens = %d, want 80", u.CompletionTokens)
	}
	if u.ReasoningTokens != 25 {
		t.Errorf("ReasoningTokens = %d, want 25", u.ReasoningTokens)
	}
}

func TestParseAnthropicEvent_MessageDeltaNilUsage(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"type":"message_delta"}`)

	u := p.ParseAnthropicEvent("message_delta", data)
	if u != nil {
		t.Fatal("expected nil when usage field is missing")
	}
}

// ---------------------------------------------------------------------------
// ParseAnthropicEvent — unknown event type
// ---------------------------------------------------------------------------

func TestParseAnthropicEvent_UnknownEvent(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"type":"ping"}`)

	u := p.ParseAnthropicEvent("ping", data)
	if u != nil {
		t.Fatal("expected nil for unknown event type")
	}
}

func TestParseAnthropicEvent_ContentBlockDelta(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"type":"content_block_delta","delta":{"text":"hello"}}`)

	u := p.ParseAnthropicEvent("content_block_delta", data)
	if u != nil {
		t.Fatal("expected nil for content_block_delta event")
	}
}

func TestParseAnthropicEvent_MessageStop(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"type":"message_stop"}`)

	u := p.ParseAnthropicEvent("message_stop", data)
	if u != nil {
		t.Fatal("expected nil for message_stop event")
	}
}

func TestParseOpenAIChunk_OnlyUsageField(t *testing.T) {
	p := orchestrator.NewStreamUsageParser()
	data := []byte(`{"usage":{"prompt_tokens":5,"completion_tokens":10,"total_tokens":15}}`)

	u := p.ParseOpenAIChunk(data)
	if u == nil {
		t.Fatal("expected non-nil StreamUsage")
	}
	if u.PromptTokens != 5 {
		t.Errorf("PromptTokens = %d, want 5", u.PromptTokens)
	}
	if u.CompletionTokens != 10 {
		t.Errorf("CompletionTokens = %d, want 10", u.CompletionTokens)
	}
	// No details sub-fields in this payload
	if u.CachedTokens != 0 {
		t.Errorf("CachedTokens = %d, want 0", u.CachedTokens)
	}
	if u.ReasoningTokens != 0 {
		t.Errorf("ReasoningTokens = %d, want 0", u.ReasoningTokens)
	}
}
