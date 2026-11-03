package modeldata

import (
	"github.com/example/llm-gateway/internal/core"
)

// MergeMetadata merges override onto base field-wise. Non-zero override fields
// win; zero override fields preserve the base value. Capabilities and Rankings
// are merged key-by-key (override keys replace base keys). Returns a new
// *ModelMetadata; inputs are not mutated and the returned value never shares
// slice, map, or pointer backing storage with base or override.
//
// This supports config-driven overrides layered on top of remote-registry
// enrichment: operators can specify just the fields they care about (e.g.
// context_window for a local Ollama model) without clobbering unrelated fields.
func MergeMetadata(base, override *core.ModelMetadata) *core.ModelMetadata {
	if override == nil {
		return base.Clone()
	}
	if base == nil {
		return override.Clone()
	}

	merged := base.Clone()

	if override.DisplayName != "" {
		merged.DisplayName = override.DisplayName
	}
	if override.Description != "" {
		merged.Description = override.Description
	}
	if override.Family != "" {
		merged.Family = override.Family
	}
	if len(override.Modes) > 0 {
		merged.Modes = append([]string(nil), override.Modes...)
	}
	if len(override.Categories) > 0 {
		merged.Categories = append([]core.ModelCategory(nil), override.Categories...)
	}
	if len(override.Tags) > 0 {
		merged.Tags = append([]string(nil), override.Tags...)
	}
	if override.ContextWindow != nil {
		v := *override.ContextWindow
		merged.ContextWindow = &v
	}
	if override.MaxOutputTokens != nil {
		v := *override.MaxOutputTokens
		merged.MaxOutputTokens = &v
	}
	if len(override.Capabilities) > 0 {
		out := make(map[string]bool, len(merged.Capabilities)+len(override.Capabilities))
		for k, v := range merged.Capabilities {
			out[k] = v
		}
		for k, v := range override.Capabilities {
			out[k] = v
		}
		merged.Capabilities = out
	}
	if len(override.Rankings) > 0 {
		out := make(map[string]core.ModelRanking, len(merged.Rankings)+len(override.Rankings))
		// merged.Rankings is already a deep clone from base.Clone(), so we can
		// reuse its values directly; override values must be deep-cloned so the
		// result does not share Elo/Rank pointers with the caller's input.
		for k, v := range merged.Rankings {
			out[k] = v
		}
		for k, v := range override.Rankings {
			out[k] = core.CloneModelRanking(v)
		}
		merged.Rankings = out
	}
	if override.Pricing != nil {
		merged.Pricing = MergePricing(merged.Pricing, override.Pricing)
		if len(override.PricingSources) > 0 {
			merged.PricingSources = clonePricingSources(override.PricingSources)
		} else {
			merged.PricingSources = nil
		}
	}

	return merged
}

// MergePricing overlays only explicitly supplied price buckets. Provider-level
// overrides therefore do not erase registry buckets such as rerank search-unit
// pricing when they customize an unrelated token price.
func MergePricing(base, override *core.ModelPricing) *core.ModelPricing {
	if override == nil {
		return base.Clone()
	}
	if base == nil {
		return override.Clone()
	}
	out := base.Clone()
	if override.Currency != "" {
		out.Currency = override.Currency
	}
	mergeFloat := func(dst **float64, src *float64) {
		if src != nil {
			*dst = cloneFloat(src)
		}
	}
	mergeFloat(&out.InputPerMtok, override.InputPerMtok)
	mergeFloat(&out.OutputPerMtok, override.OutputPerMtok)
	mergeFloat(&out.CachedInputPerMtok, override.CachedInputPerMtok)
	mergeFloat(&out.CacheWritePerMtok, override.CacheWritePerMtok)
	mergeFloat(&out.ReasoningOutputPerMtok, override.ReasoningOutputPerMtok)
	mergeFloat(&out.BatchInputPerMtok, override.BatchInputPerMtok)
	mergeFloat(&out.BatchOutputPerMtok, override.BatchOutputPerMtok)
	mergeFloat(&out.AudioInputPerMtok, override.AudioInputPerMtok)
	mergeFloat(&out.AudioOutputPerMtok, override.AudioOutputPerMtok)
	mergeFloat(&out.PerImage, override.PerImage)
	mergeFloat(&out.InputPerImage, override.InputPerImage)
	mergeFloat(&out.PerSecondInput, override.PerSecondInput)
	mergeFloat(&out.PerSecondOutput, override.PerSecondOutput)
	mergeFloat(&out.PerCharacterInput, override.PerCharacterInput)
	mergeFloat(&out.PerRequest, override.PerRequest)
	mergeFloat(&out.PerPage, override.PerPage)
	mergeFloat(&out.RerankPerSearchUnit, override.RerankPerSearchUnit)
	mergeFloat(&out.InputPerImageToken, override.InputPerImageToken)
	mergeFloat(&out.OutputPerImageToken, override.OutputPerImageToken)
	mergeFloat(&out.InputPerPixel, override.InputPerPixel)
	mergeFloat(&out.OutputPerPixel, override.OutputPerPixel)
	mergeFloat(&out.InputPerVideoToken, override.InputPerVideoToken)
	mergeFloat(&out.OutputPerVideoToken, override.OutputPerVideoToken)
	if len(override.Tiers) > 0 {
		out.Tiers = override.Clone().Tiers
	}
	return out
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func clonePricingSources(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
