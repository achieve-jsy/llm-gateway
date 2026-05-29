package orchestrator

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestStreamUsageObserver_CachedTokensFromPromptDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	obs := newStreamUsageObserver(c, time.Now())
	obs.OnJSONEvent(map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(2000),
			"completion_tokens": float64(10),
			"total_tokens":      float64(2010),
			"prompt_tokens_details": map[string]any{
				"cached_tokens": float64(1800),
			},
		},
	})
	if obs.usage == nil || obs.usage.PromptTokensDetails == nil {
		t.Fatal("expected usage with prompt_tokens_details")
	}
	if got := obs.usage.PromptTokensDetails.CachedTokens; got != 1800 {
		t.Fatalf("cached_tokens=%d want 1800", got)
	}
}

func TestStreamUsageObserver_CachedTokensFromInputTokensDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	obs := newStreamUsageObserver(c, time.Now())
	obs.OnJSONEvent(map[string]any{
		"usage": map[string]any{
			"input_tokens":  float64(2000),
			"output_tokens": float64(10),
			"total_tokens":  float64(2010),
			"input_tokens_details": map[string]any{
				"cached_tokens": float64(1500),
			},
			"output_tokens_details": map[string]any{
				"reasoning_tokens": float64(3),
			},
		},
	})
	if obs.usage == nil {
		t.Fatal("expected usage")
	}
	if obs.usage.PromptTokens != 2000 || obs.usage.CompletionTokens != 10 {
		t.Fatalf("tokens=%d/%d", obs.usage.PromptTokens, obs.usage.CompletionTokens)
	}
	if obs.usage.PromptTokensDetails == nil || obs.usage.PromptTokensDetails.CachedTokens != 1500 {
		t.Fatalf("cached_tokens missing: %#v", obs.usage.PromptTokensDetails)
	}
	if obs.usage.CompletionTokensDetails == nil || obs.usage.CompletionTokensDetails.ReasoningTokens != 3 {
		t.Fatalf("reasoning_tokens missing: %#v", obs.usage.CompletionTokensDetails)
	}
}

func TestStreamUsageObserver_ResponsesOutputTextDelta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	obs := newStreamUsageObserver(c, time.Now())
	obs.OnJSONEvent(map[string]any{"type": "response.output_text.delta", "delta": "Hello "})
	obs.OnJSONEvent(map[string]any{"type": "response.output_text.delta", "delta": "world"})
	obs.OnJSONEvent(map[string]any{
		"type":     "response.completed",
		"response": map[string]any{"id": "resp_xyz", "model": "gpt-5.5", "created_at": float64(1700000000)},
	})
	if got := obs.contentBuf.String(); got != "Hello world" {
		t.Fatalf("content=%q want %q", got, "Hello world")
	}
	if obs.lastID != "resp_xyz" || obs.lastModel != "gpt-5.5" || obs.lastCreated != 1700000000 {
		t.Fatalf("metadata=%s/%s/%d want resp_xyz/gpt-5.5/1700000000", obs.lastID, obs.lastModel, obs.lastCreated)
	}
}

func TestStreamUsageObserver_ResponsesReasoningDelta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	obs := newStreamUsageObserver(c, time.Now())
	obs.OnJSONEvent(map[string]any{"type": "response.reasoning_summary_text.delta", "delta": "think"})
	if got := obs.reasoningBuf.String(); got != "think" {
		t.Fatalf("reasoning=%q want %q", got, "think")
	}
}

func TestStreamUsageObserver_ResponsesFailedRecordsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	obs := newStreamUsageObserver(c, time.Now())
	obs.OnJSONEvent(map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"id":     "resp_fail",
			"status": "failed",
			"error":  map[string]any{"message": "upstream exploded", "code": "server_error"},
		},
	})
	if got, _ := c.Get("error_message"); got != "upstream exploded" {
		t.Fatalf("error_message=%v want %q", got, "upstream exploded")
	}
}

func TestStreamUsageObserver_ResponsesFailedWithoutErrorObject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	obs := newStreamUsageObserver(c, time.Now())
	obs.OnJSONEvent(map[string]any{"type": "response.failed", "response": map[string]any{"id": "resp_f", "status": "failed"}})
	if got, _ := c.Get("error_message"); got != "upstream responses stream failed" {
		t.Fatalf("error_message=%v want %q", got, "upstream responses stream failed")
	}
}

func TestStreamUsageObserver_ConverterErrorEventRecordsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	obs := newStreamUsageObserver(c, time.Now())
	obs.OnJSONEvent(map[string]any{
		"error": map[string]any{"message": "upstream responses stream ended with status response.failed", "type": "upstream_error"},
	})
	if got, _ := c.Get("error_message"); got != "upstream responses stream ended with status response.failed" {
		t.Fatalf("error_message=%v", got)
	}
}

func TestStreamUsageObserver_ResponsesAPIUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	obs := newStreamUsageObserver(c, time.Now())
	// Responses API format: usage nested under response.usage
	obs.OnJSONEvent(map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     "resp_abc123",
			"status": "completed",
			"usage": map[string]any{
				"input_tokens":  float64(500),
				"output_tokens": float64(200),
				"total_tokens":  float64(700),
				"input_tokens_details": map[string]any{
					"cached_tokens": float64(100),
				},
				"output_tokens_details": map[string]any{
					"reasoning_tokens": float64(50),
				},
			},
		},
	})
	if obs.usage == nil {
		t.Fatal("expected usage from Responses API format")
	}
	if obs.usage.PromptTokens != 500 {
		t.Fatalf("PromptTokens=%d want 500", obs.usage.PromptTokens)
	}
	if obs.usage.CompletionTokens != 200 {
		t.Fatalf("CompletionTokens=%d want 200", obs.usage.CompletionTokens)
	}
	if obs.usage.TotalTokens != 700 {
		t.Fatalf("TotalTokens=%d want 700", obs.usage.TotalTokens)
	}
	if obs.usage.PromptTokensDetails == nil || obs.usage.PromptTokensDetails.CachedTokens != 100 {
		t.Fatalf("cached_tokens missing from Responses API format: %#v", obs.usage.PromptTokensDetails)
	}
	if obs.usage.CompletionTokensDetails == nil || obs.usage.CompletionTokensDetails.ReasoningTokens != 50 {
		t.Fatalf("reasoning_tokens missing from Responses API format: %#v", obs.usage.CompletionTokensDetails)
	}
}
