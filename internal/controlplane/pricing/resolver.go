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
//
// 判定改用 FieldSources：只有 token 价的模型行为不变，但只配了媒体价
// （per_image / per_second_* / per_request）或 rerank 单价的模型不再被判为「查不到」。
func (t *PricingTable) GetModelPricing(model string) *core.ModelPricing {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.data[model]
	if !ok || len(p.FieldSources("database")) == 0 {
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
		CatalogMode           string   `gorm:"column:catalog_mode"`
		PriceMode             string   `gorm:"column:price_mode"`
		InputPrice            float64  `gorm:"column:input_price"`
		OutputPrice           float64  `gorm:"column:output_price"`
		ModelCacheReadPrice   *float64 `gorm:"column:model_cache_read_price"`
		ModelCacheWritePrice  *float64 `gorm:"column:model_cache_write_price"`
		InputPricePerMillion  *float64 `gorm:"column:input_price_per_million"`
		OutputPricePerMillion *float64 `gorm:"column:output_price_per_million"`
		CacheReadPrice        *float64 `gorm:"column:cache_read_price"`
		CacheWritePrice       *float64 `gorm:"column:cache_write_price"`
		TierThreshold         *int64   `gorm:"column:tier_threshold"`
		InputPriceAboveTier   *float64 `gorm:"column:input_price_above_tier"`
		OutputPriceAboveTier  *float64 `gorm:"column:output_price_above_tier"`
		PerImage              *float64 `gorm:"column:per_image"`
		InputPerImage         *float64 `gorm:"column:input_per_image"`
		PerSecondInput        *float64 `gorm:"column:per_second_input"`
		PerSecondOutput       *float64 `gorm:"column:per_second_output"`
		PerRequest            *float64 `gorm:"column:per_request"`
		InputPerImageToken    *float64 `gorm:"column:input_per_image_token"`
		OutputPerImageToken   *float64 `gorm:"column:output_per_image_token"`
		InputPerPixel         *float64 `gorm:"column:input_per_pixel"`
		OutputPerPixel        *float64 `gorm:"column:output_per_pixel"`
		InputPerVideoToken    *float64 `gorm:"column:input_per_video_token"`
		OutputPerVideoToken   *float64 `gorm:"column:output_per_video_token"`
		PerCharacterInput     *float64 `gorm:"column:per_character_input"`
		CatalogRawPayload     string   `gorm:"column:catalog_raw_payload"`
	}

	var rows []row
	err := db.WithContext(ctx).
		Table("models m").
		Select("m.name AS model_name, p.name AS provider_name, p.type AS provider_type, " +
			"m.model_type, m.price_mode, m.input_price, m.output_price, " +
			"m.cache_read_price AS model_cache_read_price, m.cache_write_price AS model_cache_write_price, " +
			"c.input_price_per_million, c.output_price_per_million, " +
			"c.cache_read_price, c.cache_write_price, " +
			"COALESCE(c.mode, '') AS catalog_mode, " +
			"c.tier_threshold, c.input_price_above_tier, c.output_price_above_tier, " +
			"c.per_image, c.input_per_image, c.per_second_input, c.per_second_output, c.per_request, " +
			"c.input_per_image_token, c.output_per_image_token, " +
			"c.input_per_pixel, c.output_per_pixel, " +
			"c.input_per_video_token, c.output_per_video_token, c.per_character_input, " +
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
			r.PriceMode, r.InputPrice, r.OutputPrice, r.ModelCacheReadPrice, r.ModelCacheWritePrice,
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
		ApplyCatalogMediaPricing(&p, MediaPricingInput{
			ModelType:           r.ModelType,
			CatalogMode:         r.CatalogMode,
			PriceMode:           r.PriceMode,
			PerImage:            r.PerImage,
			InputPerImage:       r.InputPerImage,
			PerSecondInput:      r.PerSecondInput,
			PerSecondOutput:     r.PerSecondOutput,
			PerRequest:          r.PerRequest,
			InputPerImageToken:  r.InputPerImageToken,
			OutputPerImageToken: r.OutputPerImageToken,
			InputPerPixel:       r.InputPerPixel,
			OutputPerPixel:      r.OutputPerPixel,
			InputPerVideoToken:  r.InputPerVideoToken,
			OutputPerVideoToken: r.OutputPerVideoToken,
			PerCharacterInput:   r.PerCharacterInput,
		})
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

// MediaPricingInput 媒体价解析入参（对应 model_catalog 的媒体价列）。
type MediaPricingInput struct {
	// ModelType 是 models.model_type（image/video/chat/…），仅在 catalog mode 缺失时兜底。
	ModelType string
	// CatalogMode 是 catalog 的权威判别字段：image_generation / image_edit /
	// video_generation / chat / embedding / rerank / audio_* / …
	CatalogMode     string
	PriceMode       string
	PerImage        *float64
	InputPerImage   *float64
	PerSecondInput  *float64
	PerSecondOutput *float64
	PerRequest      *float64
	// 媒体补充维度（只为解析/透传，是否参与计价见 cost.go）
	InputPerImageToken  *float64
	OutputPerImageToken *float64
	InputPerPixel       *float64
	OutputPerPixel      *float64
	InputPerVideoToken  *float64
	OutputPerVideoToken *float64
	PerCharacterInput   *float64
}

// 媒体模式白名单：直接消费 catalog 的 mode，而不是靠模型名/厂商启发式猜类型。
var mediaCatalogModes = map[string]struct{}{
	"image_generation": {},
	"image_edit":       {},
	"video_generation": {},
}

// ApplyCatalogMediaPricing 按 catalog 的 mode 把媒体价填入 p。
//
// 口径（与 docs/guardrails.md A3、docs/media-channels-integration-design.md §8 一致）：
//   - 判别依据优先用 catalog.Mode（image_generation/image_edit → 图像价，
//     video_generation → 视频价）；mode 为空（未绑定 catalog）时回退 model_type=image/video。
//     chat/embedding/rerank/audio_* 一律不动，保证现有 token 与 rerank 计价行为完全不变；
//   - 只填非空且 > 0 的单价，不动 p 上任何 token/rerank 字段；
//   - price_mode = manual 暂不解析媒体价（models 表还没有媒体价列，
//     设计文档 §7.2 的 billing_unit/unit_price_usd 尚未落地）→ 留空 = 无价，
//     由计价层判定 usage_missing，绝不静默记 0 元。
//
// 注意：媒体单价是「每张 / 每秒 / 每次 / 每 token」的美元原值，不做任何 token 缩放。
func ApplyCatalogMediaPricing(p *core.ModelPricing, input MediaPricingInput) {
	if p == nil {
		return
	}
	if !isMediaPricingScope(input) {
		return
	}
	if strings.EqualFold(input.PriceMode, "manual") {
		return
	}
	if price := positivePrice(input.PerImage); price != nil {
		p.PerImage = price
	}
	if price := positivePrice(input.InputPerImage); price != nil {
		p.InputPerImage = price
	}
	if price := positivePrice(input.PerSecondInput); price != nil {
		p.PerSecondInput = price
	}
	if price := positivePrice(input.PerSecondOutput); price != nil {
		p.PerSecondOutput = price
	}
	if price := positivePrice(input.PerRequest); price != nil {
		p.PerRequest = price
	}
	if price := positivePrice(input.InputPerImageToken); price != nil {
		p.InputPerImageToken = price
	}
	if price := positivePrice(input.OutputPerImageToken); price != nil {
		p.OutputPerImageToken = price
	}
	if price := positivePrice(input.InputPerPixel); price != nil {
		p.InputPerPixel = price
	}
	if price := positivePrice(input.OutputPerPixel); price != nil {
		p.OutputPerPixel = price
	}
	if price := positivePrice(input.InputPerVideoToken); price != nil {
		p.InputPerVideoToken = price
	}
	if price := positivePrice(input.OutputPerVideoToken); price != nil {
		p.OutputPerVideoToken = price
	}
	if price := positivePrice(input.PerCharacterInput); price != nil {
		p.PerCharacterInput = price
	}
}

// isMediaPricingScope 判定该模型是否走媒体价口径。
func isMediaPricingScope(input MediaPricingInput) bool {
	if mode := strings.ToLower(strings.TrimSpace(input.CatalogMode)); mode != "" {
		_, ok := mediaCatalogModes[mode]
		return ok
	}
	switch strings.ToLower(strings.TrimSpace(input.ModelType)) {
	case "image", "video":
		return true
	default:
		return false
	}
}

// positivePrice 过滤 nil 与 <= 0 的单价：0 在 catalog 里表达「无价/占位」，
// 不能当成有价，否则会重新引入静默 0 元。
func positivePrice(price *float64) *float64 {
	if price == nil || *price <= 0 {
		return nil
	}
	value := *price
	return &value
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
	modelCacheReadPrice, modelCacheWritePrice *float64,
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
		if modelCacheReadPrice != nil {
			p.CachedInputPerMtok = modelCacheReadPrice
		} else if cacheReadPrice != nil {
			p.CachedInputPerMtok = cacheReadPrice
		}
		if modelCacheWritePrice != nil {
			p.CacheWritePerMtok = modelCacheWritePrice
		} else if cacheWritePrice != nil {
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
