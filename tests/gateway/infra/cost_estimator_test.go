package infra_test

import (
	"math"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/infra"
)

const tolerance = 0.0001

func floatEqual(a, b float64) bool {
	return math.Abs(a-b) < tolerance
}

func TestEstimateCost(t *testing.T) {
	p := func(v float64) *float64 { return &v }

	t.Run("all four buckets with independent prices", func(t *testing.T) {
		tokens := infra.TokenBuckets{
			PromptTokens:     1000,
			CompletionTokens: 500,
			ReasoningTokens:  200,
			CachedTokens:     300,
		}
		pricing := infra.ModelPricing{
			PromptPrice:     2.0,
			CompletionPrice: 8.0,
			ReasoningPrice:  p(12.0),
			CachedPrice:     p(1.0),
		}

		result := infra.EstimateCost(tokens, pricing)

		wantPrompt := 1000.0 * 2.0 / 1_000_000.0
		wantCompletion := 500.0 * 8.0 / 1_000_000.0
		wantReasoning := 200.0 * 12.0 / 1_000_000.0
		wantCached := 300.0 * 1.0 / 1_000_000.0
		wantTotal := wantPrompt + wantCompletion + wantReasoning + wantCached

		if !floatEqual(result.PromptCost, wantPrompt) {
			t.Errorf("PromptCost = %v, want %v", result.PromptCost, wantPrompt)
		}
		if !floatEqual(result.CompletionCost, wantCompletion) {
			t.Errorf("CompletionCost = %v, want %v", result.CompletionCost, wantCompletion)
		}
		if !floatEqual(result.ReasoningCost, wantReasoning) {
			t.Errorf("ReasoningCost = %v, want %v", result.ReasoningCost, wantReasoning)
		}
		if !floatEqual(result.CachedCost, wantCached) {
			t.Errorf("CachedCost = %v, want %v", result.CachedCost, wantCached)
		}
		if !floatEqual(result.TotalCost, wantTotal) {
			t.Errorf("TotalCost = %v, want %v", result.TotalCost, wantTotal)
		}
		if result.FallbackReasoning {
			t.Errorf("FallbackReasoning = true, want false")
		}
	})

	t.Run("ReasoningPrice nil falls back to CompletionPrice with FallbackReasoning=true", func(t *testing.T) {
		tokens := infra.TokenBuckets{
			PromptTokens:     500,
			CompletionTokens: 300,
			ReasoningTokens:  200,
			CachedTokens:     100,
		}
		pricing := infra.ModelPricing{
			PromptPrice:     3.0,
			CompletionPrice: 6.0,
			ReasoningPrice:  nil,
			CachedPrice:     p(2.0),
		}

		result := infra.EstimateCost(tokens, pricing)

		wantReasoning := 200.0 * 6.0 / 1_000_000.0
		if !floatEqual(result.ReasoningCost, wantReasoning) {
			t.Errorf("ReasoningCost = %v, want %v", result.ReasoningCost, wantReasoning)
		}
		if !result.FallbackReasoning {
			t.Errorf("FallbackReasoning = false, want true")
		}

		// Other costs should still be correct
		wantPrompt := 500.0 * 3.0 / 1_000_000.0
		wantCompletion := 300.0 * 6.0 / 1_000_000.0
		wantCached := 100.0 * 2.0 / 1_000_000.0
		if !floatEqual(result.PromptCost, wantPrompt) {
			t.Errorf("PromptCost = %v, want %v", result.PromptCost, wantPrompt)
		}
		if !floatEqual(result.CompletionCost, wantCompletion) {
			t.Errorf("CompletionCost = %v, want %v", result.CompletionCost, wantCompletion)
		}
		if !floatEqual(result.CachedCost, wantCached) {
			t.Errorf("CachedCost = %v, want %v", result.CachedCost, wantCached)
		}
	})

	t.Run("CachedPrice nil results in CachedCost=0", func(t *testing.T) {
		tokens := infra.TokenBuckets{
			PromptTokens:     100,
			CompletionTokens: 100,
			ReasoningTokens:  50,
			CachedTokens:     9999,
		}
		pricing := infra.ModelPricing{
			PromptPrice:     5.0,
			CompletionPrice: 10.0,
			ReasoningPrice:  p(15.0),
			CachedPrice:     nil,
		}

		result := infra.EstimateCost(tokens, pricing)

		if result.CachedCost != 0 {
			t.Errorf("CachedCost = %v, want 0", result.CachedCost)
		}
		if result.TotalCost == 0 {
			t.Errorf("TotalCost = 0, expected non-zero")
		}
	})

	t.Run("PromptPrice=0 triggers PricingMissing (all costs zero)", func(t *testing.T) {
		tokens := infra.TokenBuckets{
			PromptTokens:     1000,
			CompletionTokens: 500,
			ReasoningTokens:  200,
			CachedTokens:     300,
		}
		pricing := infra.ModelPricing{
			PromptPrice:     0,
			CompletionPrice: 8.0,
			ReasoningPrice:  p(12.0),
			CachedPrice:     p(1.0),
		}

		result := infra.EstimateCost(tokens, pricing)

		if result.PromptCost != 0 {
			t.Errorf("PromptCost = %v, want 0", result.PromptCost)
		}
		if result.CompletionCost != 0 {
			t.Errorf("CompletionCost = %v, want 0", result.CompletionCost)
		}
		if result.ReasoningCost != 0 {
			t.Errorf("ReasoningCost = %v, want 0", result.ReasoningCost)
		}
		if result.CachedCost != 0 {
			t.Errorf("CachedCost = %v, want 0", result.CachedCost)
		}
		if result.TotalCost != 0 {
			t.Errorf("TotalCost = %v, want 0", result.TotalCost)
		}
		if result.FallbackReasoning {
			t.Errorf("FallbackReasoning = true, want false")
		}
	})

	t.Run("CompletionPrice=0 triggers PricingMissing (all costs zero)", func(t *testing.T) {
		tokens := infra.TokenBuckets{
			PromptTokens:     100,
			CompletionTokens: 200,
			ReasoningTokens:  300,
			CachedTokens:     400,
		}
		pricing := infra.ModelPricing{
			PromptPrice:     5.0,
			CompletionPrice: 0,
			ReasoningPrice:  p(10.0),
			CachedPrice:     p(2.0),
		}

		result := infra.EstimateCost(tokens, pricing)

		if result.PromptCost != 0 {
			t.Errorf("PromptCost = %v, want 0", result.PromptCost)
		}
		if result.CompletionCost != 0 {
			t.Errorf("CompletionCost = %v, want 0", result.CompletionCost)
		}
		if result.ReasoningCost != 0 {
			t.Errorf("ReasoningCost = %v, want 0", result.ReasoningCost)
		}
		if result.CachedCost != 0 {
			t.Errorf("CachedCost = %v, want 0", result.CachedCost)
		}
		if result.TotalCost != 0 {
			t.Errorf("TotalCost = %v, want 0", result.TotalCost)
		}
		if result.FallbackReasoning {
			t.Errorf("FallbackReasoning = true, want false")
		}
	})

	t.Run("all tokens zero results in all costs zero", func(t *testing.T) {
		tokens := infra.TokenBuckets{
			PromptTokens:     0,
			CompletionTokens: 0,
			ReasoningTokens:  0,
			CachedTokens:     0,
		}
		pricing := infra.ModelPricing{
			PromptPrice:     2.0,
			CompletionPrice: 8.0,
			ReasoningPrice:  p(12.0),
			CachedPrice:     p(1.0),
		}

		result := infra.EstimateCost(tokens, pricing)

		if result.PromptCost != 0 {
			t.Errorf("PromptCost = %v, want 0", result.PromptCost)
		}
		if result.CompletionCost != 0 {
			t.Errorf("CompletionCost = %v, want 0", result.CompletionCost)
		}
		if result.ReasoningCost != 0 {
			t.Errorf("ReasoningCost = %v, want 0", result.ReasoningCost)
		}
		if result.CachedCost != 0 {
			t.Errorf("CachedCost = %v, want 0", result.CachedCost)
		}
		if result.TotalCost != 0 {
			t.Errorf("TotalCost = %v, want 0", result.TotalCost)
		}
		if result.FallbackReasoning {
			t.Errorf("FallbackReasoning = true, want false")
		}
	})

	t.Run("mixed: some tokens zero, some non-zero", func(t *testing.T) {
		tokens := infra.TokenBuckets{
			PromptTokens:     1000,
			CompletionTokens: 500,
			ReasoningTokens:  0,
			CachedTokens:     0,
		}
		pricing := infra.ModelPricing{
			PromptPrice:     2.0,
			CompletionPrice: 8.0,
			ReasoningPrice:  p(12.0),
			CachedPrice:     p(1.0),
		}

		result := infra.EstimateCost(tokens, pricing)

		wantPrompt := 1000.0 * 2.0 / 1_000_000.0
		wantCompletion := 500.0 * 8.0 / 1_000_000.0

		if !floatEqual(result.PromptCost, wantPrompt) {
			t.Errorf("PromptCost = %v, want %v", result.PromptCost, wantPrompt)
		}
		if !floatEqual(result.CompletionCost, wantCompletion) {
			t.Errorf("CompletionCost = %v, want %v", result.CompletionCost, wantCompletion)
		}
		if result.ReasoningCost != 0 {
			t.Errorf("ReasoningCost = %v, want 0", result.ReasoningCost)
		}
		if result.CachedCost != 0 {
			t.Errorf("CachedCost = %v, want 0", result.CachedCost)
		}
		wantTotal := wantPrompt + wantCompletion
		if !floatEqual(result.TotalCost, wantTotal) {
			t.Errorf("TotalCost = %v, want %v", result.TotalCost, wantTotal)
		}
	})
}
