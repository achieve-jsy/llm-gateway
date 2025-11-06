package server

import (
	"net/http"

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
func Setup(registry *provider.Registry, cache *infra.Cache) *gin.Engine {
	r := gin.New()

	// 全局中间件链
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.Tracing())
	r.Use(middleware.Logger())
	r.Use(middleware.ErrorHandler())

	// 健康检查（无需认证）
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/ready", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	// Swagger 文档（无需认证）
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 创建编排器和处理器
	orch := orchestrator.NewChatOrchestrator(registry)
	chatHandler := handler.NewChatHandler(orch)
	modelsHandler := handler.NewModelsHandler(cache, registry)

	// OpenAI 兼容接口（需 API Key 认证）
	v1 := r.Group("/v1")
	v1.Use(middleware.APIKeyAuth())
	{
		v1.POST("/chat/completions", chatHandler.Handle)
		v1.GET("/models", modelsHandler.Handle)
	}

	return r
}
