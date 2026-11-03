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
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/handler"
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
	// **gateway 不直连数据库**：任务读写一律走 /internal/v1/media-tasks（防护栏 A1）。
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
// 负责人：C 队（Batch 1）。**当前为空实现**，只登记待办清单，不得在此写业务逻辑。
//
// 待注册（C5）：
//
//	POST /v1/videos                 → media_task_handler.go 创建任务
//	GET  /v1/videos/:task_id        → 查询任务（归属校验 + 终态缓存 + CAS + 记账）
//	GET  /v1/videos/:task_id/content→ 产物（Phase 1-4 为 302 直跳上游，Phase 5 改代理）
//
// 实现时必须遵守：
//   - 路由挂在 deps.V1 上，沿用既有中间件链（鉴权/埋点/配额/预算）；
//   - 先做 `if deps.V1 == nil { return }` 之类的降级判断；
//   - 创建流程先过 PrepareAuthorizedGates（deps.BudgetGate + deps.RequestQuotaGate +
//     deps.ModelQuotaGate），组调度走 deps.GroupDispatch；
//   - 任务读写走控制面内部 API（deps.ControlPlaneURL / deps.InternalToken），
//     **gateway 禁止直连数据库**（防护栏 A1）；写失败必须返回 5xx，禁止 fire-and-forget；
//   - 适配器从 deps.MediaAdapterRegistry.Get(providerType) 取（可能为 nil → 501），
//     热更新由 MediaAdapterRegistry() 在 config:change 时 Swap（C4）；
//   - 注意与既有 Seedance 路由 /v1/contents/generations/tasks（router.go）并存，不要互相顶替。
func registerMediaTaskRoutes(deps MediaDeps) {
	// TODO(C 队 · C5/C6): 见上方待注册清单与约束。
}
