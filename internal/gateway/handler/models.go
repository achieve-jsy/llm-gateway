package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/gin-gonic/gin"
)

// ModelsCacheInterface Cache 抽象：handler 只关心读/写模型列表，便于测试替换
// 实现者：internal/gateway/infra.Cache
type ModelsCacheInterface interface {
	GetModelList(ctx context.Context) ([]core.Model, error)
	SetModelList(ctx context.Context, models []core.Model) error
	GetModelListWithVersion(ctx context.Context) ([]core.Model, int64, error)
	SetModelListWithVersion(ctx context.Context, models []core.Model, version int64) error
}

// ModelsRouterInterface abstracts the new GoModel Router for listing models.
// Implemented by *providers.Router.
type ModelsRouterInterface interface {
	ListModels(ctx context.Context) (*core.ModelsResponse, error)
}

// ModelsHandler 模型列表处理器
type ModelsHandler struct {
	cache    ModelsCacheInterface
	router   ModelsRouterInterface
	state    *provider.RuntimeState
}

// NewModelsHandler 创建 ModelsHandler 实例
func NewModelsHandler(cache ModelsCacheInterface, router ModelsRouterInterface, state *provider.RuntimeState) *ModelsHandler {
	return &ModelsHandler{
		cache:  cache,
		router: router,
		state:  state,
	}
}

// Handle 处理 GET /v1/models 请求
// 直接从 Router 获取模型列表
//
//	@Summary		List Models
//	@Description	列出所有可用模型，兼容 OpenAI /v1/models 接口
//	@Tags			Models
//	@Produce		json
//	@Success		200	{object}	core.ModelsResponse	"模型列表"
//	@Failure		401	{object}	map[string]interface{}	"认证失败"
//	@Security		BearerAuth
//	@Router			/v1/models [get]
func (h *ModelsHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	if h.router == nil {
		c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: []core.Model{}})
		return
	}

	resp, err := h.router.ListModels(ctx)
	if err != nil {
		slog.Warn("failed to list models from router", "error", err)
		c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: []core.Model{}})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// HandleLegacy handles GET /v1/health/models using a cache-first strategy,
// falling back to the router for a fresh model list.
func (h *ModelsHandler) HandleLegacy(c *gin.Context) {
	ctx := c.Request.Context()

	// 先查 Redis 缓存
	models, version, err := h.cache.GetModelListWithVersion(ctx)
	if err != nil {
		slog.Warn("获取模型列表缓存失败", "error", err)
	}

	currentVersion := int64(0)
	if h.state != nil {
		_, _, _, _, _, currentVersion = h.state.Snapshot()
	}

	// cache hit
	if models != nil && (currentVersion == 0 || version == currentVersion) {
		c.JSON(http.StatusOK, core.ModelsResponse{
			Object: "list",
			Data:   models,
		})
		return
	}

	// cache miss，从 Router 获取
	now := time.Now().Unix()
	var modelInfos []core.Model

	if h.router != nil {
		resp, rErr := h.router.ListModels(ctx)
		if rErr == nil && resp != nil {
			modelInfos = make([]core.Model, 0, len(resp.Data))
			for _, m := range resp.Data {
				modelInfos = append(modelInfos, core.Model{
					ID:      m.ID,
					Object:  "model",
					Created: now,
					OwnedBy: "llm-gateway",
				})
			}
		}
	}

	// 写入缓存（忽略错误）
	if len(modelInfos) > 0 {
		if err := h.cache.SetModelListWithVersion(ctx, modelInfos, currentVersion); err != nil {
			slog.Warn("设置模型列表缓存失败", "error", err)
		}
	}

	c.JSON(http.StatusOK, core.ModelsResponse{
		Object: "list",
		Data:   modelInfos,
	})
}
