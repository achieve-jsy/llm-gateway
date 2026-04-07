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
