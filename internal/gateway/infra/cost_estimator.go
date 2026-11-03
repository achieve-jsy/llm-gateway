package infra

type TokenBuckets struct {
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	CachedTokens     int
}

type CostBuckets struct {
	PromptCost        float64
	CompletionCost    float64
	ReasoningCost     float64
	CachedCost        float64
	TotalCost         float64
	FallbackReasoning bool
}

type ModelPricing struct {
	PromptPrice     float64
	CompletionPrice float64
	ReasoningPrice  *float64
	CachedPrice     *float64
	PricingMissing  bool
}

const perMillion = 1_000_000.0

func EstimateCost(tokens TokenBuckets, pricing ModelPricing) CostBuckets {
	if pricing.PromptPrice <= 0 || pricing.CompletionPrice <= 0 {
		return CostBuckets{}
	}

	b := CostBuckets{}
	b.PromptCost = float64(tokens.PromptTokens) * pricing.PromptPrice / perMillion
	b.CompletionCost = float64(tokens.CompletionTokens) * pricing.CompletionPrice / perMillion

	if pricing.ReasoningPrice != nil {
		b.ReasoningCost = float64(tokens.ReasoningTokens) * (*pricing.ReasoningPrice) / perMillion
	} else {
		b.ReasoningCost = float64(tokens.ReasoningTokens) * pricing.CompletionPrice / perMillion
		b.FallbackReasoning = true
	}

	if pricing.CachedPrice != nil {
		b.CachedCost = float64(tokens.CachedTokens) * (*pricing.CachedPrice) / perMillion
	}

	b.TotalCost = b.PromptCost + b.CompletionCost + b.ReasoningCost + b.CachedCost

	return b
}
