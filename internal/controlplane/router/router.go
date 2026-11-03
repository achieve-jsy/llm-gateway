package router

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	aiapphub "github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	authrepo "github.com/example/llm-gateway/internal/auth/repository"
	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/auth"
	cpconfig "github.com/example/llm-gateway/internal/controlplane/config"
	cpcron "github.com/example/llm-gateway/internal/controlplane/cron"
	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/keyactivity"
	"github.com/example/llm-gateway/internal/controlplane/middleware"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/notification"
	obscron "github.com/example/llm-gateway/internal/observability/cron"
	obshandler "github.com/example/llm-gateway/internal/observability/handler"
	obssvc "github.com/example/llm-gateway/internal/observability/service"
	"github.com/example/llm-gateway/internal/orgservice"
	orghandler "github.com/example/llm-gateway/internal/orgservice/handler"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Setup 初始化 Gin 路由和中间件链
func Setup(lifecycleCtx context.Context, db *gorm.DB, rdb *redis.Client) *gin.Engine {
	if lifecycleCtx == nil {
		panic("control-plane lifecycle context is required")
	}
	// 加载配置（YAML + 环境变量）
	cfg := cpconfig.Load()

	r := gin.New()

	// 全局中间件
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	r.Use(middleware.RequestID())

	// 健康检查（无需认证）：只证明进程存活，不检查依赖。
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 就绪检查（无需认证）：额外 ping Postgres/Redis。
	// 蓝绿切流门禁与 compose healthcheck 必须用它而不是 /healthz——否则「进程活着但读不了
	// api_keys」的新颜色会被判为健康、切上流量，用户侧表现为 API Key 全部报错。
	r.GET("/readyz", handler.ReadinessHandler(db, rdb))

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

	// Dev login (no auth required) — opt-in only, defaults OFF and force-disabled
	// in prod (see handler.DevLoginEnabled). Route is not registered when disabled.
	if handler.DevLoginEnabled() {
		devLoginHandler := handler.NewDevLoginHandler(db, authCfg.JWTSecret, authCfg.AccessTokenTTL)
		r.POST("/auth/dev/login", devLoginHandler.Login)
	}

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
	quickOnboardSvc := service.NewQuickOnboardService(db, publisher)
	quickOnboardSvc.SetSnapshotRefresher(snapshotter)
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

	// Org container (create early for entitlement DI + RBAC guard)
	orgContainer := orgservice.New(db, rdb, orgservice.BootstrapAdmins())
	authHandler.SetBootstrapChecker(orgContainer.Bootstrap)

	// 模型组授权服务 & RBAC guard
	entitlementRepo := repository.NewEntitlementRepo(db)
	entitlementSvc := service.NewEntitlementService(entitlementRepo, groupRepo, publisher)
	keySvc.SetEntitlementService(entitlementSvc)
	deptRepo := orgrepo.NewDeptRepo(db)
	deptLookup := &deptStudioAdapter{repo: deptRepo}
	guard := func(perm rbac.Permission) gin.HandlerFunc {
		return func(c *gin.Context) { orgContainer.RBAC.Guard(perm)(c) }
	}

	// KeyOrgSyncer：org 变动后异步同步 api_keys org 字段并失效 KeyCache
	keyOrgSyncer := service.NewKeyOrgSyncer(db, service.NewEventPublisherAdapter(publisher))

	groupSvc := service.NewGroupService(groupRepo, modelRepo)
	groupSvc.SetFailoverDefaults(
		settingsSvc.GetDefaultMaxSwitches(context.Background()),
		settingsSvc.GetDefaultSameModelRetries(context.Background()),
		settingsSvc.GetDefaultFailoverCooldownSeconds(context.Background()),
	)
	groupHandler := handler.NewGroupHandler(groupSvc, modelRepo, providerRepo)
	groupHandler.WithSettingsService(settingsSvc)
	modelTierRepo := repository.NewModelTierRepo(db)
	groupSvc.WithModelTierRepo(modelTierRepo)
	modelTierSvc := service.NewModelTierService(modelTierRepo)
	// 拦截开关：DB（设置页）优先；缺失 key 时用部署 cfg 种子。
	settingsSvc.SeedBudgetEnforcementDefaults(service.BudgetEnforcementSeed{
		MasterEnabled:              cfg.BudgetTier.MasterEnabled,
		AccountingEnabled:          cfg.BudgetTier.AccountingEnabled,
		PositiveEnforcementEnabled: cfg.BudgetTier.PositiveEnforcementEnabled,
		ZeroBlockEnabled:           cfg.BudgetTier.ZeroBlockEnabled,
		ShadowEnabled:              cfg.BudgetTier.ShadowEnabled,
		V2TotalEnforcementEnabled:  cfg.BudgetTier.V2TotalEnforcementEnabled,
		LegacyTotalWriteEnabled:    cfg.BudgetTier.LegacyTotalWriteEnabled,
	})
	budgetSettingsFromDB, _ := settingsSvc.GetSystemSettings(lifecycleCtx)
	masterEnabled := cfg.BudgetTier.MasterEnabled
	accountingEnabled := cfg.BudgetTier.AccountingEnabled
	positiveEnabled := cfg.BudgetTier.PositiveEnforcementEnabled
	zeroBlockEnabled := cfg.BudgetTier.ZeroBlockEnabled
	shadowEnabled := cfg.BudgetTier.ShadowEnabled
	v2TotalEnabled := cfg.BudgetTier.V2TotalEnforcementEnabled
	legacyTotalWriteEnabled := cfg.BudgetTier.LegacyTotalWriteEnabled
	if budgetSettingsFromDB != nil {
		masterEnabled = budgetSettingsFromDB.BudgetMasterEnabled
		accountingEnabled = budgetSettingsFromDB.BudgetAccountingEnabled
		positiveEnabled = budgetSettingsFromDB.BudgetPositiveEnforcementEnabled
		zeroBlockEnabled = budgetSettingsFromDB.BudgetZeroBlockEnabled
		shadowEnabled = budgetSettingsFromDB.BudgetShadowEnabled
		v2TotalEnabled = budgetSettingsFromDB.BudgetV2TotalEnforcementEnabled
		legacyTotalWriteEnabled = budgetSettingsFromDB.BudgetLegacyTotalWriteEnabled
	}
	accountingEnabled = accountingEnabled || positiveEnabled || shadowEnabled
	budgetSnapshotPublisher := service.NewBudgetTierSnapshotPublisher(
		groupRepo, rdb, publisher, service.BudgetTierSnapshotSettings{
			MasterEnabled:              masterEnabled,
			AccountingEnabled:          accountingEnabled,
			PositiveEnforcementEnabled: positiveEnabled,
			ZeroBlockEnabled:           zeroBlockEnabled,
			ShadowEnabled:              shadowEnabled,
			V2TotalEnforcementEnabled:  v2TotalEnabled,
			LegacyTotalWriteEnabled:    legacyTotalWriteEnabled,
			Timezone:                   cfg.Budget.Timezone,
			WeekStart:                  cfg.Budget.WeekStart,
		}, time.Now,
	)
	settingsSvc.WithBudgetEnforcement(budgetSnapshotPublisher)
	groupSvc.WithBudgetTierSnapshotPublisher(budgetSnapshotPublisher)
	modelTierSvc.WithSnapshotPublisher(budgetSnapshotPublisher)
	go budgetSnapshotPublisher.StartReconcile(lifecycleCtx, 30*time.Second)
	modelTierHandler := handler.NewModelTierHandler(modelTierSvc).WithGroupService(groupSvc)

	// v0.7: 预算 repo，注入 KeyService 用于 verify-key 下发 BudgetChain
	budgetRepo := repository.NewBudgetRepo(db)
	keySvc.WithBudgetRepo(budgetRepo)
	budgetAlertRepo := repository.NewBudgetAlertRepo(db)
	budgetSvc := service.NewBudgetService(budgetRepo, budgetAlertRepo)
	budgetTierRepo := repository.NewBudgetTierRepo(db)
	budgetSvc.SetLifecycleScheduler(budgetTierRepo)
	keySvc.WithBudgetTierRepo(budgetTierRepo)
	budgetTierSvc := service.NewBudgetTierService(budgetTierRepo, budgetRepo, modelTierRepo, time.Now).
		WithPeriodConfig(budgetperiod.Config{Timezone: cfg.Budget.Timezone, WeekStart: cfg.Budget.WeekStart}).
		WithTierUsageRepository(repository.NewCallLogRepo(db)).
		WithSnapshotPublisher(budgetSnapshotPublisher).
		WithRedis(rdb)
	go budgetTierSvc.StartPreviewCacheCleanup(lifecycleCtx, time.Minute)
	budgetTierHandler := handler.NewBudgetTierHandler(budgetTierSvc).WithScopeResolver(budgetSvc)
	// v0.7: 改额即时生效——注入 publisher 用于广播 auth_config_changed，
	// 注入 Redis 用于写 budget:amount:{scope_type}:{scope_id} 键。
	budgetSvc.SetPublisher(service.NewEventPublisherAdapter(publisher))
	budgetSvc.SetRedis(rdb)
	budgetAlertSvc := service.NewBudgetAlertService(budgetRepo, budgetAlertRepo, nil)
	budgetAlertSvc.SetRedis(rdb)
	budgetAlertSvc.SetBudgetTierRepo(budgetTierRepo)
	budgetRiskSvc := service.NewBudgetRiskService(
		repository.NewBudgetRiskRepo(db), budgetSvc, budgetTierSvc, time.Now,
	)
	budgetRiskHandler := handler.NewBudgetRiskHandler(budgetRiskSvc, orgContainer.RBAC)
	notificationRepo := repository.NewNotificationRepo(db)
	// v0.8: 审计日志（仅管理面写操作落库，异步批量写，失败不阻断业务）
	auditRepo := repository.NewAuditLogRepo(db)
	auditWriter := service.NewAuditLogWriter(db)
	auditWriter.Start()
	auditSvc := service.NewAuditLogService(auditRepo, auditWriter)
	// 优雅停机时排空审计写队列
	go func() {
		<-lifecycleCtx.Done()
		auditWriter.Stop()
	}()
	// 解耦的通知分发：MultiNotifier = 站内信 + 邮件
	// 调用方只管构造 model.Notification，内部按已配置通道分发。
	emailNotifier := service.NewEmailNotifier(settingsRepo)
	emailNotifier.SetAuditRecorder(auditSvc)
	sharedNotifier := service.NewMultiNotifier(
		service.NewDBNotifier(notificationRepo),
		emailNotifier,
	)
	budgetAlertSvc.SetNotifier(sharedNotifier)
	expansionRepo := repository.NewExpansionRepo(db)
	approvalPoolResolver := service.NewDatabaseApprovalPoolResolver(db)
	budgetOwnerResolver := service.NewDatabaseBudgetOwnerResolver(db, approvalPoolResolver)
	lifecycleObserver := service.NewBudgetLifecycleNotificationObserver(
		db, expansionRepo, budgetOwnerResolver, sharedNotifier,
	)
	budgetLifecycleExecutor := service.NewBudgetLifecycleExecutor(
		budgetTierRepo, budgetRepo, budgetTierRepo, budgetSnapshotPublisher, lifecycleObserver,
	)
	go budgetLifecycleExecutor.Start(lifecycleCtx, time.Minute)

	// 动态 notifier：按 settings 中的 webhook / email 配置构建；
	// Dispatcher 统一负责写日志文件 + Webhook + 邮件三条通道。
	go func() {
		cfg, err := settingsSvc.GetSystemSettings(context.Background())
		if err != nil {
			slog.Warn("budget_alert: failed to load notification settings", "error", err)
			return
		}

		alertLogger := notification.NewAlertLogger("logs")

		var webhookSender *service.WebhookSender
		if cfg.WebhookEnabled && cfg.WebhookURL != "" {
			webhookSender = service.NewWebhookSender(cfg.WebhookURL, cfg.WebhookSecret)
			slog.Info("budget_alert: webhook notifier initialized", "url", cfg.WebhookURL)
		}

		dispatcher := notification.NewDispatcher(nil, alertLogger, webhookSender)
		dispatcher.SetAuditRecorder(newEmailAuditAdapter(auditSvc))
		budgetAlertSvc.SetWebhookNotifier(dispatcher)
	}()
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
	expansionSvc := service.NewExpansionService(expansionRepo)
	expansionSvc.SetBudgetService(budgetSvc)
	expansionSvc.SetBudgetTierService(budgetTierSvc)
	expansionSvc.WithApprovalPool(approvalPoolResolver).
		WithSnapshotPublisher(budgetSnapshotPublisher).
		WithNotifier(sharedNotifier)
	expansionUserRepo := authrepo.NewUserRepository(db)

	// feat/key-approval: Key 创建申请 + 部门策略审批
	deptPolicyRepo := repository.NewDeptPolicyRepo(db)
	keyRequestRepo := repository.NewKeyRequestRepo(db)
	keyRequestSvc := service.NewKeyRequestService(keyRequestRepo, deptPolicyRepo, settingsRepo)
	keyRequestSvc.SetKeyCreator(keySvc)
	keyRequestSvc.SetBudgetService(budgetSvc)
	// 接入 sharedNotifier（站内信 + 邮件），使 Key 申请审批结果也发邮件；
	// 原先直写 NotificationRepo 只发站内信。
	keyRequestSvc.SetNotificationRepo(service.NewKeyRequestNotifierAdapter(sharedNotifier))

	systemKeySvc := service.NewSystemKeyService(db)
	// gateway.url 从 yaml 配置读取，环境变量 GATEWAY_URL 可覆盖
	groupCheckSvc := service.NewGroupCheckService(groupRepo, modelRepo, healthCheckRepo, systemKeySvc, cfg.Gateway.URL)
	groupCheckHandler := handler.NewGroupCheckHandler(groupCheckSvc)
	healthCheckSvc.SetGroupCheckService(groupCheckSvc)

	// 启动时确保系统探针 key 存在
	go func() {
		if _, err := systemKeySvc.EnsureProbeKey(context.Background()); err != nil {
			slog.Warn("failed to ensure system probe key", "error", err)
		}
	}()

	providerHandler := handler.NewProviderHandler(providerSvc)
	quickOnboardHandler := handler.NewProviderQuickOnboardHandler(quickOnboardSvc)
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
	feedbackHandler := handler.NewFeedbackHandler(service.NewFeedbackService(repository.NewFeedbackRepo(db)))
	notificationHandler := handler.NewNotificationHandler(notificationRepo)
	healthCheckHandler := handler.NewHealthCheckHandler(healthCheckSvc)
	modelCheckHandler := handler.NewModelCheckHandler(modelRepo, providerRepo, healthCheckRepo)
	syncSvc := service.NewSyncService(providerRepo, modelRepo, publisher)
	syncSvc.SetSnapshotRefresher(snapshotter)
	syncHandler := handler.NewSyncHandler(syncSvc, providerRepo)

	// 模型状态变更日志
	modelStatusLogHandler := handler.NewModelStatusLogHandler(modelStatusLogRepo)

	keyHandler := handler.NewKeyHandler(keySvc, keyRequestSvc, orgContainer.RBAC)
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
	// capabilities 是模型广场列表价格/能力接口，放开为匿名可调（无需 JWT）
	r.GET("/api/v1/groups/capabilities", modelInfoHandler.GetGroupModelInfo)
	r.GET("/api/v1/groups/defaults", middleware.JWTAuth(), groupHandler.GetDefaults)

	// 启动健康检测调度器（模型级 + 组级，开关由 health_check_configs.enabled 控制）
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

	// Key 活动提醒：离线扫描器（不碰 gateway 热路径）
	// 影子模式（enabled=false）：只写 key_activity_alert 表，不发通知；待观测命中分布合理后再开启通知
	// 复用 sharedNotifier：keyactivity 的扁平签名通过适配器转换成 model.Notification 后分发
	// 到站内信 + 邮件两条通道。
	kaRepo := keyactivity.NewRepo(db)
	kaNotif := service.NewKeyActivityNotifierAdapter(sharedNotifier)
	kaScanner := keyactivity.NewScanner(kaRepo, kaNotif, settingsSvc, kaRepo)
	kaCron := keyactivity.NewCron(kaScanner, settingsSvc)
	kaCron.Start(context.Background())
	go func() {
		if err := kaCron.RunOnce(context.Background()); err != nil {
			slog.Warn("key_activity: initial scan failed", "error", err)
		}
	}()

	// 内部调度器接口（X-Auth-Token 认证，在 handler 内自行校验）
	r.GET("/api/v1/internal/groups", groupHandler.ListEnabledGroups)

	// Channel Monitor 初始化和路由注册
	channelMonitorRepo := repository.NewChannelMonitorRepo(db)
	channelMonitorSvc := service.NewChannelMonitorService(channelMonitorRepo)
	channelMonitorSvc.SetSettingsReader(settingsRepo)
	channelMonitorHandler := handler.NewChannelMonitorHandler(channelMonitorSvc)
	channelMonitorUserHandler := handler.NewChannelMonitorUserHandler(channelMonitorSvc)

	// Channel Monitor Runner (后台调度)
	channelMonitorRunner := service.NewChannelMonitorRunner(channelMonitorSvc)
	channelMonitorSvc.SetRunner(channelMonitorRunner)
	go func() {
		// 启动时加载所有 enabled 的监控
		channelMonitorRunner.StartAll(context.Background())
	}()
	// 每日维护 cron
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if err := channelMonitorSvc.RunDailyMaintenance(ctx); err != nil {
				slog.Error("channel_monitor: daily maintenance failed", "error", err)
			}
			cancel()
		}
	}()

	// 用户侧公开路由（无需认证）
	r.GET("/api/v1/public/channel-monitors", channelMonitorUserHandler.ListPublic)
	r.GET("/api/v1/public/channel-monitors/:id/status", channelMonitorUserHandler.GetStatus)

	// 视频任务服务（admin + internal 共用）
	videoTaskRepo := repository.NewVideoTaskRepo(db)
	videoTaskSvc := service.NewVideoTaskService(videoTaskRepo)
	videoTaskHandler := handler.NewVideoTaskHandler(videoTaskSvc)

	// 点读服务与 Reader 的构造（规格 §8.1；计划 3/4 任务 5 构造、任务 6 接入既有 trace 端点）。
	//
	// 【为什么必须在 adminGroup 之前构造】既有 trace 端点（任务 6）与两条点读端点（任务 5）
	// 必须用**同一个**服务实例：再构造第二个 Reader 会让读侧并发上限
	// （`trace_body.read_max_concurrent`）随实例数翻倍。
	//
	// ① 读侧禁用（trace_body.root 为空）时**不构造** Reader —— NewReader 对空 Root
	//    直接报错，而「读侧未配置」是合法状态（正文仍在 jsonb 列）。此时 opts.Reader 必须
	//    保持「接口值为 nil」：**不得**把 nil 的 *ta.Reader 装进接口字段，装进去后 != nil，
	//    服务会真的去调它并 panic。
	// ② *ta.Reader 天然满足服务里的未导出窄接口，直接赋值即可；读侧禁用 + 行带定位键
	//    会在触碰任何存储之前返回 ErrTraceBodyDisabled ⇒ 503。
	tbOpts := obssvc.TraceBodyServiceOptions{Config: cfg.TraceBodyRead}
	if !cfg.TraceBodyRead.Disabled() {
		tbReader, tbReaderErr := ta.NewReader(ta.ReaderOptions{
			Root:            cfg.TraceBodyRead.Root,
			ZstdWindowBytes: cfg.TraceBodyRead.ZstdWindowBytes,
			MaxConcurrent:   cfg.TraceBodyRead.MaxConcurrent,
		})
		if tbReaderErr != nil {
			// Setup 的签名不返回 error（与既有 panic 风格一致）：构造失败必须响亮失败，
			// 静默忽略会让归档路径无声失效。
			panic("构造 tracearchive.Reader 失败: " + tbReaderErr.Error())
		}
		tbOpts.Reader = tbReader
	}
	tbSvc, tbErr := obssvc.NewTraceBodyService(db, tbOpts)
	if tbErr != nil {
		panic("构造 TraceBodyService 失败: " + tbErr.Error())
	}

	// AI 应用聚合页的同步触发器（全量 / 单工作室 / 单部门）。
	hubSyncSvc := newAIAppHubSyncService(db, settingsRepo)
	hubSyncHandler := handler.NewAIAppHubSyncHandler(hubSyncSvc)
	if aiAppHubEHRConfigured() {
		// 定时同步：默认每天 03:00（AI_APP_HUB_SYNC_TZ，默认北京时间）。
		// AI_APP_HUB_SYNC_CRON 显式设成空串 = 关掉调度（纯手动同步）。
		sched, err := service.LoadSyncSchedule(os.LookupEnv)
		switch {
		case err != nil:
			slog.Error("ai_app_hub_sync_cron_config_invalid", "error", err)
		case sched == nil:
			slog.Info("ai_app_hub_sync_cron_disabled")
		default:
			service.StartAIAppHubSyncCron(lifecycleCtx, service.SyncCronOptions{
				Schedule: sched,
				State:    service.NewSyncStateStore(rdb),
				Runner:   hubSyncSvc,
				// 蓝绿两色都跑 cron：用构建时间戳决定谁先跑，避免备用色（旧镜像）
				// 抢到当天的同步权。取不到就是 0，退回"只有到点才跑"。
				Build: service.BuildEpoch(),
			})
		}
	}

	// 管理接口组（JWT 认证）
	adminGroup := r.Group("/api/v1")
	adminGroup.Use(middleware.JWTAuth())
	adminGroup.Use(middleware.AuditMiddleware(auditSvc, db))
	{
		// 渠道定义（Registry）
		adminGroup.POST("/admin/ai-app-hub/sync", hubSyncHandler.Sync)
		adminGroup.GET("/admin/ai-app-hub/sync-status", hubSyncHandler.Status)
		adminGroup.GET("/provider-definitions", guard(rbac.PermProviderManage), defHandler.ListDefinitions)
		adminGroup.GET("/provider-definitions/:id", guard(rbac.PermProviderManage), defHandler.GetDefinition)

		// 渠道管理
		adminGroup.POST("/providers/quick-onboard/preview", guard(rbac.PermProviderManage), quickOnboardHandler.Preview)
		adminGroup.POST("/providers/quick-onboard", guard(rbac.PermProviderManage), quickOnboardHandler.Create)
		adminGroup.POST("/providers", guard(rbac.PermProviderManage), providerHandler.CreateProvider)
		adminGroup.GET("/providers", guard(rbac.PermProviderManage), providerHandler.ListProviders)
		adminGroup.GET("/providers/:id", guard(rbac.PermProviderManage), providerHandler.GetProvider)
		adminGroup.PUT("/providers/:id", guard(rbac.PermProviderManage), providerHandler.UpdateProvider)
		adminGroup.PATCH("/providers/:id/status", guard(rbac.PermProviderManage), providerHandler.ToggleProviderStatus)
		adminGroup.DELETE("/providers/:id", guard(rbac.PermProviderManage), providerHandler.DeleteProvider)
		adminGroup.POST("/providers/:id/test", guard(rbac.PermProviderManage), testHandler.TestProvider)
		adminGroup.POST("/providers/:id/sync-models", guard(rbac.PermProviderManage), syncHandler.SyncModels)
		adminGroup.POST("/providers/:id/sync-models/confirm", guard(rbac.PermProviderManage), syncHandler.ConfirmSync)
		adminGroup.POST("/providers/:id/check-models", guard(rbac.PermProviderManage), syncHandler.CheckModels)

		// 模型管理
		adminGroup.GET("/model-tiers", guard(rbac.PermModelTierView), modelTierHandler.List)
		// 只读：模型→所属模型组→模型等级(是否高阶) 映射，供用量周报「高阶模型成本占比」。
		adminGroup.GET("/model-tier-hierarchy", guard(rbac.PermModelTierView), modelTierHandler.ModelTierHierarchy)
		adminGroup.POST("/model-tiers", guard(rbac.PermModelTierManage), modelTierHandler.Create)
		adminGroup.PUT("/model-tiers/reorder", guard(rbac.PermModelTierManage), modelTierHandler.Reorder)
		adminGroup.GET("/model-tiers/:id", guard(rbac.PermModelTierView), modelTierHandler.Get)
		adminGroup.PUT("/model-tiers/:id", guard(rbac.PermModelTierManage), modelTierHandler.Update)
		adminGroup.PATCH("/model-tiers/:id/status", guard(rbac.PermModelTierManage), modelTierHandler.SetStatus)
		adminGroup.PATCH("/model-tiers/:id/archive", guard(rbac.PermModelTierManage), modelTierHandler.Archive)
		adminGroup.DELETE("/model-tiers/:id", guard(rbac.PermModelTierManage), modelTierHandler.Delete)
		adminGroup.PUT("/groups/budget-tiers", guard(rbac.PermModelTierManage), modelTierHandler.SetGroupBudgetTierBatch)
		adminGroup.PUT("/groups/:id/budget-tier", guard(rbac.PermModelTierManage), modelTierHandler.SetGroupBudgetTier)
		adminGroup.GET("/budgets/tier-limits", guard(rbac.PermBudgetViewScoped), budgetTierHandler.ListScoped)
		adminGroup.GET("/budgets/:allocation_id/tier-limits", guard(rbac.PermBudgetViewScoped), budgetTierHandler.List)
		adminGroup.PUT("/budgets/:allocation_id/tier-limits", guard(rbac.PermBudgetSet), budgetTierHandler.DeprecatedSet)
		adminGroup.POST("/budgets/tier-limits/batch/preview", guard(rbac.PermBudgetSet), budgetTierHandler.PreviewBatch)
		adminGroup.POST("/budgets/tier-limits/batch/apply", guard(rbac.PermBudgetSet), budgetTierHandler.ApplyBatch)
		adminGroup.GET("/budgets/tier-usage", guard(rbac.PermBudgetViewScoped), budgetTierHandler.TierUsage)
		adminGroup.GET("/my/budget/tiers", budgetTierHandler.MyBudgetTiers)

		adminGroup.POST("/models", guard(rbac.PermModelManage), modelHandler.CreateModel)
		adminGroup.GET("/models", guard(rbac.PermModelManage), modelHandler.ListModels)
		// 注意：静态路径必须在 :id 通配前注册，否则会被吞掉
		adminGroup.GET("/models/check-name", guard(rbac.PermModelManage), modelHandler.CheckModelName)
		adminGroup.POST("/models/check-all", guard(rbac.PermModelManage), modelCheckHandler.CheckAll)
		adminGroup.DELETE("/models/batch", guard(rbac.PermModelManage), modelHandler.BatchDeleteModels)
		adminGroup.PATCH("/models/batch-status", guard(rbac.PermModelManage), modelHandler.BatchUpdateModelStatus)
		adminGroup.PUT("/models/batch-tags", guard(rbac.PermModelManage), modelTagHandler.BatchSetModelTags)
		adminGroup.POST("/models/batch-tags/query", guard(rbac.PermModelManage), modelTagHandler.BatchGetModelTags)
		// CAT-1：catalog 一键创建（静态路径，必须在 :id 之前）
		adminGroup.POST("/models/from-catalog", guard(rbac.PermModelManage), modelHandler.CreateModelFromCatalog)
		adminGroup.POST("/models/batch", guard(rbac.PermModelManage), modelHandler.BatchCreateModels)
		adminGroup.POST("/models/batch-from-catalog", guard(rbac.PermModelManage), modelHandler.BatchCreateModelsFromCatalog)
		// CAT-2：批量绑定（静态路径，必须在 :id 之前）
		adminGroup.POST("/models/batch-bind-catalog", guard(rbac.PermModelManage), modelHandler.BatchBindCatalog)
		adminGroup.GET("/models/:id/status-logs", guard(rbac.PermModelManage), modelStatusLogHandler.GetModelStatusLogs)
		adminGroup.PUT("/models/:id", guard(rbac.PermModelManage), modelHandler.UpdateModel)
		adminGroup.DELETE("/models/:id", guard(rbac.PermModelManage), modelHandler.DeleteModel)
		adminGroup.POST("/models/:id/check", guard(rbac.PermModelManage), modelCheckHandler.CheckOne)
		adminGroup.POST("/models/:id/reset-override", guard(rbac.PermModelManage), modelHandler.ResetModelOverride)
		// CAT-2：单条绑定/换绑/解绑
		adminGroup.POST("/models/:id/bind-catalog", guard(rbac.PermModelManage), modelHandler.BindCatalog)
		adminGroup.POST("/models/:id/unbind-catalog", guard(rbac.PermModelManage), modelHandler.UnbindCatalog)

		// 模型标签
		adminGroup.GET("/model-tags", guard(rbac.PermModelManage), modelTagHandler.ListTags)
		adminGroup.POST("/model-tags", guard(rbac.PermModelManage), modelTagHandler.CreateTag)
		adminGroup.PUT("/model-tags/:id", guard(rbac.PermModelManage), modelTagHandler.UpdateTag)
		adminGroup.DELETE("/model-tags/:id", guard(rbac.PermModelManage), modelTagHandler.DeleteTag)
		adminGroup.GET("/models/:id/tags", guard(rbac.PermModelManage), modelTagHandler.GetModelTags)
		adminGroup.PUT("/models/:id/tags", guard(rbac.PermModelManage), modelTagHandler.SetModelTags)

		// 健康检测管理（配置/触发仅 admin；只读状态与日志按运维看板权限 admin-only）
		adminGroup.GET("/health-check/config", guard(rbac.PermHealthCheckManage), healthCheckHandler.GetConfig)
		adminGroup.PUT("/health-check/config", guard(rbac.PermHealthCheckManage), healthCheckHandler.UpdateConfig)
		adminGroup.GET("/health-check/logs", guard(rbac.PermOpsView), healthCheckHandler.GetLogs)
		adminGroup.GET("/health-check/status", guard(rbac.PermOpsView), healthCheckHandler.GetStatus)
		adminGroup.POST("/health-check/run", guard(rbac.PermHealthCheckManage), healthCheckHandler.RunNow)
		adminGroup.GET("/health-check/timeline", guard(rbac.PermOpsView), healthCheckHandler.GetTimeline)
		adminGroup.GET("/health-check/group-timeline", healthCheckHandler.GetGroupTimeline)

		// 性能看板
		callLogRepo := repository.NewCallLogRepo(db)
		performanceSvc := service.NewPerformanceService(callLogRepo, groupRepo)
		performanceHandler := handler.NewPerformanceHandler(performanceSvc)
		adminGroup.GET("/health/performance", performanceHandler.GetPerformance)

		// 运维看板（Ops Dashboard）
		systemMetricsCollector := service.NewSystemMetricsCollector(db, rdb)
		systemMetricsCollector.Start(context.Background())

		// 系统日志异步写入器（供 sink-health 读取真实状态）
		sysLogWriter := service.NewSystemLogWriter(db)
		sysLogWriter.Start()
		service.SetSystemLogWriter(sysLogWriter)

		// 日志清理 cron（Redis leader lock 保证单实例）
		opsCleanup := service.NewOpsCleanupService(db, rdb)
		// 从 AdvancedSettings 加载保留天数配置
		if val, err := settingsRepo.GetValue("ops_advanced_settings"); err == nil && val != "" {
			opsCleanup.LoadRetentionFromSettings(val)
		}
		opsCleanup.Start(context.Background())

		opsHandler := handler.NewOpsHandler(db, rdb, systemMetricsCollector)

		// WP9: 邮件服务 + 定时报表服务
		emailSvc := service.NewEmailService()
		scheduledReportSvc := service.NewScheduledReportService(db, rdb, emailSvc)
		scheduledReportSvc.Start()
		adminGroup.GET("/ops/groups", guard(rbac.PermOpsView), opsHandler.GetGroupOptions)
		adminGroup.GET("/ops/providers", guard(rbac.PermOpsView), opsHandler.ListProviders)
		adminGroup.GET("/ops/models", guard(rbac.PermOpsView), opsHandler.ListModels)
		adminGroup.GET("/ops/dashboard", guard(rbac.PermOpsView), opsHandler.GetDashboard)
		adminGroup.GET("/ops/dashboard/snapshot-v2", guard(rbac.PermOpsView), opsHandler.GetSnapshotV2)
		adminGroup.GET("/ops/traffic-trend", guard(rbac.PermOpsView), opsHandler.GetTrafficTrend)
		adminGroup.GET("/ops/error-trend", guard(rbac.PermOpsView), opsHandler.GetErrorTrend)
		adminGroup.GET("/ops/latency-histogram", guard(rbac.PermOpsView), opsHandler.GetLatencyHistogram)
		adminGroup.GET("/ops/error-distribution", guard(rbac.PermOpsView), opsHandler.GetErrorDistribution)
		adminGroup.GET("/ops/top-issues", guard(rbac.PermOpsView), opsHandler.GetTopIssues)

		// 运维看板 v2：请求日志、错误分析、并发、趋势
		adminGroup.GET("/ops/requests", guard(rbac.PermOpsView), opsHandler.ListRequests)
		adminGroup.GET("/ops/request-errors", guard(rbac.PermOpsView), opsHandler.ListRequestErrors)
		adminGroup.GET("/ops/request-errors/:id", guard(rbac.PermOpsView), opsHandler.GetRequestErrorDetail)
		adminGroup.GET("/ops/upstream-errors", guard(rbac.PermOpsView), opsHandler.ListUpstreamErrors)
		adminGroup.GET("/ops/concurrency", guard(rbac.PermOpsView), opsHandler.GetConcurrencyStats)
		adminGroup.GET("/ops/switch-rate-trend", guard(rbac.PermOpsView), opsHandler.GetSwitchRateTrend)
		adminGroup.GET("/ops/throughput-trend", guard(rbac.PermOpsView), opsHandler.GetThroughputTrend)

		// 运维看板 v2：告警规则 / 事件 / 静默
		adminGroup.GET("/ops/alert-rules", guard(rbac.PermOpsView), opsHandler.ListAlertRules)
		adminGroup.POST("/ops/alert-rules", guard(rbac.PermOpsView), opsHandler.CreateAlertRule)
		adminGroup.PUT("/ops/alert-rules/:id", guard(rbac.PermOpsView), opsHandler.UpdateAlertRule)
		adminGroup.DELETE("/ops/alert-rules/:id", guard(rbac.PermOpsView), opsHandler.DeleteAlertRule)
		adminGroup.GET("/ops/alert-events", guard(rbac.PermOpsView), opsHandler.ListAlertEvents)
		adminGroup.GET("/ops/alert-events/:id", guard(rbac.PermOpsView), opsHandler.GetAlertEvent)
		adminGroup.PUT("/ops/alert-events/:id/status", guard(rbac.PermOpsView), opsHandler.UpdateAlertEventStatus)
		adminGroup.POST("/ops/alert-silences", guard(rbac.PermOpsView), opsHandler.CreateAlertSilence)

		// 运维看板 v2：系统日志
		adminGroup.GET("/ops/system-logs", guard(rbac.PermOpsView), opsHandler.ListSystemLogs)
		adminGroup.GET("/ops/system-logs/health", guard(rbac.PermOpsView), opsHandler.GetSystemLogSinkHealth)
		adminGroup.POST("/ops/system-logs/cleanup", guard(rbac.PermOpsView), opsHandler.CleanupSystemLogs)

		// 运维看板 v2：运行时日志配置
		adminGroup.GET("/ops/runtime/logging", guard(rbac.PermOpsView), opsHandler.GetRuntimeLogConfig)
		adminGroup.PUT("/ops/runtime/logging", guard(rbac.PermOpsView), opsHandler.UpdateRuntimeLogConfig)
		adminGroup.POST("/ops/runtime/logging/reset", guard(rbac.PermOpsView), opsHandler.ResetRuntimeLogConfig)

		// 运维看板 v2：Token 统计
		adminGroup.GET("/ops/token-stats", guard(rbac.PermOpsView), opsHandler.GetTokenStats)

		// 运维看板 v2：阈值 & 看板设置
		adminGroup.GET("/ops/settings/metric-thresholds", guard(rbac.PermOpsView), opsHandler.GetMetricThresholds)
		adminGroup.PUT("/ops/settings/metric-thresholds", guard(rbac.PermOpsView), opsHandler.UpdateMetricThresholds)
		adminGroup.GET("/ops/settings/dashboard", guard(rbac.PermOpsView), opsHandler.GetDashboardSettings)
		adminGroup.PUT("/ops/settings/dashboard", guard(rbac.PermOpsView), opsHandler.UpdateDashboardSettings)
		adminGroup.GET("/ops/settings/advanced", guard(rbac.PermOpsView), opsHandler.GetAdvancedSettings)
		adminGroup.PUT("/ops/settings/advanced", guard(rbac.PermOpsView), opsHandler.UpdateAdvancedSettings)

		// 运维看板 v2：用户并发 & 账户可用性 & 实时流量 & 请求错误关联
		adminGroup.GET("/ops/user-concurrency", guard(rbac.PermOpsView), opsHandler.GetUserConcurrency)
		adminGroup.GET("/ops/account-availability", guard(rbac.PermOpsView), opsHandler.GetAccountAvailability)
		adminGroup.GET("/ops/realtime-traffic", guard(rbac.PermOpsView), opsHandler.GetRealtimeTraffic)
		adminGroup.GET("/ops/request-errors/:id/correlation", guard(rbac.PermOpsView), opsHandler.GetRequestErrorCorrelation)
		adminGroup.GET("/ops/upstream-errors/:id", guard(rbac.PermOpsView), opsHandler.GetUpstreamErrorDetail)

		// 运维看板 v2：后台任务
		adminGroup.GET("/ops/jobs", guard(rbac.PermOpsView), opsHandler.ListJobs)

		// 运维看板 v3：邮件通知配置
		adminGroup.GET("/ops/settings/email-notification", guard(rbac.PermOpsView), opsHandler.GetEmailNotificationConfig)
		adminGroup.PUT("/ops/settings/email-notification", guard(rbac.PermOpsView), opsHandler.UpdateEmailNotificationConfig)
		adminGroup.POST("/ops/settings/email-notification/test", guard(rbac.PermOpsView), opsHandler.SendTestAlertEmail)

		// 运维看板 v3：告警运行时设置
		adminGroup.GET("/ops/settings/alert-runtime", guard(rbac.PermOpsView), opsHandler.GetAlertRuntimeSettings)
		adminGroup.PUT("/ops/settings/alert-runtime", guard(rbac.PermOpsView), opsHandler.UpdateAlertRuntimeSettings)

		// 运维看板 v3：错误日志增强视图
		adminGroup.GET("/ops/error-logs", guard(rbac.PermOpsView), opsHandler.ListErrorLogs)
		adminGroup.PUT("/ops/error-logs/:id/resolve", guard(rbac.PermOpsView), opsHandler.ResolveErrorLog)
		adminGroup.PUT("/ops/request-errors/:id/resolve", guard(rbac.PermOpsView), opsHandler.ResolveRequestError)
		adminGroup.PUT("/ops/upstream-errors/:id/resolve", guard(rbac.PermOpsView), opsHandler.ResolveUpstreamError)

		// 视频任务统计（Ops Dashboard 视频指标卡片）
		adminGroup.GET("/ops/video-tasks/stats", guard(rbac.PermUsageViewScoped), videoTaskHandler.Stats)
		adminGroup.GET("/ops/video-tasks", guard(rbac.PermUsageViewScoped), videoTaskHandler.List)
		adminGroup.GET("/ops/video-tasks/:id", guard(rbac.PermUsageViewScoped), videoTaskHandler.Get)

		// 视频任务管理
		adminGroup.GET("/video/tasks/stats/trend", guard(rbac.PermUsageViewScoped), videoTaskHandler.StatsTrend)
		adminGroup.GET("/video/tasks/stats/distribution", guard(rbac.PermUsageViewScoped), videoTaskHandler.StatsDistribution)
		adminGroup.GET("/video/tasks/stats", guard(rbac.PermUsageViewScoped), videoTaskHandler.Stats)
		adminGroup.GET("/video/tasks", guard(rbac.PermUsageViewScoped), videoTaskHandler.List)
		adminGroup.GET("/video/tasks/:id", guard(rbac.PermUsageViewScoped), videoTaskHandler.Get)

		// 模型状态变更日志
		adminGroup.GET("/model-status-logs", guard(rbac.PermModelManage), modelStatusLogHandler.ListModelStatusLogs)

		// 模型 Catalog（注意：source_model_id 含 '/'，通过 query 入口而非路径参数访问）
		adminGroup.GET("/model-catalog/search", catalogHandler.Search)

		// API Key 管理：自助 CRUD（创建/查看/开关/删除/重置/绑定组/access-all-groups）
		// 均按 user_id 隔离，service 层对每个操作做 k.UserID != userID 校验，因此走 JWTAuth 即可，
		// 与 expansion-requests 的自助操作同范式。access-all-groups 对 personal key 是
		// 「访问自己有权的全部组」（entitlement 天花板），不越权。
		// 注意：Service Key 的 access-all-groups 才是管理操作，走 PermServiceKeyRevoke（见 service-keys）。
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

		// OpenCode 配置（token 生成 + 状态轮询，作用于自己的 key）
		adminGroup.POST("/keys/:id/opencode-config/token", opencodeConfigHandler.GenerateToken)
		adminGroup.GET("/keys/:id/opencode-config/status", opencodeConfigHandler.GetStatus)
		adminGroup.GET("/model-catalog/by-source", guard(rbac.PermModelCatalogManage), catalogHandler.GetBySource)
		adminGroup.POST("/model-catalog/sync", guard(rbac.PermModelCatalogManage), catalogHandler.Sync)
		adminGroup.GET("/model-catalog/diff", guard(rbac.PermModelCatalogManage), catalogHandler.Diff)
		adminGroup.POST("/model-catalog/apply-diff", guard(rbac.PermModelCatalogManage), catalogHandler.ApplyDiff)
		adminGroup.POST("/model-catalog/auto-bind", guard(rbac.PermModelCatalogManage), catalogHandler.AutoBind)
		adminGroup.GET("/model-catalog/binding-stats", guard(rbac.PermModelCatalogManage), catalogHandler.BindingStats)
		adminGroup.GET("/model-catalog/sync-status", guard(rbac.PermModelCatalogManage), catalogHandler.SyncStatus)
		// :id（UUID）放最后，避免吃掉前面的静态路径
		adminGroup.GET("/model-catalog/:id", guard(rbac.PermModelCatalogManage), catalogHandler.GetByID)

		// 模型分组管理
		adminGroup.POST("/groups/check-all", guard(rbac.PermGroupManage), groupCheckHandler.CheckAll)
		adminGroup.POST("/groups", guard(rbac.PermGroupManage), groupHandler.Create)
		adminGroup.GET("/groups", groupHandler.List)
		adminGroup.GET("/groups/:id", groupHandler.GetByID)
		adminGroup.PUT("/groups/:id", guard(rbac.PermGroupManage), groupHandler.Update)
		adminGroup.DELETE("/groups/:id", guard(rbac.PermGroupManage), groupHandler.Delete)
		adminGroup.POST("/groups/:id/models", guard(rbac.PermGroupManage), groupHandler.AddModels)
		adminGroup.GET("/groups/:id/models", groupHandler.ListModels)
		adminGroup.DELETE("/groups/:id/models/:model_id", guard(rbac.PermGroupManage), groupHandler.RemoveModel)
		adminGroup.POST("/groups/:id/reset-capability", guard(rbac.PermGroupManage), groupHandler.ResetCapability)
		adminGroup.POST("/groups/:id/check", guard(rbac.PermGroupManage), groupCheckHandler.CheckOne)

		// 系统设置
		adminGroup.GET("/settings", guard(rbac.PermSettingsManage), settingsHandler.GetSystemSettings)
		adminGroup.PUT("/settings", guard(rbac.PermSettingsManage), settingsHandler.UpdateSettings)
		adminGroup.POST("/settings/test-webhook", guard(rbac.PermSettingsManage), settingsHandler.TestWebhook)
		adminGroup.POST("/settings/test-email", guard(rbac.PermSettingsManage), settingsHandler.TestEmail)
		adminGroup.POST("/settings/export", guard(rbac.PermSettingsManage), settingsHandler.ExportSettings)
		adminGroup.POST("/settings/import", guard(rbac.PermSettingsManage), settingsHandler.ImportSettings)

		// 公告管理（读/标记已读对所有登录用户开放，写操作仅 admin）
		adminGroup.GET("/announcements", announcementHandler.List)
		adminGroup.POST("/announcements", guard(rbac.PermAnnouncementManage), announcementHandler.Create)
		adminGroup.GET("/announcements/:id", announcementHandler.GetByID)
		adminGroup.PUT("/announcements/:id", guard(rbac.PermAnnouncementManage), announcementHandler.Update)
		adminGroup.DELETE("/announcements/:id", guard(rbac.PermAnnouncementManage), announcementHandler.Delete)
		adminGroup.PATCH("/announcements/:id/publish", guard(rbac.PermAnnouncementManage), announcementHandler.Publish)
		adminGroup.PATCH("/announcements/:id/archive", guard(rbac.PermAnnouncementManage), announcementHandler.Archive)
		adminGroup.POST("/announcements/:id/read", announcementHandler.MarkRead)
		adminGroup.GET("/announcements/unread", announcementHandler.GetUnread)
		adminGroup.GET("/announcements/unread/count", announcementHandler.GetUnreadCount)
		adminGroup.GET("/announcements/history", announcementHandler.GetReadHistory)

		// 用户反馈（提交对所有登录用户开放，查看与处理仅 admin）
		adminGroup.POST("/feedback", feedbackHandler.Create)
		adminGroup.GET("/feedback", guard(rbac.PermFeedbackManage), feedbackHandler.List)
		adminGroup.PATCH("/feedback/:id", guard(rbac.PermFeedbackManage), feedbackHandler.UpdateStatus)

		// 用户反馈附件（先上传后提交；上传对已登录用户开放）
		adminGroup.POST("/feedback/attachments", feedbackHandler.UploadAttachment)
		// 下载端点的鉴权是「上传者本人 or feedback:manage」，403 必须是资源归属的
		// feedback_attachment_forbidden 而非 org.forbidden，所以这里只构建 Actor 不挂 guard，
		// 判定放在 handler 内（与 Guard 同源，均以服务端 user_roles 为准）。
		adminGroup.GET("/feedback/attachments/:id",
			func(c *gin.Context) { orgContainer.RBAC.BuildAndInjectActor(c) },
			feedbackHandler.DownloadAttachment,
		)

		// 站内通知（系统自动触发，如预算告警）
		adminGroup.GET("/notifications/unread", notificationHandler.GetUnread)
		adminGroup.GET("/notifications/unread/count", notificationHandler.GetUnreadCount)
		adminGroup.GET("/notifications/history", notificationHandler.GetHistory)
		adminGroup.POST("/notifications/:id/read", notificationHandler.MarkRead)
		adminGroup.POST("/notifications/read-all", notificationHandler.MarkAllRead)

		// 用量与成本查询
		usageSvc := obssvc.NewRollupService(db)
		usageH := obshandler.NewUsageHandler(usageSvc, db)
		usageH.SetRedis(rdb)
		// 既有 trace 端点（Trace / MyTrace）的归档正文回填（计划 3/4 任务 6）：
		// 复用同一个点读服务实例（不注入时端点行为与改造前逐字相同）。
		usageH.SetTraceBodyService(tbSvc)
		adminGroup.GET("/usage/logs", guard(rbac.PermUsageViewAll), usageH.Logs)
		adminGroup.GET("/usage/logs/:request_id/trace", guard(rbac.PermUsageViewAll), usageH.Trace)
		adminGroup.GET("/usage/options", usageH.Options)
		adminGroup.GET("/usage", guard(rbac.PermUsageViewAll), usageH.Query)
		adminGroup.GET("/usage-logs", guard(rbac.PermUsageViewAll), usageH.Logs)
		// 个人用量查询（按登录用户 user_id 隔离）
		adminGroup.GET("/my/keys", keyHandler.MyKeys)
		adminGroup.GET("/my/usage", usageH.MyQuery)
		adminGroup.GET("/my/usage/percentiles", usageH.MyPercentiles)
		adminGroup.GET("/my/usage/logs", usageH.MyLogs)
		// 本人记录：MyTrace 会校验归属，他人 request_id 一律 404（避免越权读正文）
		adminGroup.GET("/my/usage/logs/:request_id/trace", usageH.MyTrace)
		adminGroup.GET("/my/video/tasks/stats/trend", videoTaskHandler.MyStatsTrend)
		adminGroup.GET("/my/video/tasks/stats/distribution", videoTaskHandler.MyStatsDistribution)
		adminGroup.GET("/my/video/tasks/stats", videoTaskHandler.MyStats)
		adminGroup.GET("/my/video/tasks", videoTaskHandler.MyList)
		adminGroup.GET("/my/video/tasks/:id", videoTaskHandler.MyGet)
	}

	// 点读两条端点（规格 §8.1；计划 3/4 任务 5）
	// 服务与 Reader 已在 adminGroup 之前构造（任务 6 让既有 trace 端点复用同一实例，
	// 见上文「点读服务与 Reader 的构造」的注释）。
	// 注入既有实现：归属反查与既有 trace 端点同一套判据（LookupTraceSubject），
	// 可见性用既有的 rbac.CanViewPrompt；不新建鉴权机制（规格 §2.2）。
	tbH := obshandler.NewTraceBodyHandler(tbSvc, func(ctx context.Context, requestID string) (string, string, bool) {
		return obshandler.LookupTraceSubject(ctx, db, requestID)
	}, rbac.CanViewPrompt)
	adminGroup.GET("/trace/:request_id/body", guard(rbac.PermUsageViewAll), tbH.TraceBody)

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
		// 点读内部端点（规格 §8.1）：服务间调用，鉴权由本组既有的 InternalAuth 负责。
		internalGroup.GET("/trace-body", tbH.InternalTraceBody)
	}

	// 组织架构服务（org-service）
	authHandler.SetOrgSyncer(keyOrgSyncer)
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

	// 模型组授权管理（注册在 adminGroup，与现有组管理路由同级）
	entitlementHandler := handler.NewEntitlementHandler(entitlementSvc, deptLookup)
	adminGroup.PUT("/groups/:id/open-mode", guard(rbac.PermEntitlementManage), entitlementHandler.SetOpenMode)
	adminGroup.POST("/groups/:id/entitlements", guard(rbac.PermEntitlementGrantDept), entitlementHandler.Grant)
	adminGroup.DELETE("/groups/:id/entitlements/:eid", guard(rbac.PermEntitlementGrantDept), entitlementHandler.Revoke)
	adminGroup.GET("/groups/:id/entitlements", guard(rbac.PermEntitlementGrantDept), entitlementHandler.List)

	// v0.7: Service Key 管理路由
	serviceKeyHandler := handler.NewServiceKeyHandler(keySvc, orgContainer.RBAC)
	serviceKeyHandler.SetAuditRecorder(auditSvc)
	auditHandler := handler.NewAuditLogHandler(auditSvc, orgContainer.RBAC)
	// v0.7: 预算管理路由
	budgetHandler := handler.NewBudgetHandler(budgetSvc, orgContainer.RBAC)
	budgetHandler.SetAlertService(budgetAlertSvc)
	budgetHandler.SetDB(db)
	budgetHandler.SetCounterRebuildService(service.NewBudgetCounterRebuildService(
		db, rdb, budgetperiod.Config{Timezone: cfg.Budget.Timezone, WeekStart: cfg.Budget.WeekStart},
	))
	// 内部端点（供 gateway 调用）：均为机器间调用（verify-key / budget.evaluate-alert / video/tasks），
	// 属请求级流量而非管理面人工操作，不写审计——审计表只记录管理面写操作，避免被内部高频调用刷爆。
	internalGroup.POST("/budget/evaluate-alert", budgetHandler.EvaluateAlert)
	internalGroup.POST("/video/tasks", videoTaskHandler.InternalCreate)
	internalGroup.PATCH("/video/tasks/:id", videoTaskHandler.InternalUpdate)
	internalGroup.PATCH("/video/tasks/:id/status", videoTaskHandler.InternalUpdateStatus)
	internalGroup.GET("/video/tasks/:id/provider-type", videoTaskHandler.InternalGetProviderType)
	// v0.7: 扩容审批路由
	expansionHandler := handler.NewExpansionHandler(expansionSvc, orgContainer.RBAC, expansionUserRepo)
	{
		// Service Key CRUD（部门负责人+admin）
		adminGroup.POST("/service-keys", guard(rbac.PermServiceKeyCreate), serviceKeyHandler.Create)
		adminGroup.GET("/service-keys", guard(rbac.PermServiceKeyRevoke), serviceKeyHandler.List)
		adminGroup.PATCH("/service-keys/:id/status", guard(rbac.PermServiceKeyRevoke), serviceKeyHandler.Toggle)
		adminGroup.PATCH("/service-keys/:id/ip-allowlist", guard(rbac.PermServiceKeyRevoke), serviceKeyHandler.UpdateIPAllowlist)
		adminGroup.POST("/service-keys/:id/groups", guard(rbac.PermServiceKeyRevoke), serviceKeyHandler.UpdateGroups)
		adminGroup.POST("/service-keys/:id/access-all-groups", guard(rbac.PermServiceKeyRevoke), serviceKeyHandler.ToggleAccessAllGroups)
		adminGroup.DELETE("/service-keys/:id", guard(rbac.PermServiceKeyRevoke), serviceKeyHandler.Delete)
		// v0.8: 审计日志查询（仅管理面可查）
		adminGroup.GET("/audit-logs", guard(rbac.PermAuditViewScoped), auditHandler.List)
		// 预算管理（管理面）
		adminGroup.GET("/budgets", guard(rbac.PermBudgetViewScoped), budgetHandler.List)
		adminGroup.PUT("/budgets", guard(rbac.PermBudgetSet), budgetHandler.Set)
		adminGroup.PUT("/budgets/multi-period", guard(rbac.PermBudgetSet), budgetHandler.SetMultiPeriod)
		adminGroup.PUT("/budgets/batch", guard(rbac.PermBudgetSet), budgetHandler.BatchSet)
		adminGroup.POST("/budgets/batch/preview", guard(rbac.PermBudgetSet), budgetHandler.PreviewTotalBatch)
		adminGroup.POST("/budgets/batch/apply", guard(rbac.PermBudgetSet), budgetHandler.ApplyTotalBatch)
		adminGroup.DELETE("/budgets", guard(rbac.PermBudgetSet), budgetHandler.Delete)
		adminGroup.GET("/budgets/context", guard(rbac.PermBudgetViewScoped), budgetHandler.Context)
		adminGroup.POST("/budget/rebuild-counters", guard(rbac.PermBudgetSet), budgetHandler.RebuildCounters)
		adminGroup.GET("/budget/risk-events", guard(rbac.PermBudgetViewScoped), budgetRiskHandler.List)
		adminGroup.GET("/budget/risk-events/summary", guard(rbac.PermBudgetViewScoped), budgetRiskHandler.Summary)
		// 扩容申请：创建/我的申请/流水允许登录用户访问；审批接口继续走 RBAC。
		adminGroup.POST("/expansion-requests/precheck", expansionHandler.Precheck)
		adminGroup.POST("/expansion-requests/notify-owner", expansionHandler.NotifyOwner)
		adminGroup.POST("/expansion-requests", expansionHandler.Create)
		adminGroup.GET("/expansion-requests", expansionHandler.ListMine)
		adminGroup.GET("/expansion-requests/pending", guard(rbac.PermExpansionApprove), expansionHandler.ListPending)
		adminGroup.GET("/expansion-requests/:id", expansionHandler.Get)
		adminGroup.PATCH("/expansion-requests/:id", expansionHandler.Update)
		adminGroup.POST("/expansion-requests/:id/reprecheck", expansionHandler.Reprecheck)
		adminGroup.POST("/expansion-requests/:id/decision", expansionHandler.Decide)
		adminGroup.GET("/expansion-requests/:id/steps", expansionHandler.ListSteps)
		adminGroup.POST("/expansion-requests/batch-decision", guard(rbac.PermExpansionApprove), expansionHandler.BatchDecide)

		// feat/key-approval: Key 创建申请 + 审批
		keyRequestHandler := handler.NewKeyRequestHandler(keyRequestSvc, orgContainer.RBAC, expansionUserRepo)
		adminGroup.GET("/key-requests/policy", keyRequestHandler.CheckPolicy)
		adminGroup.POST("/key-requests", keyRequestHandler.Create)
		adminGroup.GET("/key-requests", keyRequestHandler.ListMine)
		adminGroup.GET("/key-requests/summary", keyRequestHandler.CountMine)
		adminGroup.GET("/key-requests/pending", guard(rbac.PermKeyRequestApprove), keyRequestHandler.ListPending)
		adminGroup.GET("/key-requests/pending/count", guard(rbac.PermKeyRequestApprove), keyRequestHandler.CountPendingForDept)
		adminGroup.GET("/key-requests/history", guard(rbac.PermKeyRequestApprove), keyRequestHandler.ListHistory)
		adminGroup.POST("/key-requests/:id/cancel", keyRequestHandler.CancelMine)
		adminGroup.POST("/key-requests/:id/decision", guard(rbac.PermKeyRequestApprove), keyRequestHandler.Decide)
		// 部门 Key 创建策略配置（读写均走管理角色权限）
		adminGroup.GET("/dept-policies/:dept_id/key-creation", guard(rbac.PermKeyRequestSetPolicy), keyRequestHandler.GetDeptPolicy)
		adminGroup.PUT("/dept-policies/:dept_id/key-creation", guard(rbac.PermKeyRequestSetPolicy), keyRequestHandler.SetDeptPolicy)

		// Channel Monitor 管理路由
		adminGroup.GET("/channel-monitors", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.List)
		adminGroup.POST("/channel-monitors", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.Create)
		adminGroup.GET("/channel-monitors/:id", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.Get)
		adminGroup.PUT("/channel-monitors/:id", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.Update)
		adminGroup.DELETE("/channel-monitors/:id", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.Delete)
		adminGroup.POST("/channel-monitors/:id/run", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.RunCheck)
		adminGroup.GET("/channel-monitors/:id/history", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.GetHistory)
		// Channel Monitor 请求模板
		adminGroup.GET("/channel-monitor-templates", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.ListTemplates)
		adminGroup.POST("/channel-monitor-templates", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.CreateTemplate)
		adminGroup.GET("/channel-monitor-templates/:id", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.GetTemplate)
		adminGroup.PUT("/channel-monitor-templates/:id", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.UpdateTemplate)
		adminGroup.DELETE("/channel-monitor-templates/:id", guard(rbac.PermChannelMonitorManage), channelMonitorHandler.DeleteTemplate)
	}

	// 员工自助查询预算（JWTAuth 即可，不需要 admin）
	r.GET("/api/v1/my/budget", middleware.JWTAuth(), budgetHandler.MyBudget)

	// WebSocket 端点（浏览器 WS 不支持 Authorization header，由 handler 内部验证）
	wsHandler := handler.NewOpsWSHandler(rdb)
	r.GET("/api/v1/ops/ws/qps", wsHandler.HandleQPSWebSocket)

	// v0.15 四层配额体系
	quotaSvc := service.NewQuotaService(db, rdb)
	quotaHandler := handler.NewQuotaHandler(quotaSvc)
	quotaHandler.SetAuditRecorder(auditSvc)
	// 用户配额（管理员设置，ScopeManage = dept_admin/studio_admin/admin 按管辖范围）
	adminGroup.PATCH("/users/:id/quota", guard(rbac.PermQuotaUserSet), quotaHandler.SetUserQuota)
	adminGroup.GET("/users/:id/quota", guard(rbac.PermQuotaUserSet), quotaHandler.GetUserQuota)
	// Service Key 配额
	adminGroup.PATCH("/keys/:id/quota", guard(rbac.PermQuotaServiceKeySet), quotaHandler.SetServiceKeyQuota)
	adminGroup.GET("/keys/:id/quota", guard(rbac.PermQuotaServiceKeySet), quotaHandler.GetServiceKeyQuota)
	// 模型组配额（仅 admin）
	adminGroup.PATCH("/groups/:id/quota", guard(rbac.PermQuotaGroupSet), quotaHandler.SetGroupQuota)
	adminGroup.GET("/groups/:id/quota", guard(rbac.PermQuotaGroupSet), quotaHandler.GetGroupQuota)
	// 模型配额（仅 admin）
	adminGroup.PATCH("/models/:id/quota", guard(rbac.PermQuotaModelSet), quotaHandler.SetModelQuota)
	adminGroup.GET("/models/:id/quota", guard(rbac.PermQuotaModelSet), quotaHandler.GetModelQuota)
	// 我的配额（所有登录用户只读）
	r.GET("/api/v1/me/quota", middleware.JWTAuth(), quotaHandler.GetMyQuota)
	// 紧急豁免（admin only）
	quotaExemptionSvc := service.NewQuotaExemptionService(db, rdb)
	quotaExemptionHandler := handler.NewQuotaExemptionHandler(quotaExemptionSvc)
	quotaExemptionHandler.SetAuditRecorder(auditSvc)
	adminGroup.POST("/quota/exemptions", guard(rbac.PermQuotaDefaultSet), quotaExemptionHandler.Create)
	adminGroup.DELETE("/quota/exemptions/:id", guard(rbac.PermQuotaDefaultSet), quotaExemptionHandler.Revoke)
	// 豁免列表含真实用户姓名 / Service Key 名 / 原因，属敏感数据，仅 admin 可见
	//（与前端 admin-only 入口、Create/Revoke 的 PermQuotaDefaultSet 保持一致）。
	adminGroup.GET("/quota/exemptions", guard(rbac.PermQuotaDefaultSet), quotaExemptionHandler.List)
	// 过期豁免清理定时任务（每小时一次）
	exemptionCleanupCron := cpcron.NewExemptionCleanupCron(quotaExemptionSvc)
	exemptionCleanupCron.Start(context.Background())
	go func() {
		if count, err := exemptionCleanupCron.RunOnce(context.Background()); err != nil {
			slog.Warn("exemption cleanup: initial run failed", "error", err)
		} else if count > 0 {
			slog.Info("exemption cleanup: initial run completed", "cleaned", count)
		}
	}()

	// 内部 snapshot（gateway 拉取）
	internalGroup.GET("/quota/snapshot", func(c *gin.Context) {
		snap, err := quotaSvc.GetQuotaSnapshot(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, snap)
	})

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
		orgGroup.GET("/users", guard(rbac.PermOrgUserSearch), orgH.SearchUsers) // 用户搜索（任意管理角色）

		// ── 角色管理 ─────────────────────────────────────────────────────────
		orgGroup.GET("/all-roles", guard(rbac.PermRoleList), orgH.ListAllRoles)
		orgGroup.POST("/roles/grant", guard(rbac.PermRoleGrant), roleH.GrantRole)
		orgGroup.POST("/roles/revoke", guard(rbac.PermRoleRevoke), roleH.RevokeRole)
		orgGroup.GET("/roles/me", roleH.GetMyRoles) // 当前用户 org 角色（/me 须在 /:user_id 前）
		orgGroup.GET("/roles/:user_id", guard(rbac.PermRoleView), roleH.GetUserRoles)

		// ── 员工花名册导入（仅 admin）────────────────────────────────────────
		orgGroup.POST("/employees/import", guard(rbac.PermEmployeeImport), empImportH.Import)

		// ── 成员部门归属修正（studio_admin 及以上）───────────────────────────
		orgGroup.PUT("/users/:id/department", guard(rbac.PermDeptChange), deptAssignH.ChangeDepartment)
		orgGroup.PUT("/users/:id/department/follow-sso", guard(rbac.PermSSORevert), deptAssignH.FollowSSO)

		// ── 用量看板：dashboard/scoped-logs 需 Guard 注入 Actor ─────────────
		// usageH2 复用同一 DB，仅在此处注册带 Guard 的路由
		usageH2 := obshandler.NewUsageHandler(obssvc.NewRollupService(db), db)
		// ScopedTrace 同样需要归档正文回填（任务 6）：与 usageH 共用同一个点读服务实例。
		usageH2.SetTraceBodyService(tbSvc)
		// 方案X 异常反馈闭环（真人工反馈）：写路径 CRUD 落在 control-plane（anomaly_records 由其 GORM model 管理），
		// 独立于 observability 的只读报告聚合 handler，故单独构造 AnomalyFeedbackHandler（轻量：直接持 *gorm.DB）。
		anomalyFeedbackH := handler.NewAnomalyFeedbackHandler(db)
		adminGroup.GET("/usage/dashboard", guard(rbac.PermUsageViewScoped), usageH2.GetDashboard)
		adminGroup.GET("/usage/dashboard/trend", guard(rbac.PermUsageViewScoped), usageH2.DashboardTrend)
		adminGroup.GET("/usage/dashboard/models", guard(rbac.PermUsageViewScoped), usageH2.DashboardModelsEnhanced)
		adminGroup.GET("/usage/dashboard/users", guard(rbac.PermUsageViewScoped), usageH2.DashboardUsersEnhanced)
		adminGroup.GET("/usage/dashboard/user-top-models", guard(rbac.PermUsageViewScoped), usageH2.DashboardUserTopModels)
		adminGroup.GET("/usage/dashboard/user-detail", guard(rbac.PermUsageViewScoped), usageH2.DashboardUserDetail)
		adminGroup.GET("/usage/dashboard/model-detail", guard(rbac.PermUsageViewScoped), usageH2.DashboardModelDetail)
		adminGroup.GET("/usage/scoped/logs", guard(rbac.PermUsageViewScoped), usageH2.ScopedLogs)
		adminGroup.GET("/usage/scoped/logs/:request_id/trace", guard(rbac.PermUsageViewScoped), usageH2.ScopedTrace)
		// 离线用量报告专用明细：keyset 分页，稳定键(user_id/dept_id/studio_id)过滤，专供 usage-report CLI。
		adminGroup.GET("/usage/report/logs", guard(rbac.PermUsageViewScoped), usageH2.ReportLogs)
		// 离线用量报告专用于期前基线的日聚合+分钟桶聚合（零明细），供 28 天稳健基线物化与 A1/A2 窗口分位重建。
		adminGroup.GET("/usage/report/baseline", guard(rbac.PermUsageViewScoped), usageH2.ReportBaseline)
		// 成员级聚合：按 user_id 返回每人一行的核心单一指标（排除 service），作为「按人统计+逐层横向对比」方案的人工层地基。
		adminGroup.GET("/usage/report/members", guard(rbac.PermUsageViewScoped), usageH2.ReportMembers)
		// 程序 Service Key 聚合：仅 key_type='service'，按 key_id 返回每 Key 一行的核心单一指标（成员+程序对账用）。
		adminGroup.GET("/usage/report/service-keys", guard(rbac.PermUsageViewScoped), usageH2.ReportServiceKeys)
		// 预算执行：按 scope(studio|dept)+month 返回月度配额/实际/百分比（工作室「一页摘要」预算执行行）。
		adminGroup.GET("/usage/report/budget-exec", guard(rbac.PermUsageViewScoped), usageH2.ReportBudgetExec)
		// 方案X 异常反馈闭环（真人工反馈）：批量 upsert 命中异常（默认「待确认」，不覆盖用户反馈态）。
		adminGroup.POST("/usage/report/anomalies", guard(rbac.PermUsageViewScoped), anomalyFeedbackH.ReportAnomalyUpsert)
		// 方案X 异常反馈闭环：读回指定范围+时间窗的真实状态（报告侧覆盖 AnomalyHit.Status）。
		adminGroup.GET("/usage/report/anomaly-status", guard(rbac.PermUsageViewScoped), anomalyFeedbackH.ReportAnomalyStatus)
		// 方案X 异常反馈闭环：人工反馈状态（校验5态枚举，写 status/feedback_at/feedback_by/reason）。
		adminGroup.POST("/usage/report/anomalies/:id/feedback", guard(rbac.PermUsageViewScoped), anomalyFeedbackH.ReportAnomalyFeedback)
		// Token 使用分析 v2：统一 Report JSON、四象限成员点集、待核查反馈。
		reportV2H := obshandler.NewReportHandler(db)
		adminGroup.GET("/usage/analysis-report", guard(rbac.PermUsageViewScoped), reportV2H.Report)
		adminGroup.GET("/usage/analysis-report/members", guard(rbac.PermUsageViewScoped), reportV2H.Members)
		adminGroup.POST("/usage/analysis-report/anomaly/:user_id/feedback", guard(rbac.PermUsageViewScoped), reportV2H.AnomalyFeedback)
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

type deptStudioAdapter struct {
	repo *orgrepo.DeptRepo
}

func (a *deptStudioAdapter) FindStudioIDByDeptID(deptID string) (string, error) {
	dept, err := a.repo.GetByDeptID(deptID)
	if err != nil {
		return "", err
	}
	return dept.GetStudioID(), nil
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
		if raw := settingsSvc.GetOIDCClientSecret(ctx); raw != "" {
			cfg.ClientSecret = raw
		}
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

// EHR_PREFIX 有默认值（路由前缀是接口约定），但地址/凭据/组织三者必须显式配置：
// 只要少一个就不启动同步，避免把生产组织树写进非生产库。
const ehrPrefixDefault = "/open-apis/ehr-apic/open-apis/resource/v1"

// aiAppHubEHRConfigured 三个变量齐全才认为 EHR 已配置。
func aiAppHubEHRConfigured() bool {
	return os.Getenv("EHR_BASE_URL") != "" && os.Getenv("EHR_API_TOKEN") != "" && os.Getenv("EHR_ORG_ID") != ""
}

// newAIAppHubSyncService 构造同步服务。EHR_API_TOKEN 缺失时不注册真实客户端，
// 调用会返回 ErrUpstream，接口上能明确看到"没配 token"，而不是 panic。
func newAIAppHubSyncService(db *gorm.DB, settingsRepo *repository.SettingsRepo) *service.AIAppHubSyncService {
	repo := repository.NewAIAppHubRepo(db)
	// 投影必须接：组织页面 / RBAC / Key 快照读的是老表，不投影同步就白跑。
	projection := orgrepo.NewEHRProjectionRepo(db)
	if !aiAppHubEHRConfigured() {
		return service.NewAIAppHubSyncService(unconfiguredFetcher{}, repo, settingsRepo, service.DefaultSyncRules()).WithProjection(projection)
	}
	fetcher := aiapphub.New(aiapphub.Config{
		BaseURL: os.Getenv("EHR_BASE_URL"),
		Prefix:  envOr("EHR_PREFIX", ehrPrefixDefault),
		Token:   os.Getenv("EHR_API_TOKEN"),
		OrgID:   os.Getenv("EHR_ORG_ID"),
	})
	return service.NewAIAppHubSyncService(fetcher, repo, settingsRepo, service.DefaultSyncRules()).WithProjection(projection)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type unconfiguredFetcher struct{}

func (unconfiguredFetcher) FetchDepartments(context.Context) ([]aiapphub.Department, error) {
	return nil, fmt.Errorf("%w: EHR_BASE_URL/EHR_API_TOKEN/EHR_ORG_ID 未配置", aiapphub.ErrUpstream)
}
