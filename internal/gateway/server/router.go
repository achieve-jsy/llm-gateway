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
	"github.com/example/llm-gateway/internal/gateway/quota"
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

	var embeddingsHandler *handler.EmbeddingsHandler
	if groupDispatch != nil {
		embeddingsHandler = handler.NewEmbeddingsHandlerWithGroup(orch, groupDispatch)
	} else {
		embeddingsHandler = handler.NewEmbeddingsHandler(orch)
	}

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
	modelDetailHandler := handler.NewModelDetailHandler(controlPlaneURL, internalToken)
	modelInfoHandler := handler.NewModelInfoHandler(controlPlaneURL, internalToken)
	healthHandler := handler.NewHealthHandler(routerCache.Client(), modelsRouter, state, sch)
	var responsesHandler *handler.ResponsesHandler
	if groupDispatch != nil {
		responsesHandler = handler.NewResponsesHandlerWithGroup(orch, groupDispatch)
	} else {
		responsesHandler = handler.NewResponsesHandler(orch)
	}

	// v0.7: 预算预扣依赖（一次构造传给所有 handler）。
	// Pricing 暂用 fallback（保守单价），后续接入 providers.ModelRegistry 真实 catalog 定价。
	// FallbackOutputTokens：请求未传 max_tokens 时的保守估计（对齐控制面配置 2048）。
	// MaxSingleRequestUSD：单请求成本上限（对齐控制面配置 10.0 美元，防单次击穿软上限）。
	// v0.9: 事后记账模式，BudgetDeps 只需要 Redis client
	budgetDeps := &middleware.BudgetDeps{
		RDB: rdb,
	}
	chatHandler.SetBudgetDeps(budgetDeps)
	embeddingsHandler.SetBudgetDeps(budgetDeps)
	messagesHandler.SetBudgetDeps(budgetDeps)
	responsesHandler.SetBudgetDeps(budgetDeps)

	r.GET("/health", healthHandler.Health)
	r.GET("/ready", healthHandler.Ready)

	// 加载并发管控配置
	// v0.15: 四层配额体系 — 初始化 QuotaConfigLoader + QuotaMiddleware
	// 替换旧的 GatewayGuard + LayeredRateLimiter
	quotaHolder := quota.NewSnapshotHolder()
	quotaStore := quota.NewRedisStore(rdb)
	quotaLoader := quota.NewConfigLoader(rdb, quotaHolder)
	if rdb != nil {
		quotaLoader.StartPolling(30 * time.Second)
		quotaLoader.SubscribeToChanges(context.Background())
	}
	quotaMW := quota.NewMiddleware(quotaHolder, quotaStore)

	// v0.15: 模型层配额检查（路由选定具体 model 之后、转发上游之前）
	modelQuotaGate := quota.NewModelQuotaGate(quotaHolder, quotaStore)
	chatHandler.SetModelQuotaGate(modelQuotaGate)
	embeddingsHandler.SetModelQuotaGate(modelQuotaGate)
	messagesHandler.SetModelQuotaGate(modelQuotaGate)
	responsesHandler.SetModelQuotaGate(modelQuotaGate)

	// OpenAI 兼容接口（需 API Key 认证）
	v1 := r.Group("/v1")
	v1.Use(middleware.AuthMiddleware(authClient))
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
		// Provider 并发/限流追踪（写入 Redis 供运维看板读取）
		// 必须在 UsageCollector 之后，确保 provider 信息已由 orchestrator 写入 context
		v1.Use(middleware.ProviderTracker(rdb))
	// 组织归集状态检查：pending_studio 员工调用模型时返回 403
	// 注意：必须在 UsageCollector 之后，403 才能被记录到调用明细
	v1.Use(middleware.OrgCheckMiddleware())
	// v0.7: 预算预扣已改由每个 handler 在解析 body 后主动调用 middleware.Reserve
	// （不再用 BudgetCheckMiddleware，因为 middleware 拿不到 body 里的 model/max_tokens）
	{
		// Models
		v1.GET("/models", boundModelsHandler.Handle)
		v1.GET("/models/:model_id", modelDetailHandler.Handle)
		v1.GET("/model/info", modelInfoHandler.Handle)
		v1.GET("/health/models", modelsHandler.HandleLegacy)

		// Chat completions
		v1.POST("/chat/completions", quotaMW.Handler(), chatHandler.Handle)

		// Embeddings (new)
		v1.POST("/embeddings", quotaMW.Handler(), embeddingsHandler.Handle)

		// Responses API — OpenAI Agent SDK 核心依赖
		v1.POST("/responses", quotaMW.Handler(), responsesHandler.Handle)

		// Anthropic Messages API
		v1.POST("/messages", quotaMW.Handler(), messagesHandler.HandleMessages)
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

// hook 在 usage_collector 埋点后调用，异步执行 Redis 累加和控制面告警触发。
//
// v0.9 事后记账模式：
//   - 请求失败（status >= 400）→ 不累加（没有预扣，无需冲销）
//   - 请求成功（status < 400） → BudgetAdd（直接累加真实成本）
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

		// 1. Redis 累加真实成本（同步，耗时 < 1ms）
		// 请求失败不累加（没有预扣，无需冲销）
		if httpStatus < 400 && realCostUSD > 0 {
			argsVal, _ := c.Get(middleware.CtxKeyBudgetScopeArgs)
			scopeArgs, _ := argsVal.([]interface{})
			middleware.BudgetAdd(c.Request.Context(), rdb, budgetKeys, scopeArgs, realCostUSD)
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

