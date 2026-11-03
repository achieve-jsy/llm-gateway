package model

// ModelPricing 四桶价格结构（以百万 token 为单位，USD）。
//
//   - PromptPrice：每百万 prompt token 价格
//   - CompletionPrice：每百万 completion token 价格
//   - ReasoningPrice：每百万 reasoning token 价格（nil 表示 fallback 到 completion 价格）
//   - CachedPrice：每百万 cached token 价格（nil 表示 0，不收费）
//   - PricingMissing：prompt 或 completion 价格不可用时为 true
//   - FallbackReasoning：reasoning 价格回退到 completion 价格时为 true
type ModelPricing struct {
	PromptPrice        float64  `json:"prompt_price"`
	CompletionPrice    float64  `json:"completion_price"`
	ReasoningPrice     *float64 `json:"reasoning_price"`
	CachedPrice        *float64 `json:"cached_price"`
	PricingMissing     bool     `json:"pricing_missing"`
	FallbackReasoning  bool     `json:"fallback_reasoning"`
}

const (
	// PriceModeCatalog 表示价格来自 catalog 同步。
	PriceModeCatalog = "catalog"
	// PriceModeManual 表示价格由管理员手工设置。
	PriceModeManual = "manual"
)
