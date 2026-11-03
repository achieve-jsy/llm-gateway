package server

// 本文件是「媒体链路」的路由接缝（A7），**当前只有骨架**：
//   - registerMediaImageRoutes：图像同步链路（B 队）—— /v1/images/generations、/v1/images/edits
//   - registerMediaTaskRoutes：任务平台链路（C 队）—— /v1/videos 三件套
//
// 为什么单独一个文件：B 队（图像）与 C 队（任务平台）是 Batch 1 的两条并行线，必须
// 「不共享文件」。router.go 由 A 队保持稳定（只新增本文件两个函数的调用点），
// 两条链路各自的注册代码分别写在本文件的两个函数里，互不重叠。
//
// 设计依据：docs/media-channels-integration-design.md §2（总体架构）、§6.3（路由与处理流程）。

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/infra"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/gin-gonic/gin"
)

// MediaDeps 汇总媒体两条链路注册与处理所需的依赖。
//
// 约定：
//   - 所有字段都是「可缺省」的：未配置时实现方必须自行降级（跳过或返回明确错误），
//     不得因为某个依赖为 nil 而在启动期 panic；
//   - 本结构由 router.go 的 newMediaDeps 一次性构造；**B/C 队需要新依赖时只改本结构与
//     newMediaDeps**，不要把新的参数加回 Setup()/setup()（那会牵动 router.go 与 cmd/gateway）。
type MediaDeps struct {
	// V1 是已挂载鉴权/埋点/配额中间件的 /v1 路由组。
	// 为 nil 表示路由尚未就绪，注册函数必须直接返回（不能 panic）。
	V1 *gin.RouterGroup

	// RouterGetter 每次调用返回最新的 providers.Router（热更新安全）。
	// 用于「模型 → provider 类型/实例」的解析。为 nil 时表示未启用。
	RouterGetter func() *providers.Router

	// GroupDispatch 模型组调度器（组名 → 上游模型 + 渠道类型）。
	// 为 nil 表示未启用模型组（此时直连模型 ID 语义未定，实现方应拒绝或按既有范式处理）。
	GroupDispatch *orchestrator.GroupAwareDispatcher

	// ControlPlaneURL / InternalToken 控制面内部 API 地址与凭证。
	// **gateway 不直连数据库**：任务读写一律走**既有**内部端点
	// `/internal/v1/video/tasks`（防护栏 A1）。冻结契约刻意复用该路径而不是新建
	// `/internal/v1/media/tasks`，避免滚动发布窗口内新旧控制面不匹配 → 404；
	// tests/contract/contracts.json 里写的 media-tasks 是早期设计稿，以冻结契约为准。
	ControlPlaneURL string
	InternalToken   string

	// RDB 可选的 Redis 客户端（任务热缓存 / 配额）。为 nil 表示未配置 Redis。
	RDB redisClient

	// Collector 埋点采集器（用量与成本）。为 nil 时实现方跳过埋点，不得 panic。
	Collector *gwobs.Collector

	// RequestQuotaGate 请求层配额闸门（四层配额的主体层/组层），
	// 与 BudgetGate、ModelQuotaGate 一起构成设计文档 §2 的 PrepareAuthorizedGates（预算+请求配额+模型配额）。
	RequestQuotaGate handler.RequestQuotaGate

	// ModelQuotaGate 模型层配额闸门（路由选定具体 model 之后、转发上游之前）。
	ModelQuotaGate *quota.ModelQuotaGate

	// BudgetGate 预算闸门。为 nil 表示未启用预算。
	BudgetGate *budget.Gate

	// MediaAdapterRegistry 异步媒体任务适配器注册表（A9 提供）。
	// C 队在 provider snapshot 热更新（config:change）时对它调用 Swap（整表原子替换）。
	// 未接线时 Get 返回 nil（注册表自身 nil-safe），实现方据此返回 501。
	MediaAdapterRegistry *providers.MediaAdapterRegistry
}

// mediaAdapterRegistry 是进程内异步媒体适配器注册表的包级单例。
//
// 之所以用包级单例而不是把它塞进 Setup() 的参数：provider snapshot 的热更新回调挂在
// cmd/gateway（`onSeedanceReloadReady` 范式），C 队需要在**不改 router.go 签名**的前提下
// 拿到同一个注册表实例做 Swap。对外通过 MediaAdapterRegistry() 暴露。
var mediaAdapterRegistry = providers.NewMediaAdapterRegistry()

// MediaAdapterRegistry 返回进程内异步媒体适配器注册表。
//
// 供 cmd/gateway 在 provider snapshot 热更新时调用 Swap：先按新 snapshot 构造整张
// `map[providerType]core.MediaTaskProvider`，再一次性替换（不要增量改表）。
// 未接线时所有方法安全降级（Get 返回 nil）。
func MediaAdapterRegistry() *providers.MediaAdapterRegistry {
	return mediaAdapterRegistry
}

// newMediaDeps 把 setup() 中已构造好的依赖汇总成 MediaDeps（由 router.go 一行调用）。
//
// 这样 router.go 永远只有「构造 deps + 调用两个注册函数」这几行：
// 后续 B/C 队需要新增依赖时，只改 MediaDeps 字段与本函数，不需要再动 router.go。
func newMediaDeps(
	v1 *gin.RouterGroup,
	routerGetter func() *providers.Router,
	groupDispatch *orchestrator.GroupAwareDispatcher,
	controlPlaneURL, internalToken string,
	rdb redisClient,
	collector *gwobs.Collector,
	requestQuotaGate handler.RequestQuotaGate,
	modelQuotaGate *quota.ModelQuotaGate,
	budgetGate *budget.Gate,
) MediaDeps {
	return MediaDeps{
		V1:                   v1,
		RouterGetter:         routerGetter,
		GroupDispatch:        groupDispatch,
		ControlPlaneURL:      controlPlaneURL,
		InternalToken:        internalToken,
		RDB:                  rdb,
		Collector:            collector,
		RequestQuotaGate:     requestQuotaGate,
		ModelQuotaGate:       modelQuotaGate,
		BudgetGate:           budgetGate,
		MediaAdapterRegistry: mediaAdapterRegistry,
	}
}

// registerMediaImageRoutes 注册「图像同步链路」路由（设计文档 §2.1、§6.3）。
//
// 负责人：B 队（Batch 1）。**当前为空实现**，只登记待办清单，不得在此写业务逻辑。
//
// 待注册（B2/B3）：
//
//	POST /v1/images/generations  → image_handler.go 的生成入口（JSON）
//	POST /v1/images/edits        → image_handler.go 的编辑入口（multipart/form-data）
//
// 实现时必须遵守：
//   - 路由挂在 deps.V1 上（已含 Auth / BodyBudgetLimiter / BillingFactGuard / TraceCapture /
//     UsageCollector / OrgCheck 中间件链），不要另建路由组绕开它们；
//   - 先做 `if deps.V1 == nil { return }` 之类的降级判断；
//   - handler 需通过 deps.RouterGetter 解析「模型 → provider」，并复用
//     deps.GroupDispatch 做「组名 → 上游模型」改写（B2）；直连模型 ID 返回 403；
//   - 不支持的参数（generations 的 response_format / style）返回 400 并提示替代项（B5）。
func registerMediaImageRoutes(deps MediaDeps) {
	if deps.V1 == nil {
		return
	}
	imageHandler := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(deps.GroupDispatch),
		handler.WithImageModelResolver(handler.RouterImageModelResolver(deps.RouterGetter)),
	)
	// 挂在 deps.V1 上，沿用既有中间件链（Auth / BodyBudgetLimiter / BillingFactGuard /
	// TraceCapture / UsageCollector / OrgCheck）：不另建路由组，避免绕开鉴权与埋点。
	//
	// TraceCapture 的采集路径白名单（middleware.traceCapturePaths）**不含** /v1/images/*：
	// multipart 与 base64 正文一律不落 call_traces（B4）；DEBUG 级 RequestBodyLogger
	// 也对本路径跳过（见 middleware/middleware.go）。
	deps.V1.POST("/images/generations", imageHandler.HandleImageGeneration)
	deps.V1.POST("/images/edits", imageHandler.HandleImageEdit)
}

// registerMediaTaskRoutes 注册「任务平台链路」路由（设计文档 §2.1、§6.3）。
//
// 负责人：C 队（Batch 1）。
//
// 注册（C5）：
//
//	POST /v1/videos                  → media_task_handler.go 创建任务
//	GET  /v1/videos/:task_id         → 查询任务（归属校验 + 终态缓存 + CAS + 记账）
//	GET  /v1/videos/:task_id/content → 产物（Phase 1-4 为 302 直跳上游，Phase 5 改代理）
//
// 实现约束（逐条对应任务书）：
//   - 路由挂在 deps.V1 上，沿用既有中间件链（鉴权/埋点/配额/预算）；
//   - 创建流程先过 PrepareAuthorizedGates（deps.BudgetGate + deps.RequestQuotaGate +
//     deps.ModelQuotaGate），组调度走 deps.GroupDispatch；
//   - 任务读写走控制面内部 API（deps.ControlPlaneURL / deps.InternalToken），
//     **gateway 禁止直连数据库**（防护栏 A1）；写失败必须返回 5xx，禁止 fire-and-forget；
//   - 适配器从 deps.MediaAdapterRegistry.Get(providerType) 取（可能为 nil → 501），
//     热更新由 MediaAdapterRegistry() 在 config:change 时 Swap（C4）；
//   - 与既有 Seedance 路由 /v1/contents/generations/tasks（router.go）路径前缀不同，
//     两条链路**并存**、不互相顶替（D1 决策）。
func registerMediaTaskRoutes(deps MediaDeps) {
	if deps.V1 == nil {
		slog.Warn("media task routes not registered: /v1 group is nil")
		return
	}

	policy := currentMediaTaskPolicy()
	timeout := time.Duration(policy.TimeoutMinutes) * time.Minute

	// 只在配置了控制面地址时才注入持久化客户端：否则 handler 返回 503（未接线），
	// 而不是伪装成「控制面 502」。
	var store handler.MediaTaskStore
	if deps.ControlPlaneURL != "" {
		store = infra.NewVideoTaskClient(deps.ControlPlaneURL, deps.InternalToken)
	}

	// 注意：把「值为 nil 的具体类型指针」赋给接口会得到**非 nil 接口**，
	// 调用时直接 panic。必须先判空再赋值。
	var dispatcher handler.VideoGroupDispatcher
	if deps.GroupDispatch != nil {
		dispatcher = deps.GroupDispatch
	}

	h := handler.NewMediaTaskHandler(handler.MediaTaskHandlerConfig{
		Store:           store,
		Registry:        deps.MediaAdapterRegistry,
		GroupDispatch:   dispatcher,
		RouterGetter:    deps.RouterGetter,
		BudgetGate:      deps.BudgetGate,
		RequestQuota:    deps.RequestQuotaGate,
		ModelQuotaGate:  deps.ModelQuotaGate,
		Pricing:         policy.PricingResolver,
		Timeout:         timeout,
		MaxPollFailures: policy.PollMaxFailures,
	})

	deps.V1.POST("/videos", h.Create)
	deps.V1.GET("/videos/:task_id", h.Retrieve)
	deps.V1.GET("/videos/:task_id/content", h.Content)

	slog.Info("media task routes registered",
		"paths", []string{"/v1/videos", "/v1/videos/:task_id", "/v1/videos/:task_id/content"},
		"media_adapters", deps.MediaAdapterRegistry.Len(),
		"timeout", timeout.String(),
		"poll_max_failures", policy.PollMaxFailures,
	)
}

// RegisterMediaTaskRoutes 是 registerMediaTaskRoutes 的导出入口，仅供 tests/ 下的
// 组件测试在最小 gin 引擎上验证「注册结果与整体接线」（生产路径由 setup() 调用
// 未导出的 registerMediaTaskRoutes，两者是同一个函数、不存在行为差异）。
func RegisterMediaTaskRoutes(deps MediaDeps) {
	registerMediaTaskRoutes(deps)
}

// ════════════════════════════════════════════════════════════════════════════
// C4：异步媒体适配器注册表的接线（整表原子替换 + config:change 热更新）
// ════════════════════════════════════════════════════════════════════════════

// MediaAdapterFactory 把 provider snapshot entries 编译成**整张**
// 「providerType → 适配器」表（以及展示名表）。
//
// 为什么用工厂注册而不是在 routes_media.go 里 switch providerType：
// 每个渠道适配器由 D 队在自己的包内实现（Vidu/Kling…），C 队不做渠道翻译层。
// 工厂由渠道侧（或 cmd/gateway）在启动期注册一次，C 队只负责「按 snapshot 整表重建
// + 一次性 Swap + 未接线时 nil → 501」这条链路。
//
// 返回 nil/nil 表示本次 snapshot 没有任何可用的媒体渠道 —— Swap 会把注册表清空
// （渠道被删除/停用时必须真的消失，否则会继续用旧凭证打上游）。
type MediaAdapterFactory func(entries []gwmodel.ProviderEntry) (map[string]core.MediaTaskProvider, map[string]string)

var mediaAdapterFactory atomic.Pointer[MediaAdapterFactory]

// RegisterMediaAdapterFactory 注册整表构建器（传 nil 表示取消注册）。
func RegisterMediaAdapterFactory(factory MediaAdapterFactory) {
	if factory == nil {
		mediaAdapterFactory.Store(nil)
		return
	}
	mediaAdapterFactory.Store(&factory)
}

// ReloadMediaAdapters 按 provider snapshot 重建整张适配器表并**一次性 Swap**。
//
// 由 cmd/gateway 挂在既有的 provider 热更新链路（ProviderLoader 的 onReload 回调，
// 与 seedance 适配器同一条 config:change 链路）上，改渠道后无需重启 gateway。
//
// 语义：**整表替换，不是增量 merge**（见 MediaAdapterRegistry.Swap 的注释）。
func ReloadMediaAdapters(entries []gwmodel.ProviderEntry) {
	var adapters map[string]core.MediaTaskProvider
	var names map[string]string
	if factory := mediaAdapterFactory.Load(); factory != nil && *factory != nil {
		adapters, names = (*factory)(entries)
	}
	mediaAdapterRegistry.Swap(adapters, names)
	slog.Info("media adapters reloaded",
		"count", mediaAdapterRegistry.Len(),
		"provider_types", mediaAdapterRegistry.ProviderTypes(),
	)
}

// ════════════════════════════════════════════════════════════════════════════
// C7/C8：任务平台的运行策略（超时/轮询上限/价表）
// ════════════════════════════════════════════════════════════════════════════

// MediaTaskPolicy 是任务平台的运行策略，由 cmd/gateway 在启动期注入一次。
//
// 为什么不走 MediaDeps 字段：router.go 的 newMediaDeps 签名由 A 队锁定（B/C 队
// 不得改动 setup()），而这两个值来自配置（MEDIA_TASK_TIMEOUT_MINUTES /
// MEDIA_POLL_MAX_FAILURES），启动期才有。这里用与 MediaAdapterRegistry() 同样的
// 「包级单例 + 显式注入」范式。
type MediaTaskPolicy struct {
	// TimeoutMinutes 任务超时阈值（分钟；<=0 时回退 30）。
	TimeoutMinutes int
	// PollMaxFailures 轮询失败上限（<=0 时回退 20）。
	PollMaxFailures int
	// PricingResolver 媒体价表解析器（与准入闸门同源；可为 nil → 记不上成本）。
	PricingResolver gwobs.PricingResolver
}

var mediaTaskPolicyHolder atomic.Pointer[MediaTaskPolicy]

// ConfigureMediaTaskPolicy 注入任务平台运行策略（启动期调用一次；测试可直接调用）。
func ConfigureMediaTaskPolicy(policy MediaTaskPolicy) {
	mediaTaskPolicyHolder.Store(&policy)
}

// MediaTaskPolicyDefaults 返回未注入策略时的兜底值（与 gateway 配置默认值一致）。
//
// 导出是为了让启动自检与组件测试能断言「代码默认值 == 配置默认值」，避免两处漂移。
func MediaTaskPolicyDefaults() MediaTaskPolicy {
	return MediaTaskPolicy{TimeoutMinutes: 30, PollMaxFailures: 20}
}

// currentMediaTaskPolicy 读取策略并做安全兜底（未注入时用与配置默认值一致的 30/20）。
func currentMediaTaskPolicy() MediaTaskPolicy {
	policy := MediaTaskPolicy{TimeoutMinutes: 30, PollMaxFailures: 20}
	if stored := mediaTaskPolicyHolder.Load(); stored != nil {
		policy = *stored
	}
	if policy.TimeoutMinutes <= 0 {
		policy.TimeoutMinutes = 30
	}
	if policy.PollMaxFailures <= 0 {
		policy.PollMaxFailures = 20
	}
	return policy
}
