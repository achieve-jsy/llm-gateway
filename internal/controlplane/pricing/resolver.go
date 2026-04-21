package pricing

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/core"
)

// PricingTable 内存价表，由 PricingService 5min 定时刷新
type PricingTable struct {
	mu           sync.RWMutex
	data         map[string]core.ModelPricing // only unambiguous model names
	providerData map[string]core.ModelPricing // provider selector + model
}

// NewPricingTable 创建内存价表
func NewPricingTable() *PricingTable {
	return &PricingTable{
		data:         make(map[string]core.ModelPricing),
		providerData: make(map[string]core.ModelPricing),
	}
}

func (t *PricingTable) ResolveForProvider(modelKey, provider string) (core.ModelPricing, bool) {
	if strings.TrimSpace(provider) == "" {
		return t.Resolve(modelKey)
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.providerData[pricingProviderKey(provider, modelKey)]
	return p, ok
}

// Resolve 查询模型定价
func (t *PricingTable) Resolve(modelKey string) (core.ModelPricing, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.data[modelKey]
	return p, ok
}

// GetModelPricing 实现 middleware.PricingResolver 接口，供 budget pre-deduction 使用。
// 查不到时返回 nil，调用方自行降级到 FallbackPricingResolver。
func (t *PricingTable) GetModelPricing(model string) *core.ModelPricing {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.data[model]
	if !ok || (p.InputPerMtok == nil && p.OutputPerMtok == nil) {
		return nil
	}
	return &p
}

// GetAll 返回全量价表的副本
func (t *PricingTable) GetAll() map[string]core.ModelPricing {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]core.ModelPricing, len(t.data))
	for k, v := range t.data {
		out[k] = v
	}
	return out
}

// Refresh 从数据库全量刷新价表（atomic swap）
func (t *PricingTable) Refresh(ctx context.Context, db *gorm.DB) error {
	type row struct {
		ModelName             string   `gorm:"column:model_name"`
		ProviderName          string   `gorm:"column:provider_name"`
		ProviderType          string   `gorm:"column:provider_type"`
		ModelType             string   `gorm:"column:model_type"`
		PriceMode             string   `gorm:"column:price_mode"`
		InputPrice            float64  `gorm:"column:input_price"`
		OutputPrice           float64  `gorm:"column:output_price"`
		InputPricePerMillion  *float64 `gorm:"column:input_price_per_million"`
		OutputPricePerMillion *float64 `gorm:"column:output_price_per_million"`
		CacheReadPrice        *float64 `gorm:"column:cache_read_price"`
		CacheWritePrice       *float64 `gorm:"column:cache_write_price"`
		TierThreshold         *int64   `gorm:"column:tier_threshold"`
		InputPriceAboveTier   *float64 `gorm:"column:input_price_above_tier"`
		OutputPriceAboveTier  *float64 `gorm:"column:output_price_above_tier"`
		CatalogRawPayload     string   `gorm:"column:catalog_raw_payload"`
	}

	var rows []row
	err := db.WithContext(ctx).
		Table("models m").
		Select("m.name AS model_name, p.name AS provider_name, p.type AS provider_type, " +
			"m.model_type, m.price_mode, m.input_price, m.output_price, " +
			"c.input_price_per_million, c.output_price_per_million, " +
			"c.cache_read_price, c.cache_write_price, " +
			"c.tier_threshold, c.input_price_above_tier, c.output_price_above_tier, " +
			"c.raw_payload AS catalog_raw_payload").
		Joins("JOIN providers p ON p.id = m.provider_id").
		Joins("LEFT JOIN model_catalog c ON m.catalog_id = c.id").
		Find(&rows).Error
	if err != nil {
		return err
	}

	newData := make(map[string]core.ModelPricing, len(rows))
	providerData := make(map[string]core.ModelPricing, len(rows)*2)
	ambiguousModels := make(map[string]bool)
	ambiguousProviderKeys := make(map[string]bool)
	for _, r := range rows {
		p := ResolveModelPricing(
			r.PriceMode, r.InputPrice, r.OutputPrice,
			r.InputPricePerMillion, r.OutputPricePerMillion,
			r.CacheReadPrice, r.CacheWritePrice,
			r.TierThreshold, r.InputPriceAboveTier, r.OutputPriceAboveTier,
		)
		usagePricing := ResolveUsagePricing(UsagePricingInput{
			ModelType: r.ModelType, PriceMode: r.PriceMode,
			InputPrice: r.InputPrice, CatalogRawPayload: r.CatalogRawPayload,
		})
		if usagePricing.RerankPerSearchUnit != nil {
			p.RerankPerSearchUnit = usagePricing.RerankPerSearchUnit
		}
		if len(p.FieldSources("database")) == 0 {
			continue
		}
		addUniquePricing(newData, ambiguousModels, r.ModelName, p)
		addUniquePricing(providerData, ambiguousProviderKeys, pricingProviderKey(r.ProviderName, r.ModelName), p)
		addUniquePricing(providerData, ambiguousProviderKeys, pricingProviderKey(r.ProviderType, r.ModelName), p)
	}

	t.mu.Lock()
	t.data = newData
	t.providerData = providerData
	t.mu.Unlock()

	slog.InfoContext(ctx, "pricing_table_refreshed", "count", len(newData))
	return nil
}

func pricingProviderKey(provider, model string) string {
	return strings.TrimSpace(provider) + "\x00" + strings.TrimSpace(model)
}

func addUniquePricing(
	target map[string]core.ModelPricing,
	ambiguous map[string]bool,
	key string,
	value core.ModelPricing,
) {
	if key == "" || ambiguous[key] {
		return
	}
	if _, exists := target[key]; exists {
		delete(target, key)
		ambiguous[key] = true
		return
	}
	target[key] = value
}

type UsagePricingInput struct {
	ModelType         string
	PriceMode         string
	InputPrice        float64
	CatalogRawPayload string
}

// ResolveUsagePricing resolves non-token price buckets. For rerank and
// rerank_vl models, manual input_price is the custom per-search-unit price;
// catalog mode reads the corresponding LiteLLM raw payload bucket.
func ResolveUsagePricing(input UsagePricingInput) core.ModelPricing {
	modelType := strings.ToLower(strings.TrimSpace(input.ModelType))
	if modelType != "rerank" && modelType != "rerank_vl" {
		return core.ModelPricing{}
	}
	if strings.EqualFold(input.PriceMode, "manual") {
		if input.InputPrice <= 0 {
			return core.ModelPricing{}
		}
		price := input.InputPrice
		return core.ModelPricing{RerankPerSearchUnit: &price}
	}
	var raw map[string]any
	if json.Unmarshal([]byte(input.CatalogRawPayload), &raw) != nil {
		return core.ModelPricing{}
	}
	for _, key := range []string{
		"rerank_per_search_unit",
		"input_cost_per_search_unit",
		"rerank_cost_per_query",
		"input_cost_per_query",
	} {
		if value, ok := raw[key].(float64); ok && value > 0 {
			price := value
			return core.ModelPricing{RerankPerSearchUnit: &price}
		}
	}
	return core.ModelPricing{}
}

// ResolveModelPricing 解析单个模型定价（catalog 或 manual 模式）
func ResolveModelPricing(
	priceMode string,
	inputPrice, outputPrice float64,
	inputPricePerMillion, outputPricePerMillion *float64,
	cacheReadPrice, cacheWritePrice *float64,
	tierThreshold *int64,
	inputPriceAboveTier, outputPriceAboveTier *float64,
) core.ModelPricing {
	if priceMode == "manual" {
		if inputPrice <= 0 && outputPrice <= 0 {
			return core.ModelPricing{} // 缺价
		}
		p := core.ModelPricing{
			InputPerMtok:  &inputPrice,
			OutputPerMtok: &outputPrice,
		}
		if cacheReadPrice != nil {
			p.CachedInputPerMtok = cacheReadPrice
		}
		if cacheWritePrice != nil {
			p.CacheWritePerMtok = cacheWritePrice
		}
		return p
	}

	// catalog 模式：从 model_catalog 解析
	if inputPricePerMillion == nil || outputPricePerMillion == nil {
		return core.ModelPricing{} // 未绑定 catalog → 缺价
	}

	inP := *inputPricePerMillion
	outP := *outputPricePerMillion

	p := core.ModelPricing{
		InputPerMtok:       &inP,
		OutputPerMtok:      &outP,
		CachedInputPerMtok: cacheReadPrice,
		CacheWritePerMtok:  cacheWritePrice,
	}

	if tierThreshold != nil && inputPriceAboveTier != nil && outputPriceAboveTier != nil {
		th := float64(*tierThreshold)
		inAT := *inputPriceAboveTier
		outAT := *outputPriceAboveTier
		p.Tiers = []core.ModelPricingTier{
			{
				UpToTokens:    &th,
				InputPerMtok:  &inAT,
				OutputPerMtok: &outAT,
			},
		}
	}

	return p
}
