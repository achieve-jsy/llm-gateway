package server

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// Setup 初始化 Gin 路由与中间件
// routerGetter 在每次请求时被调用以获取最新的 Router（支持热更新）。
// state tracks load status.
func Setup(
	routerGetter func() *providers.Router,
	routerCache *infra.Cache,
	state *provider.RuntimeState,
	authClient *infra.AuthClient,
	controlPlaneURL, internalToken string,
	sch *scheduler.Scheduler,
	nameCache *cache.GroupNameCache,
	collector ...*gwobs.Collector, // 可选：埋点 Collector
) *gin.Engine {
	// 获取初始 router 用于 models/health/passthrough 等（不需要热更新的场景）
	router := routerGetter()

	r := gin.New()

	// 全局中间件链
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.Tracing())
	r.Use(middleware.Logger())
	r.Use(middleware.ErrorHandler())
	// DEBUG 级请求体日志（生产 INFO 级别下自动跳过，无性能开销）
	r.Use(middleware.RequestBodyLogger())

	// Swagger 文档（无需认证）
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 创建编排器和处理器
	orch := orchestrator.NewChatOrchestratorWithGetter(routerGetter)

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

	embeddingsHandler := handler.NewEmbeddingsHandler(orch)

	// Convert *providers.Router to interface carefully to avoid the nil-interface trap.
	// A nil *providers.Router assigned to a non-nil interface value would cause panics
	// inside methods that check `router != nil` on the interface.
	var modelsRouter handler.ModelsRouterInterface
	if router != nil {
		modelsRouter = router
	}
	modelsHandler := handler.NewModelsHandler(routerCache, modelsRouter, state)
	boundModelsHandler := handler.NewBoundModelsHandler(controlPlaneURL, internalToken)
	modelInfoHandler := handler.NewModelInfoHandler(controlPlaneURL, internalToken)
	healthHandler := handler.NewHealthHandler(routerCache.Client(), modelsRouter, state, sch)

	r.GET("/health", healthHandler.Health)
	r.GET("/ready", healthHandler.Ready)

	// 加载并发管控配置
	guard := loadGatewayGuard(controlPlaneURL)

	// OpenAI 兼容接口（需 API Key 认证）
	v1 := r.Group("/v1")
	v1.Use(middleware.AuthMiddleware(authClient))
	v1.Use(guard.Handler())
	// 埋点采集中间件（可选，有 Collector 时生效）
	if len(collector) > 0 && collector[0] != nil {
		v1.Use(middleware.UsageCollector(collector[0]))
	}
	{
		// Models
		v1.GET("/models", boundModelsHandler.Handle)
		v1.GET("/model/info", modelInfoHandler.Handle)
		v1.GET("/health/models", modelsHandler.HandleLegacy)

		// Chat completions
		v1.POST("/chat/completions", chatHandler.Handle)

		// Embeddings (new)
		v1.POST("/embeddings", embeddingsHandler.Handle)

		// Provider passthrough: /v1/p/:provider/*path forwards opaquely to the upstream.
		v1.Any("/p/:provider/*path", makePassthroughHandler(router))
	}

	return r
}

// makePassthroughHandler returns a Gin handler that proxies requests to
// GET /v1/p/:provider/* directly to the upstream provider.
func makePassthroughHandler(router *providers.Router) gin.HandlerFunc {
	return func(c *gin.Context) {
		if router == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{"message": "router not ready", "code": "service_unavailable"},
			})
			return
		}

		providerType := c.Param("provider")
		path := c.Param("path")
		if path == "" {
			path = "/"
		}

		passthroughReq := &core.PassthroughRequest{
			Method:   c.Request.Method,
			Endpoint: path,
			Body:     c.Request.Body,
			Headers:  c.Request.Header.Clone(),
		}

		resp, err := router.Passthrough(c.Request.Context(), providerType, passthroughReq)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": gin.H{"message": err.Error(), "code": "passthrough_error"},
			})
			return
		}
		if resp == nil {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": gin.H{"message": "empty passthrough response", "code": "passthrough_error"},
			})
			return
		}

		// Forward response headers and status code.
		for key, values := range resp.Headers {
			for _, v := range values {
				c.Header(key, v)
			}
		}
		c.Status(resp.StatusCode)
		if resp.Body != nil {
			defer resp.Body.Close()
			_, _ = io.Copy(c.Writer, resp.Body)
		}
	}
}

func loadGatewayGuard(controlPlaneURL string) *middleware.GatewayGuard {
	settings, err := infra.LoadGatewaySettings(controlPlaneURL)
	if err != nil {
		slog.Warn("[guard] failed to load gateway settings from control-plane, using defaults",
			"error", err,
			"default_rate_limit", 60,
			"default_max_concurrency", 10,
		)
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

	slog.Info("[guard] gateway guard initialized",
		"rate_limit_per_min", settings.RateLimitPerMinute,
		"max_concurrency", settings.MaxConcurrencyPerKey,
	)
	return guard
}
