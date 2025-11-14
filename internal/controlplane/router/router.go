package router

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	"github.com/example/llm-gateway/internal/controlplane/auth"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/middleware"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Setup 初始化 Gin 路由和中间件链
func Setup(db *gorm.DB, rdb *redis.Client) *gin.Engine {
	// 加载配置（YAML + 环境变量）
	cfg := cpconfig.Load()

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
	settingsRepo := repository.NewSettingsRepo(db)
	settingsSvc := service.NewSettingsService(settingsRepo)

	// SSO/OAuth2 认证路由（无需认证）
	authCfg := auth.LoadConfig()
	applyOIDCFromDB(authCfg, settingsSvc)
	authHandler := auth.NewHandler(authCfg, db, rdb)
	// SSO login redirect (frontend: GET /api/v1/auth/oidc/login)
	r.GET("/api/v1/auth/oidc/login", authHandler.Login)
	// SSO callback (frontend: POST /auth/oidc/callback)
	r.POST("/auth/oidc/callback", authHandler.Callback)
	r.POST("/auth/refresh", authHandler.Refresh)
	r.POST("/auth/logout", authHandler.Logout)
	// 需要认证的用户信息接口
	r.GET("/api/v1/me", middleware.JWTAuth(), authHandler.GetCurrentUser)

	// Dev login (no auth required)
	devLoginHandler := handler.NewDevLoginHandler(db, authCfg.JWTSecret, authCfg.AccessTokenTTL)
	r.POST("/auth/dev/login", devLoginHandler.Login)

	// 初始化依赖
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	defRepo := repository.NewProviderDefinitionRepo(db)
	modelTagRepo := repository.NewModelTagRepo(db)
	healthCheckRepo := repository.NewHealthCheckRepo(db)

	keyRepo := repository.NewKeyRepo(db)

	if err := crypto.InitFromDB(db); err != nil {
		slog.Warn("crypto key manager init failed", "error", err)
	}

	// 创建事件发布器
	publisher := event.NewPublisher(rdb)
	snapshotter := service.NewProviderSnapshotRefresher(modelRepo, rdb)

	providerSvc := service.NewProviderService(providerRepo, modelRepo, publisher)
	providerSvc.SetSnapshotRefresher(snapshotter)
	modelSvc := service.NewModelService(modelRepo, providerRepo, publisher)
	modelSvc.SetSnapshotRefresher(snapshotter)
	internalSvc := service.NewInternalService(modelRepo, providerRepo)
	healthCheckSvc := service.NewHealthCheckService(healthCheckRepo, providerRepo, modelRepo, rdb)

	groupRepo := repository.NewGroupRepo(db)
	keyGroupRepo := repository.NewKeyGroupRepo(db)
	keyGroupSvc := service.NewKeyGroupService(keyGroupRepo, groupRepo, keyRepo, service.NewEventPublisherAdapter(publisher))
	keySvc := service.NewKeyService(keyRepo, keyGroupSvc, service.NewEventPublisherAdapter(publisher))

	groupSvc := service.NewGroupService(groupRepo)
	groupSvc.SetFailoverDefaults(
		settingsSvc.GetDefaultMaxSwitches(context.Background()),
		settingsSvc.GetDefaultSameModelRetries(context.Background()),
		settingsSvc.GetDefaultFailoverCooldownSeconds(context.Background()),
	)
	groupHandler := handler.NewGroupHandler(groupSvc, modelRepo, providerRepo)

	systemKeySvc := service.NewSystemKeyService(db)
	// gateway.url 从 yaml 配置读取，环境变量 GATEWAY_URL 可覆盖
	groupCheckSvc := service.NewGroupCheckService(groupRepo, healthCheckRepo, systemKeySvc, cfg.Gateway.URL)
	groupCheckHandler := handler.NewGroupCheckHandler(groupCheckSvc)
	healthCheckSvc.SetGroupCheckService(groupCheckSvc)

	// 启动时确保系统探针 key 存在
	go func() {
		if _, err := systemKeySvc.EnsureProbeKey(context.Background()); err != nil {
			slog.Warn("failed to ensure system probe key", "error", err)
		}
	}()

	providerHandler := handler.NewProviderHandler(providerSvc)
	modelHandler := handler.NewModelHandler(modelSvc)
	internalHandler := handler.NewInternalHandler(internalSvc, keySvc, keyGroupSvc)
	defHandler := handler.NewDefinitionHandler(defRepo)
	testHandler := handler.NewTestHandler(providerRepo, modelRepo)
	modelTagHandler := handler.NewModelTagHandler(modelTagRepo)
	healthCheckHandler := handler.NewHealthCheckHandler(healthCheckSvc)
	modelCheckHandler := handler.NewModelCheckHandler(modelRepo, providerRepo, healthCheckRepo)
	syncSvc := service.NewSyncService(providerRepo, modelRepo, publisher)
	syncSvc.SetSnapshotRefresher(snapshotter)
	syncHandler := handler.NewSyncHandler(syncSvc, providerRepo)

	keyHandler := handler.NewKeyHandler(keySvc)
	settingsHandler := handler.NewSettingsHandler(settingsSvc)
	opencodeConfigHandler := handler.NewOpencodeConfigHandler(rdb, settingsSvc)

	// 模型 Catalog（D1: LiteLLM 单源；D3: bundled fallback；D4: 启动异步首刷 + 24h cron + 手动）
	catalogRepo := repository.NewModelCatalogRepo(db)
	catalogSyncSvc := service.NewCatalogSyncService(catalogRepo, modelRepo, service.CatalogSyncOpts{})
	catalogSvc := service.NewCatalogService(catalogRepo, catalogSyncSvc)
	catalogHandler := handler.NewCatalogHandler(catalogSvc)
	// 让 ModelService 也能解析 catalog（CreateModelFromCatalog/ResetOverride）
	modelSvc.SetCatalogLookup(catalogRepo)
	if _, err := snapshotter.RefreshAvailableSnapshot(context.Background()); err != nil {
		slog.Warn("initial provider snapshot refresh failed", "error", err)
	}

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

	// 公开设置（无需认证）
	r.GET("/api/v1/settings/public", func(c *gin.Context) {
		settingsHandler.GetPublicSettings(c)
	})

	// OpenCode 配置（无需 JWT，token 即凭证）
	r.GET("/api/v1/opencode-config/script", opencodeConfigHandler.GetScript)
	r.POST("/api/v1/opencode-config/callback", opencodeConfigHandler.Callback)

	// 启动健康检测调度器（优先使用环境变量 HEALTH_CHECK_INTERVAL 控制）
	healthCheckSvc.StartBackground(context.Background())

	// 内部调度器接口（X-Auth-Token 认证，在 handler 内自行校验）
	r.GET("/api/v1/internal/groups", groupHandler.ListEnabledGroups)

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
		adminGroup.POST("/providers/:id/check-models", syncHandler.CheckModels)

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
		adminGroup.GET("/health-check/timeline", healthCheckHandler.GetTimeline)
		adminGroup.GET("/health-check/group-timeline", healthCheckHandler.GetGroupTimeline)

		// 模型 Catalog（注意：source_model_id 含 '/'，通过 query 入口而非路径参数访问）
		adminGroup.GET("/model-catalog/search", catalogHandler.Search)

		// API Key 管理
		adminGroup.POST("/keys", keyHandler.CreateKey)
		adminGroup.GET("/keys", keyHandler.ListKeys)
		adminGroup.GET("/keys/:id", keyHandler.GetKey)
		adminGroup.PATCH("/keys/:id/disable", keyHandler.DisableKey)
		adminGroup.PATCH("/keys/:id/enable", keyHandler.EnableKey)
		adminGroup.DELETE("/keys/:id", keyHandler.DeleteKey)
		adminGroup.POST("/keys/:id/reset", keyHandler.ResetKey)
		adminGroup.POST("/keys/:id/reveal", keyHandler.RevealKey)
		adminGroup.GET("/keys/:id/groups", keyHandler.ListKeyGroups)
		adminGroup.POST("/keys/:id/groups", keyHandler.UpdateKeyGroups)
		adminGroup.POST("/keys/:id/access-all-groups", keyHandler.ToggleAccessAllGroups)
		adminGroup.GET("/groups/available", keyHandler.ListAvailableGroups)

		// OpenCode 配置（token 生成 + 状态轮询）
		adminGroup.POST("/keys/:id/opencode-config/token", opencodeConfigHandler.GenerateToken)
		adminGroup.GET("/keys/:id/opencode-config/status", opencodeConfigHandler.GetStatus)
		adminGroup.GET("/model-catalog/by-source", catalogHandler.GetBySource)
		adminGroup.POST("/model-catalog/sync", catalogHandler.Sync)
		adminGroup.GET("/model-catalog/diff", catalogHandler.Diff)
		adminGroup.POST("/model-catalog/apply-diff", catalogHandler.ApplyDiff)
		adminGroup.POST("/model-catalog/auto-bind", catalogHandler.AutoBind)
		adminGroup.GET("/model-catalog/binding-stats", catalogHandler.BindingStats)
		adminGroup.GET("/model-catalog/sync-status", catalogHandler.SyncStatus)
		// :id（UUID）放最后，避免吃掉前面的静态路径
		adminGroup.GET("/model-catalog/:id", catalogHandler.GetByID)

		// 模型分组管理
		adminGroup.GET("/groups/defaults", groupHandler.GetDefaults)
		adminGroup.POST("/groups/check-all", groupCheckHandler.CheckAll)
		adminGroup.POST("/groups", groupHandler.Create)
		adminGroup.GET("/groups", groupHandler.List)
		adminGroup.GET("/groups/:id", groupHandler.GetByID)
		adminGroup.PUT("/groups/:id", groupHandler.Update)
		adminGroup.DELETE("/groups/:id", groupHandler.Delete)
		adminGroup.POST("/groups/:id/models", groupHandler.AddModels)
		adminGroup.GET("/groups/:id/models", groupHandler.ListModels)
		adminGroup.DELETE("/groups/:id/models/:model_id", groupHandler.RemoveModel)
		adminGroup.POST("/groups/:id/check", groupCheckHandler.CheckOne)

		// 系统设置
		adminGroup.GET("/settings", settingsHandler.GetSystemSettings)
		adminGroup.PUT("/settings", settingsHandler.UpdateSettings)
		adminGroup.POST("/settings/test-webhook", settingsHandler.TestWebhook)
		adminGroup.POST("/settings/export", settingsHandler.ExportSettings)
		adminGroup.POST("/settings/import", settingsHandler.ImportSettings)
	}

	// 内部接口组（内部 Token 认证）
	internalGroup := r.Group("/internal/v1")
	internalGroup.Use(middleware.InternalAuth())
	{
internalGroup.GET("/models/available", internalHandler.ListAvailableModels)
	internalGroup.GET("/models/health", internalHandler.GetModelHealth)
	internalGroup.POST("/keys/verify", keyHandler.VerifyKey)
	internalGroup.POST("/auth/verify-key", keyHandler.VerifyKey)
	internalGroup.GET("/keys/:id/groups", internalHandler.GetKeyGroups)
	}

	return r
}

func applyOIDCFromDB(cfg *auth.Config, settingsSvc *service.SettingsService) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	settings, err := settingsSvc.GetSystemSettings(ctx)
	if err != nil || settings == nil {
		return
	}
	if !settings.OIDCEnabled {
		return
	}
	if settings.OIDCIssuerURL != "" {
		cfg.IssuerURL = settings.OIDCIssuerURL
	}
	if settings.OIDCClientID != "" {
		cfg.ClientID = settings.OIDCClientID
	}
	if settings.OIDCClientSecret != "" {
		cfg.ClientSecret = settings.OIDCClientSecret
	}
	if settings.OIDCRedirectURL != "" {
		cfg.RedirectURI = settings.OIDCRedirectURL
	}
	if settings.OIDCScopes != "" {
		cfg.Scopes = splitScopes(settings.OIDCScopes)
	}
}

func splitScopes(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(strings.ReplaceAll(s, ",", " "))
}
