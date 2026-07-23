package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/httpclient"
	"github.com/joho/godotenv"

	acrypto "github.com/example/llm-gateway/internal/auth/crypto"
	authhandler "github.com/example/llm-gateway/internal/auth/handler"
	authsvc "github.com/example/llm-gateway/internal/auth/service"

	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/config"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/loader"
	gatewaymodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/gateway/server"
	"github.com/example/llm-gateway/internal/gateway/middleware"

	// Register all GoModel provider adapters.
	"github.com/example/llm-gateway/internal/providers"
	providersanthropic "github.com/example/llm-gateway/internal/providers/anthropic"
	providersazure "github.com/example/llm-gateway/internal/providers/azure"
	providersbailian "github.com/example/llm-gateway/internal/providers/bailian"
	providersbedrock "github.com/example/llm-gateway/internal/providers/bedrock"
	providerscohere "github.com/example/llm-gateway/internal/providers/cohere"
	providersdeepseek "github.com/example/llm-gateway/internal/providers/deepseek"
	providersgemini "github.com/example/llm-gateway/internal/providers/gemini"
	providersgroq "github.com/example/llm-gateway/internal/providers/groq"
	providerslitellm "github.com/example/llm-gateway/internal/providers/litellm"
	providersminimax "github.com/example/llm-gateway/internal/providers/minimax"
	providersollama "github.com/example/llm-gateway/internal/providers/ollama"
	providersopenai "github.com/example/llm-gateway/internal/providers/openai"
	providersopenrouter "github.com/example/llm-gateway/internal/providers/openrouter"
	providersoracle "github.com/example/llm-gateway/internal/providers/oracle"
	seedancerelay "github.com/example/llm-gateway/internal/providers/seedance/relay"
	seedancevolcengine "github.com/example/llm-gateway/internal/providers/seedance/volcengine"
	providersvertex "github.com/example/llm-gateway/internal/providers/vertex"
	providersvllm "github.com/example/llm-gateway/internal/providers/vllm"
	providersxai "github.com/example/llm-gateway/internal/providers/xai"
	providerszai "github.com/example/llm-gateway/internal/providers/zai"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	cpinfra "github.com/example/llm-gateway/internal/controlplane/infra"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
)

type providerLoadResult struct {
	source  string
	version int64
}

func main() {
	// 加载 .env 文件（开发环境，文件不存在时不报错）
	_ = godotenv.Load()

	// 配置 slog JSON 格式，支持 LOG_LEVEL 环境变量动态控制日志级别
	// LOG_LEVEL 可选值: debug, info, warn, error（不区分大小写，默认 info）
	logLevel := resolveLogLevel(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))

	// 加载配置
	cfg := config.Load()

	// 按配置覆盖 call_traces 正文裁剪上限（默认不截断文本上下文，供用量/语义分析报告使用）
	middleware.SetTraceSanitizeLimits(middleware.TraceSanitizeLimits{
		MaxStringLenInTrace: cfg.Observability.TraceMaxStringLen,
		MaxBase64LenInTrace: cfg.Observability.TraceMaxBase64Len,
	})

	// 初始化 Redis
	rdb, err := infra.InitRedis(cfg.Redis)
	if err != nil {
		log.Fatalf("failed to connect Redis: %v", err)
	}

	// 初始化缓存
	icache := infra.NewCache(rdb)

	// 初始化 KeyCache：API Key 验证结果双层缓存（本地内存 + Redis）
	keyCache := infra.NewKeyCache(rdb)
	defer keyCache.Close()

	// 初始化 OTLP Tracer
	shutdown, err := infra.InitTracer(cfg.OTLP)
	if err != nil {
		log.Fatalf("failed to init tracer: %v", err)
	}
	defer shutdown()

	// Build the GoModel ProviderFactory and register all provider types.
	factory := buildProviderFactory()

	// 注入 LLMClient 可观测性 Hooks（上游请求开始/结束日志 + OTel span 属性）
	factory.SetHooks(infra.NewLLMClientHooks())

	// RuntimeState tracks load metadata (source, version, model count).
	state := &provider.RuntimeState{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controlPlaneURL := cfg.Provider.ControlPlaneURL
	internalToken := cfg.Provider.InternalToken

	// ProviderLoader bridges control-plane entries → GoModel Router.
	var reloadSeedance func(entries []gatewaymodel.ProviderEntry)
	providerLoader := loader.NewProviderLoader(factory, func(_ *providers.Router, entries []gatewaymodel.ProviderEntry) {
		if reloadSeedance != nil {
			reloadSeedance(entries)
		}
	})

	// Load providers: try Redis snapshot first, then control-plane HTTP.
	var activeRouter *providers.Router
	if result, err := loadProviders(ctx, rdb, providerLoader, controlPlaneURL, internalToken); err != nil {
		state.MarkLoadFailure(err)
		slog.Warn("failed to load providers, gateway will serve with empty registry", "error", err)
		// Build an empty router so the server can still start.
		if activeRouter, err = providerLoader.Build(nil); err != nil {
			log.Fatalf("failed to build empty router: %v", err)
		}
	} else {
		activeRouter = providerLoader.Router()
		state.MarkLoadSuccess(result.source, activeRouter.ModelCount(), result.version)
	}

	// Start config change subscriber so the loader rebuilds atomically on changes.
	fetchEntries := func() ([]gatewaymodel.ProviderEntry, error) {
		if snapshot, err := configsync.LoadProviderSnapshot(ctx, rdb); err == nil {
			return snapshot.Entries, nil
		}
		if controlPlaneURL != "" && internalToken != "" {
			return infra.LoadFromControlPlane(controlPlaneURL, internalToken)
		}
		return nil, fmt.Errorf("no config source available")
	}

	subscriber := infra.NewSubscriber(rdb, func(ev infra.ChangeEvent) {
		// 配额快照事件由 quota.ConfigLoader 处理，这里只负责 provider 热更新。
		if ev.ResourceType == "quota" ||
			ev.Type == "quota_snapshot_updated" ||
			ev.Type == "quota_config_changed" {
			return
		}
		providerLoader.HandleChangeEvent(fetchEntries)(ev)
		if r := providerLoader.Router(); r != nil {
			state.MarkLoadSuccess("reload", r.ModelCount(), ev.SnapshotVersion)
		}
	})
	subscriber.AddCallback(keyCache.HandleChangeEvent)
	tierHolder := budget.NewSnapshotHolder()
	tierLoader := budget.NewConfigLoader(rdb, tierHolder)
	subscriber.AddCallback(tierLoader.HandleChangeEvent)
	go tierLoader.Start(ctx, 30*time.Second)

	// 创建 binding client（热路径：模型组权限校验），在 main 层管理生命周期
	// 以便将 Key 禁用事件接入缓存失效回调
	var bindClient *binding.Client
	if controlPlaneURL != "" && internalToken != "" {
		bindClient = binding.NewClient(controlPlaneURL, internalToken)
		// Key 禁用/删除时主动清除 binding 缓存，避免旧授权残留
		subscriber.AddCallback(func(ev infra.ChangeEvent) {
			if ev.ResourceType == "api_key" {
				switch ev.Type {
				case "key_disabled", "key_deleted":
					bindClient.Cache().Invalidate(ev.ResourceID)
				}
			}
		})
	}

	go subscriber.Start(ctx)

	// Periodic reconciliation loop (every 30s) – brings in any changes missed
	// by pub/sub (e.g. gateway restart after config push).
	go reconcileProviders(ctx, rdb, providerLoader, state, controlPlaneURL, internalToken)

	// 初始化 Scheduler
	var sch *scheduler.Scheduler
	var nameCache *cache.GroupNameCache
	if controlPlaneURL != "" {
		schCfg := scheduler.DefaultConfig(controlPlaneURL)
		schCfg.InternalToken = internalToken
		schCfg.RedisClient = rdb
		sch, err = scheduler.New(schCfg)
		if err != nil {
			slog.Warn("failed to create scheduler", "error", err)
		} else {
			sch.Start(ctx)
			defer sch.Shutdown()
			slog.Info("scheduler started")

			// 初始化模型组名称缓存：阻塞等待首次刷新成功，
			// 避免 GroupNameCache 尚未就绪时让请求走 direct 路径误返回 404。
			// 最多尝试 5 次 × 2s 间隔，控制面就绪后即可进入定期刷新循环。
			nameCache = cache.NewGroupNameCache(internalToken)
			if err := nameCache.RefreshWithRetry(ctx, controlPlaneURL, 5, 2*time.Second); err != nil {
				slog.Error("group name cache failed to initialize, group requests may 404 until next refresh", "error", err)
			}
			go refreshGroupNameCache(ctx, nameCache, controlPlaneURL)
			slog.Info("group name cache started", "initialized", nameCache.IsInitialized())
		}
	}

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(shutdownSignal)

	authClient := infra.NewAuthClient(cfg.Auth.URL, cfg.Auth.Token)
	authClient.SetKeyCache(keyCache)

	// 成本计算始终启用并从最终路由模型的实时 provider registry 读取价格桶；
	// call_logs 持久化仍可在 DB 不可用时独立降级。
	providerPricing := gwobs.ProviderPricingResolverFunc(func(modelKey, provider string) (core.ModelPricing, bool) {
		return providerLoader.Router().ResolvePricing(modelKey, provider)
	})
	collectorCfg := gwobs.DefaultCollectorConfig()
	collectorCfg.SpoolCapacity = cfg.Observability.SpoolCapacity
	collectorCfg.SpoolHighWatermark = cfg.Observability.SpoolHighWatermark
	collectorCfg.SpoolClaimMinIdle = cfg.Observability.SpoolClaimMinIdle
	collectorCfg.BillableReservationTTL = cfg.Observability.BillableReservationTTL
	collector := gwobs.StartProductionCollector(
		ctx,
		collectorCfg,
		rdb,
		providerPricing,
		func(context.Context) (*gorm.DB, error) {
			return cpinfra.InitPostgres(cfg.Database.URL)
		},
		nil,
	)
	var traceWriter *gwobs.TraceWriter
	if db, err := cpinfra.InitPostgres(cfg.Database.URL); err != nil {
		slog.Warn("request trace persistence unavailable at startup; call logs remain spooled", "error", err)
	} else {
		// 请求/响应 trace 写入器
		traceWriter = gwobs.NewTraceWriter(db, 1024, 50, 2*time.Second)
		go traceWriter.Run(ctx)

		slog.Info("usage tracking enabled (direct-DB mode)")
	}

	// Wire auth services (JWT / Key Auth / OIDC)
	authJWTManager, err := acrypto.NewJWTManagerFromEnv()
	if err != nil {
		slog.Warn("JWT manager init failed, JWKS/token endpoints disabled", "error", err)
	}
	authTokenSvc := authsvc.NewTokenService(authJWTManager)

	authHTTPClient := &http.Client{Timeout: 5 * time.Second}
	authKeyAuthSvc := authsvc.NewKeyAuthService(authHTTPClient, rdb, controlPlaneURL, internalToken)

	// Auth handlers
	authJWKSHandler := authhandler.NewJWKSHandler()
	authTokenHandler := authhandler.NewTokenHandler(authTokenSvc, rdb)
	authInternalHandler := authhandler.NewInternalHandler(authKeyAuthSvc)

	// Initialize router (inject dependencies). We always use the router obtained
	// from the ProviderLoader – which is atomically swapped on config changes.
	// Seedance 视频/素材适配器同样挂到 config:change，改渠道后无需重启。
	var budgetAlertDispatcher *server.BudgetAlertDispatcher
	if controlPlaneURL != "" {
		budgetAlertDispatcher = server.NewBudgetAlertDispatcher(
			ctx,
			server.NewBudgetAlertClient(&http.Client{Timeout: 3 * time.Second}, controlPlaneURL, internalToken),
			server.BudgetAlertDispatcherConfig{},
		)
	}
	r := server.SetupWithBudgetAlertDispatcher(budgetAlertDispatcher, providerLoader.Router, icache, state, authClient, authJWTManager, controlPlaneURL, internalToken, sch, nameCache, collector, traceWriter, bindClient, rdb, tierHolder,
		func(reload func(entries []gatewaymodel.ProviderEntry)) {
			reloadSeedance = reload
		},
	)

	// Register auth routes
	r.GET("/.well-known/jwks.json", authJWKSHandler.GetJWKS)
	r.POST("/auth/refresh", authTokenHandler.Refresh)
	r.POST("/auth/logout", authTokenHandler.Logout)
	r.POST("/internal/v1/auth/verify-key", authInternalHandler.VerifyKey)

	// 校验并发租约是否覆盖运行时生效的上游超时。
	// 生效值取自 httpclient.DefaultConfig() 的**返回值**（而非常量），因此这里能捕获
	// HTTP_TIMEOUT / HTTP_RESPONSE_HEADER_TIMEOUT 把上游超时放大到租约之上的配置——
	// 那种配置下合法长请求会在租约到期后仍持有上游连接，槽位被并发复用，限额被静默削弱。
	// 刻意只告警不退出：把配置问题升级成「网关拒绝启动」是可用性故障。
	upstreamCfg := httpclient.DefaultConfig()
	if leaseErr := quota.ValidateLeaseCoversUpstream(quota.ConcurrencyLease(), upstreamCfg.Timeout, upstreamCfg.ResponseHeaderTimeout); leaseErr != nil {
		slog.Warn("concurrency lease does not cover the effective upstream timeout; slots of long streaming requests may be reused after the lease expires",
			"error", leaseErr, "concurrency_lease", quota.ConcurrencyLease())
	}

	// 启动服务。收到信号后先停止接入并等待在途请求，再取消后台
	// context，最后等待 call_logs 完成队列 drain / spool。
	slog.Info("gateway starting", "port", cfg.Server.Port)
	httpServer := &http.Server{Addr: ":" + cfg.Server.Port, Handler: r}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- httpServer.ListenAndServe()
	}()
	select {
	case sig := <-shutdownSignal:
		slog.Info("received shutdown signal", "signal", sig.String())
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("gateway HTTP shutdown failed", "error", err)
		}
		shutdownCancel()
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("gateway server stopped unexpectedly: %v", err)
		}
	}
	cancel()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 8*time.Second)
	if err := collector.Wait(drainCtx); err != nil {
		slog.Error("call log writer drain timed out", "error", err)
	}
	if err := budgetAlertDispatcher.Wait(drainCtx); err != nil {
		slog.Error("budget alert dispatcher drain timed out", "error", err)
	}
	drainCancel()
}

// buildProviderFactory creates a ProviderFactory and registers all known provider types.
func buildProviderFactory() *providers.ProviderFactory {
	factory := providers.NewProviderFactory()

	// Register GoModel provider adapters.
	factory.Add(providersanthropic.Registration)
	factory.Add(providersazure.Registration)
	factory.Add(providersbedrock.Registration)
	factory.Add(providersbailian.Registration)
	// "dashscope" is the legacy type name used by the Control Plane for Bailian/DashScope.
	// Register it as an alias so existing DB records continue to work.
	factory.Add(providers.Registration{
		Type:      "dashscope",
		New:       providersbailian.New,
		Discovery: providersbailian.Registration.Discovery,
	})
	factory.Add(providerscohere.Registration)
	factory.Add(providersdeepseek.Registration)
	factory.Add(providersgemini.Registration)
	factory.Add(providersgroq.Registration)
	factory.Add(providerslitellm.Registration)
	// "openai-compatible" is the type name the Control Plane uses for generic
	// self-hosted OpenAI-compatible endpoints (e.g. GoModel on :9050). Map it to
	// the LiteLLM adapter, which is a generic OpenAI-compatible provider with a
	// configurable base URL. Register the underscore variant too for safety.
	factory.Add(providers.Registration{
		Type:      "openai-compatible",
		New:       providerslitellm.New,
		Discovery: providerslitellm.Registration.Discovery,
	})
	factory.Add(providers.Registration{
		Type:      "openai_compatible",
		New:       providerslitellm.New,
		Discovery: providerslitellm.Registration.Discovery,
	})
	factory.Add(providersminimax.Registration)
	factory.Add(providersopenai.Registration)
	factory.Add(providersopenrouter.Registration)
	factory.Add(providersollama.Registration)
	factory.Add(providersoracle.Registration)
	factory.Add(providersvertex.Registration)
	factory.Add(providersvllm.Registration)
	factory.Add(providersxai.Registration)
	factory.Add(providerszai.Registration)
	factory.Add(seedancerelay.Registration)
	factory.Add(seedancevolcengine.Registration)

	return factory
}

func refreshGroupNameCache(ctx context.Context, nc *cache.GroupNameCache, controlPlaneURL string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// 立即执行一次
	nc.Refresh(ctx, controlPlaneURL)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := nc.Refresh(ctx, controlPlaneURL); err != nil {
				slog.Warn("group name cache refresh failed", "error", err)
			}
		}
	}
}

func loadProviders(ctx context.Context, rdb *redis.Client, pl *loader.ProviderLoader, controlPlaneURL, token string) (*providerLoadResult, error) {
	// Try Redis snapshot first.
	if snapshot, err := configsync.LoadProviderSnapshot(ctx, rdb); err == nil && len(snapshot.Entries) > 0 {
		if _, buildErr := pl.Build(snapshot.Entries); buildErr != nil {
			slog.Warn("failed to build router from redis snapshot", "error", buildErr)
		} else {
			slog.Info("providers loaded from redis snapshot successfully")
			return &providerLoadResult{source: "redis-snapshot", version: snapshot.Version}, nil
		}
	} else {
		slog.Warn("failed to load or use redis snapshot", "error", err)
	}

	// Fall back to control-plane HTTP.
	if controlPlaneURL != "" && token != "" {
		entries, err := infra.LoadFromControlPlane(controlPlaneURL, token)
		if err != nil {
			return nil, fmt.Errorf("load providers from control-plane: %w", err)
		}
		if _, buildErr := pl.Build(entries); buildErr != nil {
			return nil, fmt.Errorf("build router from control-plane entries: %w", buildErr)
		}
		slog.Info("providers loaded from control-plane successfully")
		return &providerLoadResult{source: "control-plane", version: 0}, nil
	}

	return nil, fmt.Errorf("no provider snapshot or control-plane source available")
}

func reconcileProviders(ctx context.Context, rdb *redis.Client, pl *loader.ProviderLoader, state *provider.RuntimeState, controlPlaneURL, token string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _, _, _, _, currentVersion := state.Snapshot()
			if snapshot, err := configsync.LoadProviderSnapshot(ctx, rdb); err == nil {
				if pl.Router() != nil && pl.Router().ModelCount() > 0 && snapshot.Version == currentVersion {
					continue
				}
			}

			if result, err := loadProviders(ctx, rdb, pl, controlPlaneURL, token); err != nil {
				state.MarkLoadFailure(err)
				slog.Warn("periodic provider reconcile failed", "error", err)
			} else {
				router := pl.Router()
				modelCount := 0
				if router != nil {
					modelCount = router.ModelCount()
				}
				state.MarkLoadSuccess(result.source, modelCount, result.version)
			}
		}
	}
}

// resolveLogLevel maps LOG_LEVEL env var string to slog.Level.
// Supported values (case-insensitive): debug, info, warn, error.
// Defaults to slog.LevelInfo for unknown or empty values.
func resolveLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// test: fast deploy pipeline speed test 19:24:54
