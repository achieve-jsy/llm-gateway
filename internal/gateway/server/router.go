package server

import (
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// Setup 初始化 Gin 路由与中间件
func Setup(registry *provider.Registry, cache *infra.Cache, state *provider.RuntimeState, authClient *infra.AuthClient, controlPlaneURL, internalToken string) *gin.Engine {
	r := gin.New()

	// 全局中间件链
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.Tracing())
	r.Use(middleware.Logger())
	r.Use(middleware.ErrorHandler())

	// Swagger 文档（无需认证）
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 创建编排器和处理器
	orch := orchestrator.NewChatOrchestrator(registry)
	chatHandler := handler.NewChatHandler(orch)
	modelsHandler := handler.NewModelsHandler(cache, registry, state)
	healthHandler := handler.NewHealthHandler(cache.Client(), registry, state)
	r.GET("/health", healthHandler.Health)
	r.GET("/ready", healthHandler.Ready)

	// OpenAI 兼容接口（需 API Key 认证）
	v1 := r.Group("/v1")
	meHandler := handler.NewMeHandler(registry, state, controlPlaneURL, internalToken)

	v1.Use(middleware.AuthMiddleware(authClient))
	{
		v1.GET("/me/models", meHandler.Handle)
		v1.POST("/chat/completions", chatHandler.Handle)
		v1.GET("/models", modelsHandler.Handle)
	}

	return r
}
