package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/core"
	acrypto "github.com/example/llm-gateway/internal/auth/crypto"
	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/budget"
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
// onSeedanceReloadReady 若非 nil，会收到「按 snapshot entries 热替换视频/素材适配器」的回调，
// 供 main 挂到 config:change，避免改渠道后必须重启 gateway。
func Setup(
	routerGetter func() *providers.Router,
	routerCache *infra.Cache,
	state *provider.RuntimeState,
	authClient *infra.AuthClient,
	jwtManager *acrypto.JWTManager,
	controlPlaneURL, internalToken string,
	sch *scheduler.Scheduler,
	nameCache *cache.GroupNameCache,
	collectorOpt *gwobs.Collector,
	traceWriterOpt *gwobs.TraceWriter,
	bindClient *binding.Client,
	rdb redisClient,
	tierHolder *budget.SnapshotHolder,
	onSeedanceReloadReady ...func(reload func(entries []gwmodel.ProviderEntry)),
) *gin.Engine {
	return setup(nil, routerGetter, routerCache, state, authClient, jwtManager, controlPlaneURL, internalToken,
		sch, nameCache, collectorOpt, traceWriterOpt, bindClient, rdb, tierHolder, onSeedanceReloadReady...)
}

func SetupWithBudgetAlertDispatcher(
	alertDispatcher *BudgetAlertDispatcher,
	routerGetter func() *providers.Router,
	routerCache *infra.Cache,
	state *provider.RuntimeState,
	authClient *infra.AuthClient,
	jwtManager *acrypto.JWTManager,
	controlPlaneURL, internalToken string,
	sch *scheduler.Scheduler,
	nameCache *cache.GroupNameCache,
	collectorOpt *gwobs.Collector,
	traceWriterOpt *gwobs.TraceWriter,
	bindClient *binding.Client,
	rdb redisClient,
	tierHolder *budget.SnapshotHolder,
	onSeedanceReloadReady ...func(reload func(entries []gwmodel.ProviderEntry)),
) *gin.Engine {
	return setup(alertDispatcher, routerGetter, routerCache, state, authClient, jwtManager, controlPlaneURL, internalToken,
		sch, nameCache, collectorOpt, traceWriterOpt, bindClient, rdb, tierHolder, onSeedanceReloadReady...)
}

func setup(
	alertDispatcher *BudgetAlertDispatcher,
	routerGetter func() *providers.Router,
	routerCache *infra.Cache,
	state *provider.RuntimeState,
	authClient *infra.AuthClient,
	jwtManager *acrypto.JWTManager,
	controlPlaneURL, internalToken string,
	sch *scheduler.Scheduler,
	nameCache *cache.GroupNameCache,
	collectorOpt *gwobs.Collector,
	traceWriterOpt *gwobs.TraceWriter,
	bindClient *binding.Client,
	rdb redisClient,
	tierHolder *budget.SnapshotHolder,
	onSeedanceReloadReady ...func(reload func(entries []gwmodel.ProviderEntry)),
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
		groupDispatch = orchestrator.NewGroupAwareDispatcherWithBindingAndTier(
			sch, nameCache, bindClient, budget.NewTierResolver(tierHolder),
		)
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

	var rerankHandler *handler.RerankHandler
	if groupDispatch != nil {
		rerankHandler = handler.NewRerankHandlerWithGroup(orch, groupDispatch)
	} else {
		rerankHandler = handler.NewRerankHandler(orch)
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
	healthHandler.SetBudgetSnapshotHolder(tierHolder)
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
	rerankHandler.SetBudgetDeps(budgetDeps)
	messagesHandler.SetBudgetDeps(budgetDeps)
	responsesHandler.SetBudgetDeps(budgetDeps)
	var tierBudgetGate *budget.Gate
	if tierHolder != nil && rdb != nil {
		admissionConfig := budget.AdmissionConfig{}
		if collectorOpt != nil {
			admissionConfig = collectorOpt.BudgetAdmissionConfig()
		}
		tierBudgetGate = budget.NewGate(
			tierHolder,
			budget.NewRedisStoreWithAdmission(rdb, admissionConfig),
			budget.LegacyTotalFuncs{
				CheckFunc: func(ctx context.Context, plan *budget.AccountingPlan) (*budget.Constraint, error) {
					return middleware.CheckLegacyBudget(ctx, rdb, plan)
				},
				AddFunc: func(ctx context.Context, keys []string, scopeArgs []any, costUSD float64) error {
					return middleware.BudgetAdd(ctx, rdb, keys, scopeArgs, costUSD)
				},
			},
			time.Now,
		)
		chatHandler.SetBudgetGate(tierBudgetGate)
		embeddingsHandler.SetBudgetGate(tierBudgetGate)
		rerankHandler.SetBudgetGate(tierBudgetGate)
		messagesHandler.SetBudgetGate(tierBudgetGate)
		responsesHandler.SetBudgetGate(tierBudgetGate)
	}

	r.GET("/health", healthHandler.Health)
	r.GET("/ready", func(c *gin.Context) {
		if collectorOpt != nil && !collectorOpt.CanAcceptBillableRequest() {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "not_ready", "reason": "call_log_spool_high_watermark",
			})
			return
		}
		healthHandler.Ready(c)
	})

	// 加载并发管控配置
	// v0.15: 四层配额体系 — 初始化 QuotaConfigLoader + QuotaMiddleware
	// 替换旧的 GatewayGuard + LayeredRateLimiter
	quotaHolder := quota.NewSnapshotHolder()
	quotaStore := quota.NewRedisStore(rdb)
	quotaLoader := quota.NewConfigLoader(rdb, quotaHolder)
	if rdb != nil {
		quotaLoader.StartPolling(context.Background(), 30*time.Second)
		quotaLoader.SubscribeToChanges(context.Background())
	}
	quotaMW := quota.NewMiddleware(quotaHolder, quotaStore)
	chatHandler.SetRequestQuotaGate(quotaMW)
	embeddingsHandler.SetRequestQuotaGate(quotaMW)
	rerankHandler.SetRequestQuotaGate(quotaMW)
	messagesHandler.SetRequestQuotaGate(quotaMW)
	responsesHandler.SetRequestQuotaGate(quotaMW)

	// v0.15: 模型层配额检查（路由选定具体 model 之后、转发上游之前）
	modelQuotaGate := quota.NewModelQuotaGate(quotaHolder, quotaStore)
	chatHandler.SetModelQuotaGate(modelQuotaGate)
	embeddingsHandler.SetModelQuotaGate(modelQuotaGate)
	rerankHandler.SetModelQuotaGate(modelQuotaGate)
	messagesHandler.SetModelQuotaGate(modelQuotaGate)
	responsesHandler.SetModelQuotaGate(modelQuotaGate)

	// OpenAI 兼容接口（需 API Key 认证）
	// 初始化视频适配器（从 Redis Provider Snapshot 读取真实凭证；支持热更新）
	var initialEntries []gwmodel.ProviderEntry
	if rdb != nil {
		if snapshot, err := configsync.LoadProviderSnapshot(context.Background(), rdb); err == nil {
			initialEntries = snapshot.Entries
		} else {
			slog.Warn("failed to load provider snapshot for video adapters", "error", err)
		}
	}
	adapters := buildSeedanceAdapters(initialEntries)
	relayAdapter, volcengineAdapter := adapters.asVideoProviders()

	v1 := r.Group("/v1")
	v1.Use(middleware.AuthMiddleware(authClient, jwtManager))
	// 在途字节预算准入：按 Content-Length 排队占用全局 256MB 预算，
	// 为「并发 × 体积」提供硬上界（语义见 middleware.BodyBudgetLimiter）。
	// 位置必须恰好在这里：
	//   - 不能放在 AuthMiddleware 之前：否则未鉴权 / 无效 Key 的请求也会占用预算，
	//     可以靠垃圾请求把预算占满，反而拖垮正常调用；
	//   - 不能放在 TraceCapture 之后：请求体读取发生在 TraceCapture，
	//     预算必须覆盖读取阶段，否则内存峰值在拿到预算前就已经产生，准入形同虚设。
	v1.Use(middleware.BodyBudgetLimiter(middleware.DefaultBodyBudgetBytes))
	v1.Use(BillingFactGuard(collectorOpt))
	// 请求/响应正文捕获（供 trace 记录使用）
	v1.Use(middleware.TraceCapture())
	// 埋点采集中间件放在 OrgCheck 之前，确保 403 拦截的调用也能写入 call_logs
	if (len(collector) > 0 && collector[0] != nil) || tierBudgetGate != nil {
		// v0.7: 构造预算核销+告警触发 hook（仅在 Redis 可用时启用）
		var budgetHook middleware.PostCostHook
		if tierBudgetGate != nil {
			budgetHook = buildBudgetPostHook(tierBudgetGate, alertDispatcher)
		}
		var callLogCollector *gwobs.Collector
		if len(collector) > 0 {
			callLogCollector = collector[0]
		}
		// 必须带 Redis：运维看板实时 QPS/TPS（ops:qps:* / ops:tps:*）由此写入
		v1.Use(middleware.UsageCollectorWithRedis(callLogCollector, traceWriterOpt, budgetHook, rdb))
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
		v1.POST("/chat/completions", chatHandler.Handle)

		// Embeddings (new)
		v1.POST("/embeddings", embeddingsHandler.Handle)

		// Rerank
		v1.POST("/rerank", rerankHandler.Handle)

		// Responses API — OpenAI Agent SDK 核心依赖
		v1.POST("/responses", responsesHandler.Handle)

		// Anthropic Messages API
		v1.POST("/messages", messagesHandler.HandleMessages)
		v1.POST("/messages/count_tokens", messagesHandler.HandleCountTokens)

		// Provider passthrough: /v1/p/:provider/*path
		// v0.7: 默认关闭，仅白名单 Key（access_all_groups=true 的管理员 Key）可用；
		// 普通 Key 一律 403，白名单 Key 透传但不计入预算（豁免），记日志。
		v1.Any("/p/:provider/*path", middleware.PassthroughGuard(), makePassthroughHandler(router))

		// 视频生成：Redis 热缓存 + 控制面 PG 权威存储
		var redisStore *handler.RedisVideoTaskStore
		if rdb != nil {
			hsetFn := func(ctx context.Context, key string, values ...interface{}) error {
				return rdb.HSet(ctx, key, values...).Err()
			}
			hgetFn := func(ctx context.Context, key, field string) (string, error) {
				return rdb.HGet(ctx, key, field).Result()
			}
			expireFn := func(ctx context.Context, key string, expiration time.Duration) error {
				return rdb.Expire(ctx, key, expiration).Err()
			}
			redisStore = handler.NewRedisVideoTaskStore(hsetFn, hgetFn, expireFn, 30*24*time.Hour)
		}

		var videoTaskStore handler.VideoTaskStore
		if controlPlaneURL != "" && internalToken != "" {
			cpClient := infra.NewVideoTaskClient(controlPlaneURL, internalToken)
			cpAdapter := &handler.CPVideoTaskStoreAdapter{Client: cpClient}
			videoTaskStore = handler.NewCompositeVideoTaskStore(redisStore, cpAdapter)
		} else if redisStore != nil {
			videoTaskStore = redisStore
		}

		var modelResolver handler.ModelResolver
		if routerGetter != nil {
			modelResolver = func(model string) string {
				if r := routerGetter(); r != nil {
					return r.GetProviderType(model)
				}
				return ""
			}
		}

		videoHandler := handler.NewVideoHandler(relayAdapter, volcengineAdapter,
			handler.WithTaskStore(videoTaskStore),
			handler.WithModelResolver(modelResolver),
			handler.WithGroupDispatcher(groupDispatch),
		)
		videoHandlerReg := videoHandler.AdapterRegistry()
		videoHandlerReg.Swap(relayAdapter, volcengineAdapter, adapters.relayName, adapters.volcName)

		v1.POST("/contents/generations/tasks", videoHandler.CreateTask)
		v1.GET("/contents/generations/tasks/:task_id", videoHandler.GetTask)

		// 素材库
		assetRelay, assetVolc := adapters.asAssetProviders()
		assetHandler := handler.NewAssetHandler(assetRelay, assetVolc,
			handler.WithAssetProviderType("seedance-relay"),
			handler.WithAssetProviderName(adapters.relayName),
		)
		assetHandlerReg := assetHandler.AdapterRegistry()

		reloadSeedance := func(entries []gwmodel.ProviderEntry) {
			next := buildSeedanceAdapters(entries)
			rv, vv := next.asVideoProviders()
			ra, va := next.asAssetProviders()
			videoHandlerReg.Swap(rv, vv, next.relayName, next.volcName)
			assetHandlerReg.Swap(ra, va)
			assetHandler.SetRouting("seedance-relay", next.relayName)
			slog.Info("seedance adapters hot-reloaded",
				"relay", next.relayName,
				"volcengine", next.volcName,
			)
		}
		if len(onSeedanceReloadReady) > 0 && onSeedanceReloadReady[0] != nil {
			onSeedanceReloadReady[0](reloadSeedance)
		}

		v1.POST("/assets/:action", assetHandler.Handle)

		// 媒体接缝（A7）：依赖在此一次性汇总，两条链路各自的注册实现见 routes_media.go。
		// B/C 队新增依赖时只改 routes_media.go 的 MediaDeps/newMediaDeps，不要改本文件。
		mediaDeps := newMediaDeps(v1, routerGetter, groupDispatch, controlPlaneURL, internalToken,
			rdb, collectorOpt, quotaMW, modelQuotaGate, tierBudgetGate)
		registerMediaImageRoutes(mediaDeps)
		registerMediaTaskRoutes(mediaDeps)
	}

	// Claude Code telemetry stub — returns 200 to prevent client-side 404 errors.
	r.POST("/api/event_logging/batch", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return r
}

type billableReadiness interface {
	CanAcceptBillableRequest() bool
}

func BillingFactGuard(readiness billableReadiness) gin.HandlerFunc {
	return func(c *gin.Context) {
		if readiness == nil || readiness.CanAcceptBillableRequest() || !isBillableRequest(c.Request) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"code":    "billing_fact_store_backpressure",
				"message": "Billing fact storage is temporarily at capacity",
			},
		})
	}
}

func isBillableRequest(request *http.Request) bool {
	if request == nil || request.Method != http.MethodPost {
		return false
	}
	switch request.URL.Path {
	case "/v1/chat/completions", "/v1/embeddings", "/v1/rerank", "/v1/responses", "/v1/messages":
		return true
	default:
		return false
	}
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
func buildBudgetPostHook(gate *budget.Gate, alertDispatcher *BudgetAlertDispatcher) middleware.PostCostHook {
	return func(c *gin.Context, realCostUSD float64, httpStatus int) {
		if httpStatus >= 400 || realCostUSD <= 0 || gate == nil {
			return
		}
		plan, ok := budget.GetAccountingPlan(c)
		if !ok {
			return
		}
		costMicroUSD, err := budget.MicroUSDFromFloat64(realCostUSD)
		if err != nil || costMicroUSD <= 0 {
			return
		}
		if budget.GetBillableReservation(c) != nil {
			if plan.RedisCheckSucceeded && !plan.AdmissionInfrastructureFailOpen {
				budget.SetPendingAccount(c, &budget.PendingAccount{
					Plan: budget.PlanForAccounting(plan), CostMicroUSD: costMicroUSD,
					OnAccounted: func(context.Context) {
						enqueueBudgetAlertRequests(plan, alertDispatcher)
					},
				})
			}
			if legacyErr := gate.AccountLegacy(c.Request.Context(), plan, costMicroUSD); legacyErr != nil {
				slog.Warn("legacy budget account failed", "error", legacyErr)
			}
			return
		} else {
			accountResult := gate.Account(c.Request.Context(), plan, costMicroUSD)
			if accountResult.V2Error != nil {
				slog.Warn("budget v2 account failed", "error", accountResult.V2Error)
				return
			}
			if accountResult.LegacyError != nil {
				slog.Warn("legacy budget account failed", "error", accountResult.LegacyError)
			}
		}
		enqueueBudgetAlertRequests(plan, alertDispatcher)
	}
}

func enqueueBudgetAlertRequests(plan *budget.AccountingPlan, dispatcher *BudgetAlertDispatcher) {
	if dispatcher == nil {
		return
	}
	for _, request := range BuildBudgetAlertRequests(plan) {
		dispatcher.Enqueue(request)
	}
}

func BuildBudgetAlertRequests(plan *budget.AccountingPlan) []BudgetAlertRequest {
	if !budget.ShouldEmitThresholdAlerts(plan) {
		return nil
	}
	requests := make([]BudgetAlertRequest, 0, len(plan.Constraints))
	seen := make(map[string]struct{}, len(plan.Constraints))
	for _, constraint := range plan.Constraints {
		if constraint.Dimension == budget.DimensionTier && !plan.TierAccountingEligible {
			continue
		}
		request := BudgetAlertRequest{
			ScopeType: constraint.ScopeType, ScopeID: constraint.ScopeID,
			PeriodType: string(constraint.PeriodType), DimensionType: string(constraint.Dimension),
			ModelTierID: constraint.TierID,
		}
		key := request.ScopeType + ":" + request.ScopeID + ":" + request.PeriodType + ":" +
			request.DimensionType + ":" + request.ModelTierID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		requests = append(requests, request)
	}
	return requests
}

type BudgetAlertRequest struct {
	ScopeType     string `json:"scope_type"`
	ScopeID       string `json:"scope_id"`
	PeriodType    string `json:"period_type"`
	DimensionType string `json:"dimension_type"`
	ModelTierID   string `json:"model_tier_id,omitempty"`
}
