package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

// ModelsCacheInterface Cache 抽象：handler 只关心读/写模型列表，便于测试替换
// 实现者：internal/gateway/infra.Cache
type ModelsCacheInterface interface {
	GetModelList(ctx context.Context) ([]model.ModelInfo, error)
	SetModelList(ctx context.Context, models []model.ModelInfo) error
}

// ModelsRegistryInterface Registry 抽象：handler 只关心模型名清单
// 实现者：internal/gateway/provider.Registry
type ModelsRegistryInterface interface {
	ListAllModels() []string
}

// ModelsHandler 模型列表处理器
type ModelsHandler struct {
	cache    ModelsCacheInterface
	registry ModelsRegistryInterface
}

// NewModelsHandler 创建 ModelsHandler 实例
func NewModelsHandler(cache ModelsCacheInterface, registry ModelsRegistryInterface) *ModelsHandler {
	return &ModelsHandler{
		cache:    cache,
		registry: registry,
	}
}

// Handle 处理 GET /v1/models 请求
// 优先从 Redis 缓存获取，miss 时从 Registry 构建模型列表
//
//	@Summary		List Models
//	@Description	列出所有可用模型，兼容 OpenAI /v1/models 接口
//	@Tags			Models
//	@Produce		json
//	@Success		200	{object}	model.ModelsResponse	"模型列表"
//	@Failure		401	{object}	map[string]interface{}	"认证失败"
//	@Security		BearerAuth
//	@Router			/v1/models [get]
func (h *ModelsHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	// 先查 Redis 缓存
	models, err := h.cache.GetModelList(ctx)
	if err != nil {
		slog.Warn("获取模型列表缓存失败", "error", err)
	}

	// cache hit
	if models != nil {
		c.JSON(http.StatusOK, model.ModelsResponse{
			Object: "list",
			Data:   models,
		})
		return
	}

	// cache miss，从 Registry 构建模型列表
	modelNames := h.registry.ListAllModels()
	now := time.Now().Unix()
	models = make([]model.ModelInfo, 0, len(modelNames))
	for _, name := range modelNames {
		models = append(models, model.ModelInfo{
			ID:      name,
			Object:  "model",
			Created: now,
			OwnedBy: "llm-gateway",
		})
	}

	// 写入缓存（忽略错误）
	if err := h.cache.SetModelList(ctx, models); err != nil {
		slog.Warn("设置模型列表缓存失败", "error", err)
	}

	c.JSON(http.StatusOK, model.ModelsResponse{
		Object: "list",
		Data:   models,
	})
}
