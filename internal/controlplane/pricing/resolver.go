package pricing

import (
	"context"
	"log/slog"
	"sync"

	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/core"
)

// PricingTable 内存价表，由 PricingService 5min 定时刷新
type PricingTable struct {
	mu   sync.RWMutex
	data map[string]core.ModelPricing // key: model name
}

// NewPricingTable 创建内存价表
func NewPricingTable() *PricingTable {
	return &PricingTable{
		data: make(map[string]core.ModelPricing),
	}
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
	}

	var rows []row
	err := db.WithContext(ctx).
		Table("models m").
		Select("m.name AS model_name, m.price_mode, m.input_price, m.output_price, " +
			"c.input_price_per_million, c.output_price_per_million, " +
			"c.cache_read_price, c.cache_write_price, " +
			"c.tier_threshold, c.input_price_above_tier, c.output_price_above_tier").
		Joins("LEFT JOIN model_catalog c ON m.catalog_id = c.id").
		Find(&rows).Error
	if err != nil {
		return err
	}

	newData := make(map[string]core.ModelPricing, len(rows))
	for _, r := range rows {
		p := ResolveModelPricing(
			r.PriceMode, r.InputPrice, r.OutputPrice,
			r.InputPricePerMillion, r.OutputPricePerMillion,
			r.CacheReadPrice, r.CacheWritePrice,
			r.TierThreshold, r.InputPriceAboveTier, r.OutputPriceAboveTier,
		)
		// 同名模型多条（如不同 provider）时，空定价不应覆盖已有有效定价
		if existing, ok := newData[r.ModelName]; ok {
			if p.InputPerMtok == nil && existing.InputPerMtok != nil {
				continue
			}
		}
		newData[r.ModelName] = p
	}

	t.mu.Lock()
	t.data = newData
	t.mu.Unlock()

	slog.InfoContext(ctx, "pricing_table_refreshed", "count", len(newData))
	return nil
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