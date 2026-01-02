package router

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	authrepo "github.com/example/llm-gateway/internal/auth/repository"
	"github.com/example/llm-gateway/internal/controlplane/auth"
	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/middleware"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	obscron "github.com/example/llm-gateway/internal/observability/cron"
	obshandler "github.com/example/llm-gateway/internal/observability/handler"
	obssvc "github.com/example/llm-gateway/internal/observability/service"
	"github.com/example/llm-gateway/internal/orgservice"
	orghandler "github.com/example/llm-gateway/internal/orgservice/handler"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
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
	announcementRepo := repository.NewAnnouncementRepo(db)
	healthCheckRepo := repository.NewHealthCheckRepo(db)
	modelStatusLogRepo := repository.NewModelStatusLogRepo(db)

	keyRepo := repository.NewKeyRepo(db)

	if err := crypto.InitFromDB(db); err != nil {
		slog.Warn("crypto key manager init failed", "error", err)
	}

	// 创建事件发布器
	publisher := event.NewPublisher(rdb)
	snapshotter := service.NewProviderSnapshotRefresher(modelRepo, rdb)

	// 创建状态日志服务（注入到需要记录日志的服务中）
	modelStatusLogSvc := service.NewModelStatusLogService(modelStatusLogRepo)

	providerSvc := service.NewProviderService(providerRepo, modelRepo, publisher)
	providerSvc.SetSnapshotRefresher(snapshotter)
	modelSvc := service.NewModelService(modelRepo, providerRepo, publisher)
	modelSvc.SetSnapshotRefresher(snapshotter)
	modelSvc.SetModelStatusLogService(modelStatusLogSvc)
	internalSvc := service.NewInternalService(modelRepo, providerRepo)
	healthCheckSvc := service.NewHealthCheckService(healthCheckRepo, providerRepo, modelRepo, rdb)
	healthCheckSvc.SetModelStatusLogService(modelStatusLogSvc)
	healthCheckSvc.SetSnapshotRefresher(snapshotter)

	groupRepo := repository.NewGroupRepo(db)
	keyGroupRepo := repository.NewKeyGroupRepo(db)
	keyGroupSvc := service.NewKeyGroupService(keyGroupRepo, groupRepo, keyRepo, service.NewEventPublisherAdapter(publisher))
	keySvc := service.NewKeyService(keyRepo, keyGroupSvc, service.NewEventPublisherAdapter(publisher))

	// KeyOrgSyncer：org 变动后异步同步 api_keys org 字段并失效 KeyCache
	keyOrgSyncer := service.NewKeyOrgSyncer(db, service.NewEventPublisherAdapter(publisher))

	groupSvc := service.NewGroupService(groupRepo)
	groupSvc.SetFailoverDefaults(
		settingsSvc.GetDefaultMaxSwitches(context.Background()),
		settingsSvc.GetDefaultSameModelRetries(context.Background()),
		settingsSvc.GetDefaultFailoverCooldownSeconds(context.Background()),
	)
	groupHandler := handler.NewGroupHandler(groupSvc, modelRepo, providerRepo)

	// v0.7: 授权服务
	entRepo := repository.NewEntitlementRepo(db)
	entSvc := service.NewEntitlementService(entRepo, groupRepo, service.NewEventPublisherAdapter(publisher))
	// 注入 EntitlementService 到 KeyService，enable verify-key 下发 usable_groups 快照
	keySvc.WithEntitlementService(entSvc)
	// v0.7: 预算 repo，注入 KeyService 用于 verify-key 下发 BudgetChain
	budgetRepo := repository.NewBudgetRepo(db)
	keySvc.WithBudgetRepo(budgetRepo)
	budgetAlertRepo := repository.NewBudgetAlertRepo(db)
	budgetSvc := service.NewBudgetService(budgetRepo, budgetAlertRepo)
	// v0.7: 改额即时生效——注入 publisher 用于广播 auth_config_changed，
	// 注入 Redis 用于写 budget:amount:{scope_type}:{scope_id} 键。
	budgetSvc.SetPublisher(service.NewEventPublisherAdapter(publisher))
	budgetSvc.SetRedis(rdb)
	budgetAlertSvc := service.NewBudgetAlertService(budgetRepo, budgetAlertRepo, nil)
	// v0.7: 上线默认预算初始化（异步，不阻塞启动）
	// 默认额度：部门日常 500 USD/月，部门程序服务 200 USD/月，工作室 2000 USD/月
	// 运维可通过 PUT /api/v1/budgets 按域调整
	go func() {
		if created, err := budgetSvc.InitDefaultBudgets(
			context.Background(),
			500.0,  // defaultDeptDailyUSD
			200.0,  // defaultDeptServiceUSD
			2000.0, // defaultStudioUSD
		); err != nil {
			slog.Warn("budget: default allocation init failed", "error", err)
		} else if created > 0 {
			slog.Info("budget: default allocations initialized", "created", created)
		}
	}()
	// v0.7: 扩容审批 service
	expansionRepo := repository.NewExpansionRepo(db)
	expansionSvc := service.NewExpansionService(expansionRepo)
	expansionSvc.SetBudgetService(budgetSvc)
	expansionUserRepo := authrepo.NewUserRepository(db)

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
	// 注入定价服务（供 /internal/v1/models/pricing 使用）
	pricingSvc := service.NewPricingService(db)
	go func() {
		if err := pricingSvc.Refresh(context.Background()); err != nil {
			slog.Warn("initial pricing table load failed", "error", err)
		}
	}()
	internalHandler.SetPricingService(pricingSvc)
	modelInfoSvc := service.NewModelInfoService(groupRepo, modelRepo)
	modelInfoHandler := handler.NewModelInfoHandler(modelInfoSvc)
	defHandler := handler.NewDefinitionHandler(defRepo)
	testHandler := handler.NewTestHandler(providerRepo, modelRepo)
	modelTagHandler := handler.NewModelTagHandler(modelTagRepo)
	announcementHandler := handler.NewAnnouncementHandler(announcementRepo)
	healthCheckHandler := handler.NewHealthCheckHandler(healthCheckSvc)
	modelCheckHandler := handler.NewModelCheckHandler(modelRepo, providerRepo, healthCheckRepo)
	syncSvc := service.NewSyncService(providerRepo, modelRepo, publisher)
	syncSvc.SetSnapshotRefresher(snapshotter)
	syncHandler := handler.NewSyncHandler(syncSvc, providerRepo)

	// 模型状态变更日志
	modelStatusLogHandler := handler.NewModelStatusLogHandler(modelStatusLogRepo)

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

	// 公开公告（无需认证）
	r.GET("/api/v1/public/announcements", announcementHandler.ListActive)
	r.GET("/api/v1/public/announcements/:id", announcementHandler.GetPublicByID)

	// OpenCode 配置（无需 JWT，token 即凭证）
	r.GET("/api/v1/opencode-config/script", opencodeConfigHandler.GetScript)
	r.GET("/api/v1/opencode-config/plugin", opencodeConfigHandler.GetPlugin)
	r.GET("/api/v1/opencode-config/plugin/version", opencodeConfigHandler.GetPluginVersion)
	r.POST("/api/v1/opencode-config/callback", opencodeConfigHandler.Callback)

	// 模型组静态路由：在 adminGroup 之前注册，避免被 /groups/:id 吞掉
	r.GET("/api/v1/groups/capabilities", middleware.JWTAuth(), modelInfoHandler.GetGroupModelInfo)
	r.GET("/api/v1/groups/defaults", middleware.JWTAuth(), groupHandler.GetDefaults)

	// 启动健康检测调度器（优先使用环境变量 HEALTH_CHECK_INTERVAL 控制）
	healthCheckSvc.StartBackground(context.Background())

	// 启动用量统计每小时聚合（写 usage_daily）
	rollupSvc := obssvc.NewRollupService(db)
	rollupCron := obscron.NewRollupCron(rollupSvc)
	rollupCron.Start(context.Background())
	// 启动时立即补跑一次今天 + 昨天，填充 usage_daily
	go func() {
		if err := rollupCron.RunOnce(context.Background()); err != nil {
			slog.Warn("initial rollup failed", "error", err)
		}
	}()

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
		adminGroup.GET("/models/:id/status-logs", modelStatusLogHandler.GetModelStatusLogs)
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

		// 性能看板
		callLogRepo := repository.NewCallLogRepo(db)
		performanceSvc := service.NewPerformanceService(callLogRepo, groupRepo)
		performanceHandler := handler.NewPerformanceHandler(performanceSvc)
		adminGroup.GET("/health/performance", performanceHandler.GetPerformance)

		// 模型状态变更日志
		adminGroup.GET("/model-status-logs", modelStatusLogHandler.ListModelStatusLogs)

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
		adminGroup.PATCH("/keys/:id/log-enabled", keyHandler.ToggleLogEnabled)
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

		// 公告管理
		adminGroup.GET("/announcements", announcementHandler.List)
		adminGroup.POST("/announcements", announcementHandler.Create)
		adminGroup.GET("/announcements/:id", announcementHandler.GetByID)
		adminGroup.PUT("/announcements/:id", announcementHandler.Update)
		adminGroup.DELETE("/announcements/:id", announcementHandler.Delete)
		adminGroup.PATCH("/announcements/:id/publish", announcementHandler.Publish)
		adminGroup.PATCH("/announcements/:id/archive", announcementHandler.Archive)
		adminGroup.POST("/announcements/:id/read", announcementHandler.MarkRead)
		adminGroup.GET("/announcements/unread", announcementHandler.GetUnread)
		adminGroup.GET("/announcements/unread/count", announcementHandler.GetUnreadCount)
		adminGroup.GET("/announcements/history", announcementHandler.GetReadHistory)

		// 用量与成本查询
		usageSvc := obssvc.NewRollupService(db)
		usageH := obshandler.NewUsageHandler(usageSvc, db)
		adminGroup.GET("/usage/logs", usageH.Logs)
		adminGroup.GET("/usage/logs/:request_id/trace", usageH.Trace)
		adminGroup.GET("/usage/options", usageH.Options)
		adminGroup.GET("/usage", usageH.Query)
		adminGroup.GET("/usage-logs", usageH.Logs)
		// 个人用量查询（按登录用户 user_id 隔离）
		adminGroup.GET("/my/keys", keyHandler.MyKeys)
		adminGroup.GET("/my/usage", usageH.MyQuery)
		adminGroup.GET("/my/usage/logs", usageH.MyLogs)
		adminGroup.GET("/my/usage/logs/:request_id/trace", usageH.Trace)
	}

	// 内部接口组（内部 Token 认证）
	internalGroup := r.Group("/internal/v1")
	internalGroup.Use(middleware.InternalAuth())
	{
		internalGroup.GET("/models/available", internalHandler.ListAvailableModels)
		internalGroup.GET("/models/health", internalHandler.GetModelHealth)
		internalGroup.GET("/models/pricing", internalHandler.GetModelPricing)
		internalGroup.POST("/keys/verify", keyHandler.VerifyKey)
		internalGroup.POST("/auth/verify-key", keyHandler.VerifyKey)
		internalGroup.GET("/keys/:id/groups", internalHandler.GetKeyGroups)
		internalGroup.GET("/groups/model-info", modelInfoHandler.GetGroupModelInfo)
	}

	// ─── 组织架构服务（org-service）───────────────────────────────────────────
	orgContainer := orgservice.New(db, rdb, orgservice.BootstrapAdmins())
	// 将 Bootstrap Checker 注入 auth handler（此处已在 orgContainer 创建后）
	authHandler.SetBootstrapChecker(orgContainer.Bootstrap)
	// 注入 KeyOrgSyncer 到 auth handler（SSO 登录部门迁移后同步 api_keys）
	authHandler.SetOrgSyncer(keyOrgSyncer)
	deptRepo := orgrepo.NewDeptRepo(db)
	userOrgRepo := orgrepo.NewUserOrgRepo(db)
	roleRepo := orgrepo.NewRoleRepo(db)
	mappingRepo := orgrepo.NewMappingRepo(db)
	studioRepo := orgrepo.NewStudioRepo(db)
	orgH := orghandler.NewOrgHandler(orgContainer.OrgSvc, orgContainer.StudioSvc, userOrgRepo, roleRepo)
	mappingH := orghandler.NewMappingHandler(orgContainer.MappingSvc, deptRepo)
	orgContainer.MappingSvc.SetOrgSyncer(keyOrgSyncer)
	roleH := orghandler.NewRoleHandler(orgContainer.RoleSvc, mappingRepo)
	empImportSvc := orgservice.NewEmployeeImportSvc(db, studioRepo, deptRepo, mappingRepo)
	empImportSvc.SetOrgSyncer(keyOrgSyncer)
	empImportH := orghandler.NewEmployeeImportHandler(empImportSvc)
	deptAssignH := orghandler.NewDeptAssignHandler(userOrgRepo, deptRepo)
	deptAssignH.SetOrgSyncer(keyOrgSyncer)

	// Guard 快捷别名（绑定到 orgContainer.RBAC 实例）
	guard := orgContainer.RBAC.Guard

	// v0.7: 授权管理路由（挂在 adminGroup 上，已带 JWTAuth）
	entHandler := handler.NewEntitlementHandler(entSvc, orgContainer.RBAC)
	// v0.7: Service Key 管理路由
	serviceKeyHandler := handler.NewServiceKeyHandler(keySvc, orgContainer.RBAC)
	// v0.7: 预算管理路由
	budgetHandler := handler.NewBudgetHandler(budgetSvc, orgContainer.RBAC)
	budgetHandler.SetAlertService(budgetAlertSvc)
	// 注册内部告警触发端点（供 gateway 调用）
	internalGroup.POST("/budget/evaluate-alert", budgetHandler.EvaluateAlert)
	// v0.7: 扩容审批路由
	expansionHandler := handler.NewExpansionHandler(expansionSvc, orgContainer.RBAC, expansionUserRepo)
	{
		// 设置模型组开放模式（仅平台 admin）
		adminGroup.PUT("/groups/:id/open-mode", guard(rbac.PermGroupOpenModeSet), entHandler.SetOpenMode)
		// 查看模型组授权列表
		adminGroup.GET("/groups/:id/entitlements", guard(rbac.PermGroupEntitleGrant), entHandler.ListByGroup)
		// 下放/撤销授权
		adminGroup.POST("/group-entitlements", guard(rbac.PermGroupEntitleGrant), entHandler.Grant)
		adminGroup.DELETE("/group-entitlements", guard(rbac.PermGroupEntitleRevoke), entHandler.Revoke)
		// Service Key CRUD（部门负责人+admin）
		adminGroup.POST("/service-keys", guard(rbac.PermServiceKeyCreate), serviceKeyHandler.Create)
		adminGroup.GET("/service-keys", guard(rbac.PermServiceKeyRevoke), serviceKeyHandler.List)
		adminGroup.PATCH("/service-keys/:id/status", guard(rbac.PermServiceKeyRevoke), serviceKeyHandler.Toggle)
		adminGroup.DELETE("/service-keys/:id", guard(rbac.PermServiceKeyRevoke), serviceKeyHandler.Delete)
		// 预算管理（管理面）
		adminGroup.GET("/budgets", guard(rbac.PermBudgetViewScoped), budgetHandler.List)
		adminGroup.PUT("/budgets", guard(rbac.PermBudgetSet), budgetHandler.Set)
		adminGroup.PUT("/budgets/batch", guard(rbac.PermBudgetSet), budgetHandler.BatchSet)
		adminGroup.DELETE("/budgets", guard(rbac.PermBudgetSet), budgetHandler.Delete)
		adminGroup.GET("/budgets/context", guard(rbac.PermBudgetViewScoped), budgetHandler.Context)
		// 扩容申请：创建/我的申请/流水允许登录用户访问；审批接口继续走 RBAC。
		adminGroup.POST("/expansion-requests", expansionHandler.Create)
		adminGroup.GET("/expansion-requests", expansionHandler.ListMine)
		adminGroup.GET("/expansion-requests/pending", guard(rbac.PermExpansionRequest), expansionHandler.ListPending)
		adminGroup.POST("/expansion-requests/:id/decision", expansionHandler.Decide)
		adminGroup.GET("/expansion-requests/:id/steps", expansionHandler.ListSteps)
		adminGroup.POST("/expansion-requests/batch-decision", guard(rbac.PermExpansionRequest), expansionHandler.BatchDecide)
	}

	// 员工自助查询预算（JWTAuth 即可，不需要 admin）
	r.GET("/api/v1/my/budget", middleware.JWTAuth(), budgetHandler.MyBudget)

	// 管理接口（JWTAuth 已在 adminGroup 上）
	orgGroup := adminGroup.Group("/org")
	{
		// ── 工作室（仅 admin 可增删，所有人可读）─────────────────────────────
		orgGroup.GET("/studios", orgH.ListStudios)
		orgGroup.POST("/studios", guard(rbac.PermStudioCreate), orgH.CreateStudio)
		orgGroup.GET("/studios/:id", orgH.GetStudio)
		orgGroup.DELETE("/studios/:id", guard(rbac.PermStudioDelete), orgH.DeleteStudio)

		// ── 部门与映射（admin 可改映射，所有人可读列表）──────────────────────
		orgGroup.GET("/departments", mappingH.ListDepartments)
		orgGroup.GET("/departments/unmapped", mappingH.ListUnmappedDepts)
		orgGroup.PUT("/departments/:id/studio", guard(rbac.PermMappingEdit), mappingH.SetDeptStudio)
		orgGroup.GET("/dept-studio-map", mappingH.ListMappings)
		orgGroup.POST("/dept-studio-map/import", guard(rbac.PermMappingEdit), mappingH.ImportMappings)

		// ── 成员（管理角色可查，部门范围由 Policy 过滤）─────────────────────
		orgGroup.GET("/departments/:dept_id/members", guard(rbac.PermMemberView), orgH.GetDeptMembers)
		orgGroup.GET("/users", orgH.SearchUsers) // 用户搜索，无额外权限约束

		// ── 角色管理 ─────────────────────────────────────────────────────────
		orgGroup.GET("/all-roles", guard(rbac.PermRoleList), orgH.ListAllRoles)
		orgGroup.POST("/roles/grant", guard(rbac.PermRoleGrant), roleH.GrantRole)
		orgGroup.POST("/roles/revoke", guard(rbac.PermRoleRevoke), roleH.RevokeRole)
		orgGroup.GET("/roles/me", roleH.GetMyRoles) // 当前用户 org 角色（/me 须在 /:user_id 前）
		orgGroup.GET("/roles/:user_id", roleH.GetUserRoles)

		// ── 员工花名册导入（仅 admin）────────────────────────────────────────
		orgGroup.POST("/employees/import", guard(rbac.PermEmployeeImport), empImportH.Import)

		// ── 成员部门归属修正（studio_admin 及以上）───────────────────────────
		orgGroup.PUT("/users/:id/department", guard(rbac.PermDeptChange), deptAssignH.ChangeDepartment)
		orgGroup.PUT("/users/:id/department/follow-sso", guard(rbac.PermSSORevert), deptAssignH.FollowSSO)

		// ── 用量看板：dashboard/scoped-logs 需 Guard 注入 Actor ─────────────
		// usageH2 复用同一 DB，仅在此处注册带 Guard 的路由
		usageH2 := obshandler.NewUsageHandler(obssvc.NewRollupService(db), db)
		adminGroup.GET("/usage/dashboard", guard(rbac.PermUsageViewScoped), usageH2.GetDashboard)
		adminGroup.GET("/usage/dashboard/trend", guard(rbac.PermUsageViewScoped), usageH2.DashboardTrend)
		adminGroup.GET("/usage/dashboard/models", guard(rbac.PermUsageViewScoped), usageH2.DashboardModels)
		adminGroup.GET("/usage/scoped/logs", guard(rbac.PermUsageViewScoped), usageH2.ScopedLogs)
	}

	// 内部接口（InternalAuth 已在 internalGroup 上，复用）
	internalOrgGroup := r.Group("/internal/v1/org")
	internalOrgGroup.Use(middleware.InternalAuth())
	{
		internalOrgGroup.POST("/auth/check-scope", roleH.CheckScope)
		internalOrgGroup.GET("/tree", orgH.GetOrgTree)
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
