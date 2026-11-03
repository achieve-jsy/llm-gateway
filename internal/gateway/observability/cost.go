package observability

import (
	"strconv"

	"github.com/example/llm-gateway/internal/core"
)

// ExtractCostInput 从 Usage 归一化提取成本计算输入。
//
// 对齐 LiteLLM Usage.__init__ 的三套约定，以下字段均通过 RawUsage 传递：
//
// # OpenAI / OpenAI-compatible（含 Bedrock、Gemini、Moonshot、GLM 等）
//
//	prompt_tokens_details.cached_tokens  → CachedTokens
//	（prompt_tokens 已包含 cached，不需要补偿）
//
// # DeepSeek
//
//	prompt_cache_hit_tokens  → CachedTokens（优先级低于 OpenAI details）
//	prompt_cache_miss_tokens 仅用于交叉核验，不单独存储
//	（prompt_tokens = hit + miss，已包含两者，不需要补偿）
//
// # Anthropic
//
//	cache_read_input_tokens    → CachedTokens
//	cache_creation_input_tokens → CacheWriteTokens
//	Anthropic input_tokens 仅计非缓存部分，需加回 cached + write 才符合
//	"prompt_tokens 包含所有输入"的不变式（与 LiteLLM 保持一致）
//
// 优先级（cache read）：OpenAI details > DeepSeek top-level > Anthropic top-level
//
// 参考：https://github.com/BerriAI/litellm/blob/main/litellm/types/utils.py  Usage.__init__
// 同步状态：2026-06 对齐 LiteLLM commit e285e2b9
func ExtractCostInput(u core.Usage) core.CostInput {
	in := core.CostInput{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
	}

	// ── Cache write ──────────────────────────────────────────────────────────
	// Anthropic cache_creation_input_tokens（OpenAI 目前无对应字段）
	in.CacheWriteTokens = rawInt(u.RawUsage, "cache_creation_input_tokens")

	// ── Cache read ───────────────────────────────────────────────────────────
	// 1. OpenAI-compatible: prompt_tokens_details.cached_tokens（优先）
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		in.CachedTokens = u.PromptTokensDetails.CachedTokens
	}
	// 2. DeepSeek: top-level prompt_cache_hit_tokens
	if in.CachedTokens == 0 {
		in.CachedTokens = rawInt(u.RawUsage, "prompt_cache_hit_tokens")
	}
	// 3. Anthropic: top-level cache_read_input_tokens
	if in.CachedTokens == 0 {
		in.CachedTokens = rawInt(u.RawUsage, "cache_read_input_tokens")
	}

	// ── Anthropic prompt_tokens 补偿 ─────────────────────────────────────────
	// Anthropic 的 input_tokens 仅含未命中部分；加回 cached + write，
	// 使 prompt_tokens 满足「包含全部输入」的不变式，与 EstimateCost 的桶算法对齐。
	// 判断依据：存在 Anthropic 专属字段且 OpenAI details 为空（避免双重补偿）。
	isAnthropic := (rawInt(u.RawUsage, "cache_read_input_tokens") > 0 ||
		rawInt(u.RawUsage, "cache_creation_input_tokens") > 0) &&
		(u.PromptTokensDetails == nil || u.PromptTokensDetails.CachedTokens == 0)
	if isAnthropic {
		in.PromptTokens += in.CachedTokens + in.CacheWriteTokens
	}

	return in
}

// rawInt 从 map[string]any 中提取 int 值，不存在则返回 0
func rawInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

// EstimateCost 计算估算成本（单位 USD）
// 价格单位：USD / 1M tokens
// 始终分桶计算（即使自定义价），修正 LiteLLM 线上版缺失缓存折扣的缺陷
func EstimateCost(in core.CostInput, p *core.ModelPricing) (cost float64, missing bool) {
	if p == nil || p.InputPerMtok == nil || p.OutputPerMtok == nil {
		return 0, true
	}

	inP := *p.InputPerMtok
	outP := *p.OutputPerMtok

	// 阶梯价：超过门槛用阶梯单价
	inP, outP = applyTier(p, in.PromptTokens, inP, outP)

	// 缓存桶单价，缺失则回退到普通输入价
	cacheReadP := inP
	if p.CachedInputPerMtok != nil {
		cacheReadP = *p.CachedInputPerMtok
	}

	cacheWriteP := inP
	if p.CacheWritePerMtok != nil {
		cacheWriteP = *p.CacheWritePerMtok
	}

	// 未命中缓存的纯输入 = prompt - cached - cacheWrite
	uncached := in.PromptTokens - in.CachedTokens - in.CacheWriteTokens
	if uncached < 0 {
		uncached = 0
	}

	cost = perM(uncached, inP) +
		perM(in.CachedTokens, cacheReadP) +
		perM(in.CacheWriteTokens, cacheWriteP) +
		perM(in.CompletionTokens, outP)

	return cost, false
}

// applyTier 应用阶梯定价
// 按 PromptTokens 匹配阶梯档位：小于等于 UpToTokens 阈值则使用该档价格
func applyTier(p *core.ModelPricing, totalPrompt int, inP, outP float64) (float64, float64) {
	if len(p.Tiers) == 0 {
		return inP, outP
	}
	for _, tier := range p.Tiers {
		if tier.UpToTokens != nil && totalPrompt <= int(*tier.UpToTokens) {
			if tier.InputPerMtok != nil {
				inP = *tier.InputPerMtok
			}
			if tier.OutputPerMtok != nil {
				outP = *tier.OutputPerMtok
			}
			return inP, outP
		}
	}
	return inP, outP
}

func perM(tokens int, pricePerMtok float64) float64 {
	return float64(tokens) / 1e6 * pricePerMtok
}

// CtxKeyMediaUsage 是媒体链路写入媒体用量的 gin context key。
//
// 图像/视频处理链在拿到上游终态用量后写入（失败/超时不写）：
//
//	c.Set(observability.CtxKeyMediaUsage, core.MediaCostInput{Images: len(data)})
//
// 采集器 NewUsageEvent 读取它并交给 EstimateMediaCost，按「每张/每秒/每次」
// 的媒体口径计价，而不是按 token 口径。
const CtxKeyMediaUsage = "media_usage"

// EstimateMediaCost 按媒体口径计算成本（单位 USD）。
//
// 计价桶（一个用量维度对应一个单价字段）：
//
//	Images            × PerImage（缺失时回退 InputPerImage，见下）
//	InputImages       × InputPerImage
//	InputSeconds      × PerSecondInput
//	OutputSeconds     × PerSecondOutput
//	Requests          × PerRequest
//	InputImageTokens  × InputPerImageToken
//	OutputImageTokens × OutputPerImageToken
//
// 图像张数的回退：LiteLLM 对部分图像模型把「每张价」放在 input_cost_per_image
// （例如 dall-e-3 只有 input_cost_per_image = 0.04，imagen 才是 output_cost_per_image），
// 因此 Images 桶优先用 PerImage，缺失/非正时回退 InputPerImage；InputImages 桶
// 只用 InputPerImage，不做反向回退，避免把生成价重复算到输入图上。
//
// 无价语义（修复"静默 0 元"）：
//   - 某个桶有用量、但对应单价缺失或 <= 0 → 整体 missing=true、cost=0；
//     绝不能因为「字段存在」（例如 LiteLLM 用 0.0 占位、或 DB 里存了 0）
//     就当作有价；
//   - 所有桶都没有用量 → 同样 missing=true：拿不到用量时不能当作免费。
//
// 注意：本函数只处理媒体口径，不改变 EstimateCost 的 token 语义。
func EstimateMediaCost(in core.MediaCostInput, p *core.ModelPricing) (cost float64, missing bool) {
	inputCost, outputCost, missing := estimateMediaCostBuckets(in, p)
	return inputCost + outputCost, missing
}

// estimateMediaCostBuckets 与 EstimateMediaCost 同口径，额外拆出输入/输出分项，
// 供埋点写 InputCostUSD / OutputCostUSD（前端成本分析）。
func estimateMediaCostBuckets(in core.MediaCostInput, p *core.ModelPricing) (inputCost, outputCost float64, missing bool) {
	if p == nil {
		return 0, 0, true
	}
	buckets := []struct {
		units float64
		price *float64
		input bool
	}{
		{float64(in.Images), imageCountPrice(p), false},
		{float64(in.InputImages), p.InputPerImage, true},
		{in.InputSeconds, p.PerSecondInput, true},
		{in.OutputSeconds, p.PerSecondOutput, false},
		{float64(in.Requests), p.PerRequest, true},
		{float64(in.InputImageTokens), p.InputPerImageToken, true},
		{float64(in.OutputImageTokens), p.OutputPerImageToken, false},
	}
	used := false
	for _, bucket := range buckets {
		if bucket.units <= 0 {
			continue
		}
		if bucket.price == nil || *bucket.price <= 0 {
			return 0, 0, true
		}
		amount := bucket.units * *bucket.price
		if bucket.input {
			inputCost += amount
		} else {
			outputCost += amount
		}
		used = true
	}
	if !used {
		return 0, 0, true
	}
	return inputCost, outputCost, false
}

// imageCountPrice 返回「每张生成图」的单价：优先 output（per_image），
// 缺失或非正时回退 input（input_cost_per_image）—— LiteLLM 对 dall-e-3 等
// 模型就是只给 input_cost_per_image。
func imageCountPrice(p *core.ModelPricing) *float64 {
	if p.PerImage != nil && *p.PerImage > 0 {
		return p.PerImage
	}
	if p.InputPerImage != nil && *p.InputPerImage > 0 {
		return p.InputPerImage
	}
	return nil
}
