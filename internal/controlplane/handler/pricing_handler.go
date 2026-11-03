package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// PricingHandler 模型定价查询 Handler。
//
// 对内接口，由 InternalAuth 保护，供 gateway-core 的 MemoryPricingCache
// 每 5 分钟轮询获取全部模型定价。
type PricingHandler struct {
	pricingSvc *service.PricingService
	modelRepo  service.ModelRepoInterface
}

func NewPricingHandler(pricingSvc *service.PricingService, modelRepo service.ModelRepoInterface) *PricingHandler {
	return &PricingHandler{
		pricingSvc: pricingSvc,
		modelRepo:  modelRepo,
	}
}

// GetAllPricing 返回所有模型的四桶定价。
//
// GET /internal/v1/pricing
//
// 响应格式：
//
//	{
//	  "models": {
//	    "model-name": {
//	      "prompt_price": 0.15,
//	      "completion_price": 0.6,
//	      "reasoning_price": null,
//	      "cached_price": null,
//	      "pricing_missing": false,
//	      "fallback_reasoning": true
//	    },
//	    ...
//	  }
//	}
func (h *PricingHandler) GetAllPricing(c *gin.Context) {
	models, err := h.modelRepo.ListAvailable()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to list models: " + err.Error(),
				"code":    CodeInternalError,
			},
		})
		return
	}

	result := make(map[string]*model.ModelPricing, len(models))
	for _, m := range models {
		pricing, err := h.pricingSvc.ResolveModelPricing(c.Request.Context(), m.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"type":    "internal_error",
					"message": "failed to resolve pricing for model " + m.Name + ": " + err.Error(),
					"code":    CodeInternalError,
				},
			})
			return
		}
		result[m.Name] = pricing
	}

	c.JSON(http.StatusOK, gin.H{"models": result})
}
