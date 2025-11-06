package router

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/auth"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/middleware"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Setup 初始化 Gin 路由和中间件链
func Setup(db *gorm.DB, rdb *redis.Client) *gin.Engine {
	r := gin.New()

	// 全局中间件
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	r.Use(middleware.RequestID())

	// 健康检查（无需认证）
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// SSO/OAuth2 认证路由（无需认证）
	authCfg := auth.LoadConfig()
	authHandler, authErr := auth.NewHandler(authCfg)
	if authErr != nil {
		log.Printf("WARNING: SSO auth handler init failed: %v (SSO login disabled)", authErr)
	} else {
		authGroup := r.Group("/auth")
		{
			authGroup.GET("/login", authHandler.Login)
			authGroup.GET("/callback", authHandler.Callback)
		}
		// 需要认证的用户信息接口
		r.GET("/api/v1/me", middleware.JWTAuth(), authHandler.GetCurrentUser)
	}

	// 初始化依赖
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	defRepo := repository.NewProviderDefinitionRepo(db)
	modelTagRepo := repository.NewModelTagRepo(db)
	healthCheckRepo := repository.NewHealthCheckRepo(db)

	// 创建事件发布器
	publisher := event.NewPublisher(rdb)

	providerSvc := service.NewProviderService(providerRepo, modelRepo, publisher)
	modelSvc := service.NewModelService(modelRepo, providerRepo, publisher)
	internalSvc := service.NewInternalService(modelRepo, providerRepo)
	healthCheckSvc := service.NewHealthCheckService(healthCheckRepo, providerRepo, modelRepo)

	providerHandler := handler.NewProviderHandler(providerSvc)
	modelHandler := handler.NewModelHandler(modelSvc)
	internalHandler := handler.NewInternalHandler(internalSvc)
	defHandler := handler.NewDefinitionHandler(defRepo)
	testHandler := handler.NewTestHandler(providerRepo, modelRepo)
	modelTagHandler := handler.NewModelTagHandler(modelTagRepo)
	healthCheckHandler := handler.NewHealthCheckHandler(healthCheckSvc)
	modelCheckHandler := handler.NewModelCheckHandler(modelRepo, providerRepo, healthCheckRepo)
	syncSvc := service.NewSyncService(providerRepo, modelRepo, publisher)
	syncHandler := handler.NewSyncHandler(syncSvc)

	// 模型 Catalog（D1: LiteLLM 单源；D3: bundled fallback；D4: 启动异步首刷 + 24h cron + 手动）
	catalogRepo := repository.NewModelCatalogRepo(db)
	catalogSyncSvc := service.NewCatalogSyncService(catalogRepo, modelRepo, service.CatalogSyncOpts{})
	catalogSvc := service.NewCatalogService(catalogRepo, catalogSyncSvc)
	catalogHandler := handler.NewCatalogHandler(catalogSvc)
	// 让 ModelService 也能解析 catalog（CreateModelFromCatalog/ResetOverride）
	modelSvc.SetCatalogLookup(catalogRepo)

	// 启动异步首刷（不阻塞 HTTP 启动），失败回退 bundled
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := catalogSyncSvc.SyncFromRemote(ctx); err != nil {
			slog.Warn("initial catalog sync failed", "error", err)
		}
	}()

	// 24h cron
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if _, err := catalogSyncSvc.SyncFromRemote(ctx); err != nil {
				slog.Warn("scheduled catalog sync failed", "error", err)
			}
			cancel()
		}
	}()

	// 启动健康检测调度器（优先使用环境变量 HEALTH_CHECK_INTERVAL 控制）
	healthCheckSvc.StartBackground(context.Background())

	// 管理接口组（JWT 认证）
	adminGroup := r.Group("/api/v1")
	adminGroup.Use(middleware.JWTAuth())
	{
		// 渠道定义（Registry）
		adminGroup.GET("/provider-definitions", defHandler.ListDefinitions)
		adminGroup.GET("/provider-definitions/:id", defHandler.GetDefinition)

		// 渠道管理
		adminGroup.POST("/providers/check-all", healthCheckHandler.CheckAll)
		adminGroup.POST("/providers", providerHandler.CreateProvider)
		adminGroup.GET("/providers", providerHandler.ListProviders)
		adminGroup.GET("/providers/:id", providerHandler.GetProvider)
		adminGroup.PUT("/providers/:id", providerHandler.UpdateProvider)
		adminGroup.PATCH("/providers/:id/status", providerHandler.ToggleProviderStatus)
		adminGroup.DELETE("/providers/:id", providerHandler.DeleteProvider)
		adminGroup.POST("/providers/:id/test", testHandler.TestProvider)
		adminGroup.POST("/providers/:id/check", healthCheckHandler.CheckOne)
		adminGroup.POST("/providers/:id/sync-models", syncHandler.SyncModels)
		adminGroup.POST("/providers/:id/sync-models/confirm", syncHandler.ConfirmSync)

		// 模型管理
		adminGroup.POST("/models", modelHandler.CreateModel)
		adminGroup.GET("/models", modelHandler.ListModels)
		// 注意：静态路径必须在 :id 通配前注册，否则会被吞掉
		adminGroup.GET("/models/check-name", modelHandler.CheckModelName)
		adminGroup.POST("/models/check-all", modelCheckHandler.CheckAll)
		adminGroup.DELETE("/models/batch", modelHandler.BatchDeleteModels)
		adminGroup.PATCH("/models/batch-status", modelHandler.BatchUpdateModelStatus)
		adminGroup.PUT("/models/batch-tags", modelTagHandler.BatchSetModelTags)
		adminGroup.POST("/models/batch-tags/query", modelTagHandler.BatchGetModelTags)
		// CAT-1：catalog 一键创建（静态路径，必须在 :id 之前）
		adminGroup.POST("/models/from-catalog", modelHandler.CreateModelFromCatalog)
		// CAT-2：批量绑定（静态路径，必须在 :id 之前）
		adminGroup.POST("/models/batch-bind-catalog", modelHandler.BatchBindCatalog)
		adminGroup.PUT("/models/:id", modelHandler.UpdateModel)
		adminGroup.DELETE("/models/:id", modelHandler.DeleteModel)
		adminGroup.POST("/models/:id/check", modelCheckHandler.CheckOne)
		adminGroup.POST("/models/:id/reset-override", modelHandler.ResetModelOverride)
		// CAT-2：单条绑定/换绑/解绑
		adminGroup.POST("/models/:id/bind-catalog", modelHandler.BindCatalog)
		adminGroup.POST("/models/:id/unbind-catalog", modelHandler.UnbindCatalog)

		// 模型标签
		adminGroup.GET("/model-tags", modelTagHandler.ListTags)
		adminGroup.POST("/model-tags", modelTagHandler.CreateTag)
		adminGroup.PUT("/model-tags/:id", modelTagHandler.UpdateTag)
		adminGroup.DELETE("/model-tags/:id", modelTagHandler.DeleteTag)
		adminGroup.GET("/models/:id/tags", modelTagHandler.GetModelTags)
		adminGroup.PUT("/models/:id/tags", modelTagHandler.SetModelTags)

		// 健康检测管理
		adminGroup.GET("/health-check/config", healthCheckHandler.GetConfig)
		adminGroup.PUT("/health-check/config", healthCheckHandler.UpdateConfig)
		adminGroup.GET("/health-check/logs", healthCheckHandler.GetLogs)
		adminGroup.GET("/health-check/status", healthCheckHandler.GetStatus)
		adminGroup.POST("/health-check/run", healthCheckHandler.RunNow)

		// 模型 Catalog（注意：source_model_id 含 '/'，通过 query 入口而非路径参数访问）
		adminGroup.GET("/model-catalog/search", catalogHandler.Search)
		adminGroup.GET("/model-catalog/by-source", catalogHandler.GetBySource)
		adminGroup.POST("/model-catalog/sync", catalogHandler.Sync)
		adminGroup.GET("/model-catalog/diff", catalogHandler.Diff)
		adminGroup.POST("/model-catalog/apply-diff", catalogHandler.ApplyDiff)
		adminGroup.POST("/model-catalog/auto-bind", catalogHandler.AutoBind)
		adminGroup.GET("/model-catalog/binding-stats", catalogHandler.BindingStats)
		// :id（UUID）放最后，避免吃掉前面的静态路径
		adminGroup.GET("/model-catalog/:id", catalogHandler.GetByID)
	}

	// 内部接口组（内部 Token 认证）
	internalGroup := r.Group("/internal/v1")
	internalGroup.Use(middleware.InternalAuth())
	{
		internalGroup.GET("/models/available", internalHandler.ListAvailableModels)
	}

	return r
}
