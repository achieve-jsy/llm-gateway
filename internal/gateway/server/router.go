package server

import (
	"bytes"
	"context"
	"encoding/json"
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
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// redisClient 接口让 Setup 对 *redis.Client 的依赖可 nil-safe。
type redisClient = *redis.Client

// Setup 初始化 Gin 路由与中间件
// routerGetter 在每次请求时被调用以获取最新的 Router（支持热更新）。
// state tracks load status.
// bindClient 为可选参数（nil 时不启用模型组权限校验）。
func Setup(
	routerGetter func() *providers.Router,
	routerCache *infra.Cache,
	state *provider.RuntimeState,
	authClient *infra.AuthClient,
	controlPlaneURL, internalToken string,
	sch *scheduler.Scheduler,
	nameCache *cache.GroupNameCache,
	collectorOpt *gwobs.Collector,
	traceWriterOpt *gwobs.TraceWriter,
	bindClient *binding.Client,
	rdb redisClient, // v0.7: Redis client 用于预算计数中间件（nil = 禁用）
) *gin.Engine {
	var collector []*gwobs.Collector
	if collectorOpt != nil {
		collector = []*gwobs.Collector{collectorOpt}
	}
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

	var groupDispatch *orchestrator.GroupAwareDispatcher
	if sch != nil && nameCache != nil && bindClient != nil {
		groupDispatch = orchestrator.NewGroupAwareDispatcherWithBinding(sch, nameCache, bindClient)
	}

	var chatHandler *handler.ChatHandler
	if groupDispatch != nil {
		chatHandler = handler.NewChatHandlerWithGroup(orch, groupDispatch)
	} else {
		chatHandler = handler.NewChatHandler(orch)
	}

	embeddingsHandler := handler.NewEmbeddingsHandler(orch)

	var messagesHandler *handler.MessagesHandler
	if groupDispatch != nil {
		messagesHandler = handler.NewMessagesHandlerWithGroup(orch, groupDispatch)
	} else {
		messagesHandler = handler.NewMessagesHandler(orch)
	}

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
	var responsesHandler *handler.ResponsesHandler
	if groupDispatch != nil {
		responsesHandler = handler.NewResponsesHandlerWithGroup(orch, groupDispatch)
	} else {
		responsesHandler = handler.NewResponsesHandler(orch)
	}

	r.GET("/health", healthHandler.Health)
	r.GET("/ready", healthHandler.Ready)

	// 加载并发管控配置
	guard := loadGatewayGuard(controlPlaneURL)

	// OpenAI 兼容接口（需 API Key 认证）
	v1 := r.Group("/v1")
	v1.Use(middleware.AuthMiddleware(authClient))
	v1.Use(guard.Handler())
	// v0.7: 分层限速（③关 dept/studio 层，Redis 滑动窗口）
	// RateLimitConfig 目前从 settings 下发全局 RPM，dept/studio 层先以 0（不限）兜底，
	// 后续通过 verify-key 快照携带各层限速配置。
	v1.Use(middleware.LayeredRateLimiter(rdb, func(c *gin.Context) middleware.RateLimitConfig {
		return middleware.RateLimitConfig{} // TODO: 从 settings 读取各层 RPM
	}))
	// 请求/响应正文捕获（供 trace 记录使用）
	v1.Use(middleware.TraceCapture())
	// 埋点采集中间件放在 OrgCheck 之前，确保 403 拦截的调用也能写入 call_logs
	if len(collector) > 0 && collector[0] != nil {
		// v0.7: 构造预算核销+告警触发 hook（仅在 Redis 可用时启用）
		var budgetHook middleware.PostCostHook
		if rdb != nil {
			budgetHook = buildBudgetPostHook(rdb, controlPlaneURL, internalToken)
		}
		v1.Use(middleware.UsageCollector(collector[0], traceWriterOpt, budgetHook))
	}
	// 组织归集状态检查：pending_studio 员工调用模型时返回 403
	// 注意：必须在 UsageCollector 之后，403 才能被记录到调用明细
	v1.Use(middleware.OrgCheckMiddleware())
	// v0.7: 预算并集校验（第④关，post-body；est_cost 由各 handler preflight 写入 ctx）
	v1.Use(middleware.BudgetCheckMiddleware(rdb))
	{
		// Models
		v1.GET("/models", boundModelsHandler.Handle)
		v1.GET("/model/info", modelInfoHandler.Handle)
		v1.GET("/health/models", modelsHandler.HandleLegacy)

		// Chat completions
		v1.POST("/chat/completions", chatHandler.Handle)

		// Embeddings (new)
		v1.POST("/embeddings", embeddingsHandler.Handle)

		// Responses API — OpenAI Agent SDK 核心依赖
		v1.POST("/responses", responsesHandler.Handle)

		// Anthropic Messages API
		v1.POST("/messages", messagesHandler.HandleMessages)
		v1.POST("/messages/count_tokens", messagesHandler.HandleCountTokens)

		// Provider passthrough: /v1/p/:provider/*path
		// v0.7: 默认关闭，仅白名单 Key（access_all_groups=true 的管理员 Key）可用；
		// 普通 Key 一律 403，白名单 Key 透传但不计入预算（豁免），记日志。
		v1.Any("/p/:provider/*path", middleware.PassthroughGuard(), makePassthroughHandler(router))
	}

	// Claude Code telemetry stub — returns 200 to prevent client-side 404 errors.
	r.POST("/api/event_logging/batch", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

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

// buildBudgetPostHook 构造预算核销 + 告警触发 hook。
// hook 在 usage_collector 埋点后调用，异步执行 Redis 核销和控制面告警触发。
//
// 核销逻辑（同步）：
//   - 请求失败（status >= 400）→ BudgetCancelReserve（冲销全部预扣）
//   - 请求成功（status < 400） → BudgetSettle（delta = realCost - estCost）
//
// 告警触发（异步 fire-and-forget）：
//   - 对 BudgetChain 中的每一层调用控制面 /internal/v1/budget/evaluate-alert
//   - 控制面告警服务做阈值判断（80%/100%）+ 去重 + webhook 通知
func buildBudgetPostHook(rdb *redis.Client, controlPlaneURL, internalToken string) middleware.PostCostHook {
	return func(c *gin.Context, realCostUSD float64, httpStatus int) {
		keysVal, ok := c.Get("budget_keys")
		if !ok {
			return
		}
		budgetKeys, ok := keysVal.([]string)
		if !ok || len(budgetKeys) == 0 {
			return
		}
		estVal, _ := c.Get("budget_est_cost")
		estCost, _ := estVal.(float64)

		// 1. Redis 核销（同步，耗时 < 1ms）
		if httpStatus >= 400 {
			// 请求失败 → 冲销全部预扣
			middleware.BudgetCancelReserve(c.Request.Context(), rdb, budgetKeys, estCost)
		} else {
			// 请求成功 → settle(real - est)
			middleware.BudgetSettle(c.Request.Context(), rdb, budgetKeys, estCost, realCostUSD)
		}

		// 2. 告警触发（异步 fire-and-forget，不影响响应）
		identityVal, _ := c.Get("identity")
		id, ok := identityVal.(*gwmodel.UserIdentity)
		if !ok || len(id.BudgetChain) == 0 {
			return
		}

		// 拷贝 chain 数据避免 gin.Context 生命周期问题
		chain := make([]struct {
			ScopeType string `json:"scope_type"`
			ScopeID   string `json:"scope_id"`
		}, 0, len(id.BudgetChain))
		for _, s := range id.BudgetChain {
			chain = append(chain, struct {
				ScopeType string `json:"scope_type"`
				ScopeID   string `json:"scope_id"`
			}{ScopeType: s.ScopeType, ScopeID: s.ScopeID})
		}

		go triggerBudgetAlert(chain, controlPlaneURL, internalToken)
	}
}

// triggerBudgetAlert 异步调用控制面 /internal/v1/budget/evaluate-alert。
// 对 BudgetChain 的每一层各发一次请求（控制面内部做去重）。
// 任一失败仅 log，不重试。
func triggerBudgetAlert(chain []struct {
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
}, controlPlaneURL, internalToken string) {
	if controlPlaneURL == "" {
		return
	}
	client := &http.Client{Timeout: 3 * time.Second}
	for _, s := range chain {
		if s.ScopeType == "" || s.ScopeID == "" {
			continue
		}
		body, _ := json.Marshal(map[string]string{
			"scope_type": s.ScopeType,
			"scope_id":   s.ScopeID,
		})
		url := controlPlaneURL + "/internal/v1/budget/evaluate-alert"
		req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", internalToken)
		resp, err := client.Do(req)
		if err != nil {
			slog.Debug("budget alert: control-plane unreachable", "error", err)
			continue
		}
		resp.Body.Close()
	}
}
