package server

import (
	"log"
	"time"

	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// Setup 初始化 Gin 路由与中间件
func Setup(registry *provider.Registry, cache *infra.Cache, state *provider.RuntimeState, authClient *infra.AuthClient, controlPlaneURL, internalToken string, sch *scheduler.Scheduler, nameCache *cache.GroupNameCache) *gin.Engine {
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

	bindClient := binding.NewClient(controlPlaneURL, internalToken)

	var groupDispatch *orchestrator.GroupAwareDispatcher
	if sch != nil && nameCache != nil {
		groupDispatch = orchestrator.NewGroupAwareDispatcherWithBinding(sch, nameCache, bindClient)
	}

	var chatHandler *handler.ChatHandler
	if groupDispatch != nil {
		chatHandler = handler.NewChatHandlerWithGroup(orch, groupDispatch)
	} else {
		chatHandler = handler.NewChatHandler(orch)
	}

	modelsHandler := handler.NewModelsHandler(cache, registry, state)
	boundModelsHandler := handler.NewBoundModelsHandler(controlPlaneURL, internalToken)
	healthHandler := handler.NewHealthHandler(cache.Client(), registry, state, sch)
	r.GET("/health", healthHandler.Health)
	r.GET("/ready", healthHandler.Ready)

	// 加载并发管控配置
	guard := loadGatewayGuard(controlPlaneURL)

	// OpenAI 兼容接口（需 API Key 认证）
	v1 := r.Group("/v1")

	v1.Use(middleware.AuthMiddleware(authClient))
	v1.Use(guard.Handler())
	{
		v1.GET("/models", boundModelsHandler.Handle)
		v1.POST("/chat/completions", chatHandler.Handle)
		v1.GET("/health/models", modelsHandler.Handle)
	}

	return r
}

func loadGatewayGuard(controlPlaneURL string) *middleware.GatewayGuard {
	settings, err := infra.LoadGatewaySettings(controlPlaneURL)
	if err != nil {
		log.Printf("[guard] failed to load gateway settings from control-plane: %v, using defaults", err)
		settings = &infra.GatewaySettings{RateLimitPerMinute: 60, MaxConcurrencyPerKey: 10}
	}

	guard := middleware.NewGatewayGuard(settings.RateLimitPerMinute, settings.MaxConcurrencyPerKey)
	guard.StartCleanup(2 * time.Minute)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			updated, err := infra.LoadGatewaySettings(controlPlaneURL)
			if err != nil {
				continue
			}
			guard.UpdateLimits(updated.RateLimitPerMinute, updated.MaxConcurrencyPerKey)
		}
	}()

	log.Printf("[guard] gateway guard initialized: rate_limit=%d/min max_concurrency=%d",
		settings.RateLimitPerMinute, settings.MaxConcurrencyPerKey)
	return guard
}