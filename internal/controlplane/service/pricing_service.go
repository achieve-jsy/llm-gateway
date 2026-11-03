package service

import (
	"context"
	"fmt"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

// PricingService 模型定价解析服务。
//
// 职责：根据 price_mode（catalog/manual）从 model 或关联的 model_catalog 中
// 解析出四桶价格（prompt/completion/reasoning/cached），并应用 fallback 规则。
type PricingService struct {
	modelRepo ModelRepoInterface
}

func NewPricingService(modelRepo ModelRepoInterface) *PricingService {
	return &PricingService{modelRepo: modelRepo}
}

// ResolveModelPricing 解析指定 model 的四桶价格。
//
//   - catalog mode：从 model.Catalog（需 preload）读取价格。
//   - manual mode：直接从 model 的价格字段读取。
//
// Fallback 规则：
//   - ReasoningPrice == nil → FallbackReasoning = true
//   - CachedPrice == nil → 保持 nil（调用方当作 0）
//   - PromptPrice == 0 或 CompletionPrice == 0 → PricingMissing = true
func (s *PricingService) ResolveModelPricing(ctx context.Context, modelID uuid.UUID) (*model.ModelPricing, error) {
	m, err := s.modelRepo.GetByID(modelID)
	if err != nil {
		return nil, fmt.Errorf("get model %s: %w", modelID, err)
	}
	if m == nil {
		return nil, fmt.Errorf("model %s not found", modelID)
	}

	result := &model.ModelPricing{}

	switch m.PriceMode {
	case model.PriceModeCatalog:
		if m.Catalog == nil {
			result.PricingMissing = true
			break
		}
		result.PromptPrice = m.Catalog.InputPricePerMillion
		result.CompletionPrice = m.Catalog.OutputPricePerMillion
		result.ReasoningPrice = m.Catalog.ReasoningPrice
		result.CachedPrice = m.Catalog.CachedPrice

	case model.PriceModeManual:
		result.PromptPrice = m.InputPrice
		result.CompletionPrice = m.OutputPrice
		result.ReasoningPrice = m.ReasoningPrice
		result.CachedPrice = m.CachedPrice

	default:
		result.PricingMissing = true
	}

	if result.ReasoningPrice == nil {
		result.FallbackReasoning = true
	}
	if result.PromptPrice == 0 {
		result.PricingMissing = true
	}
	if result.CompletionPrice == 0 {
		result.PricingMissing = true
	}

	return result, nil
}
