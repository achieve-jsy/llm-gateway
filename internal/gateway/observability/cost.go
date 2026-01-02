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

// FallbackEstimateCost 使用保守固定单价估算成本（无真实定价时的兜底）。
// 固定单价：输入输出均为 $50/1M tokens（≈ $0.05/1K tokens）。
// 用途：
//   - budget_reserve：handler 解析 body 后、上游响应返回前的 preflight 预扣
//   - usage_collector post-hook：真实 pricing 查不到时仍能核销预算
// 设计考量：偏高单价 → 预算提前耗尽 → 用户可申请扩容（安全）；反之偏低会让预算被绕过。
func FallbackEstimateCost(u core.Usage) float64 {
	in := ExtractCostInput(u)
	// $50 per 1M tokens = 保守上限
	price := 50.0
	return perM(in.PromptTokens, price) + perM(in.CompletionTokens, price)
}