package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/core"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// InternalServiceInterface 内部服务抽象
// 实现者：internal/controlplane/service.InternalService
type InternalServiceInterface interface {
	ListAvailableModels() ([]service.AvailableModel, error)
	GetModelHealth(names []string) (map[string]service.ModelHealthInfo, error)
}

// PricingServiceInterface 定价查询接口
type PricingServiceInterface interface {
	GetAllPricing(ctx context.Context) (map[string]core.ModelPricing, error)
}

// InternalHandler 内部接口 Handler
type InternalHandler struct {
	svc         InternalServiceInterface
	keySvc      KeyServiceInterface
	keyGroupSvc KeyGroupServiceInterface
	pricingSvc  PricingServiceInterface
}

type KeyGroupServiceInterface interface {
	GetKeyGroups(ctx context.Context, keyID uuid.UUID) ([]service.BoundGroupItem, error)
}

func NewInternalHandler(svc InternalServiceInterface, keySvc KeyServiceInterface, keyGroupSvc KeyGroupServiceInterface) *InternalHandler {
	return &InternalHandler{svc: svc, keySvc: keySvc, keyGroupSvc: keyGroupSvc}
}

// SetPricingService 注入定价服务（可选）
func (h *InternalHandler) SetPricingService(svc PricingServiceInterface) {
	h.pricingSvc = svc
}

// ListAvailableModels GET /internal/v1/models/available（T046）
func (h *InternalHandler) ListAvailableModels(c *gin.Context) {
	models, err := h.svc.ListAvailableModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to list available models",
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": models})
}

// GetModelHealth GET /internal/v1/models/health?names=a,b,c
func (h *InternalHandler) GetModelHealth(c *gin.Context) {
	names := c.QueryArray("names")
	if len(names) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": map[string]service.ModelHealthInfo{}})
		return
	}
	result, err := h.svc.GetModelHealth(names)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to get model health",
				"code":    CodeInternalError,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// VerifyKey POST /internal/v1/keys/verify
func (h *InternalHandler) VerifyKey(c *gin.Context) {
	var input struct {
		Key string `json:"key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": err.Error(),
				"code":    CodeBadRequest,
			},
		})
		return
	}

	result, err := h.keySvc.VerifyKey(c.Request.Context(), input.Key)
	if err != nil {
		if errors.Is(err, service.ErrKeyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found",
					"message": "key not found",
					"code":    CodeKeyNotFound,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to verify key",
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *InternalHandler) GetKeyGroups(c *gin.Context) {
	keyIDStr := c.Param("id")
	keyID, err := uuid.Parse(keyIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid key_id",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	groups, err := h.keyGroupSvc.GetKeyGroups(c.Request.Context(), keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found",
				"message": "key not found",
				"code":    CodeKeyNotFound,
			},
		})
		return
	}

	c.JSON(http.StatusOK, groups)
}

// GetModelPricing GET /internal/v1/models/pricing
// 返回全量模型定价数据，供 gateway-api 和 observability-hub 消费
func (h *InternalHandler) GetModelPricing(c *gin.Context) {
	if h.pricingSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"type":    "service_unavailable",
				"message": "pricing service not initialized",
				"code":    CodeInternalError,
			},
		})
		return
	}

	pricing, err := h.pricingSvc.GetAllPricing(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to get pricing",
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, pricing)
}
