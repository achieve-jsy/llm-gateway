package router

import (
	"net/http"

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
	syncSvc := service.NewSyncService(providerRepo, modelRepo, publisher)
	syncHandler := handler.NewSyncHandler(syncSvc)

	// 启动健康检测调度器
	config, err := healthCheckRepo.GetConfig()
	if err == nil && config.Enabled {
		healthCheckSvc.Start()
	}

	// 管理接口组（JWT 认证）
	adminGroup := r.Group("/api/v1")
	adminGroup.Use(middleware.JWTAuth())
	{
		// 渠道定义（Registry）
		adminGroup.GET("/provider-definitions", defHandler.ListDefinitions)
		adminGroup.GET("/provider-definitions/:id", defHandler.GetDefinition)

		// 渠道管理
		adminGroup.POST("/providers", providerHandler.CreateProvider)
		adminGroup.GET("/providers", providerHandler.ListProviders)
		adminGroup.GET("/providers/:id", providerHandler.GetProvider)
		adminGroup.PUT("/providers/:id", providerHandler.UpdateProvider)
		adminGroup.PATCH("/providers/:id/status", providerHandler.ToggleProviderStatus)
		adminGroup.DELETE("/providers/:id", providerHandler.DeleteProvider)
		adminGroup.POST("/providers/:id/test", testHandler.TestProvider)
		adminGroup.POST("/providers/:id/sync-models", syncHandler.SyncModels)
		adminGroup.POST("/providers/:id/sync-models/confirm", syncHandler.ConfirmSync)

		// 模型管理
		adminGroup.POST("/models", modelHandler.CreateModel)
		adminGroup.GET("/models", modelHandler.ListModels)
		// 注意：静态路径必须在 :id 通配前注册，否则会被吞掉
		adminGroup.GET("/models/check-name", modelHandler.CheckModelName)
		adminGroup.DELETE("/models/batch", modelHandler.BatchDeleteModels)
		adminGroup.PATCH("/models/batch-status", modelHandler.BatchUpdateModelStatus)
		adminGroup.PUT("/models/batch-tags", modelTagHandler.BatchSetModelTags)
		adminGroup.POST("/models/batch-tags/query", modelTagHandler.BatchGetModelTags)
		adminGroup.PUT("/models/:id", modelHandler.UpdateModel)
		adminGroup.DELETE("/models/:id", modelHandler.DeleteModel)

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
	}

	// 内部接口组（内部 Token 认证）
	internalGroup := r.Group("/internal/v1")
	internalGroup.Use(middleware.InternalAuth())
	{
		internalGroup.GET("/models/available", internalHandler.ListAvailableModels)
	}

	return r
}
