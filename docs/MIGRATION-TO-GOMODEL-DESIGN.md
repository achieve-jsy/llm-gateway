# LLM Gateway 迁移至 GoModel 架构设计方案

> 版本: v1.1  
> 日期: 2026-05-28  
> 状态: Draft  
> 变更: v1.1 - 保留 Gin 框架，不切换 Echo v5

---

## 一、迁移目标

将当前 llm-gateway 的**网关转发内核**整体切换为 GoModel 的设计模式，同时保留我们自身的差异化能力。最终达成：

| 采用 GoModel 的能力 | 保留我们自己的能力 |
|--------------------|--------------------|
| Provider 接口 + 工厂模式 | Model Group 调度（RR/Priority/Weighted） |
| LLMClient（重试 + 指数退避 + 熔断器） | 主动健康探测 + 健康路由 |
| Router（多优先级模型解析） | Redis Pub/Sub 热配置推送 |
| 零拷贝 SSE 流代理（Observer 模式） | Control Plane 独立微服务 |
| Fallback 智能降级（手动链 + 自动发现） | OTel 分布式追踪 |
| Workflow/Policy 引擎 | API Key + 分组绑定权限 |
| Guardrail 防护栏管道 | 身份注入（Model Identity） |
| 响应缓存（精确 + 语义） | Vue 3 运维控制台 |
| Budget/Usage 成本管控 | GatewayGuard（限流/并发控制） |
| Passthrough 直通代理 | LiteLLM Model Catalog 同步 |
| Audit Log 审计日志 | 内部 API 通信机制 |

---

## 二、架构全景

### 2.1 迁移后目标架构

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         LLM Gateway System (v2)                              │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐  │
│  │                    Gateway Service (port 8080)                          │  │
│  │                                                                        │  │
│  │  ┌──── Gin HTTP Server (保留现有框架) ─────────────────────────┐   │  │
│  │  │                                                                 │   │  │
│  │  │  Middleware Stack:                                              │   │  │
│  │  │    RequestID → OTel Tracing(ours) → Logger → Recover           │   │  │
│  │  │    → BodyLimit → RequestSnapshot → AuditLog(GoModel logic)     │   │  │
│  │  │    → Auth(ours: sk-key + JWT) → GatewayGuard(ours: RPM+Conc)  │   │  │
│  │  │    → Workflow Resolution(GoModel logic)                         │   │  │
│  │  │                                                                 │   │  │
│  │  │  Routes:                                                        │   │  │
│  │  │    /v1/chat/completions  → ChatHandler                          │   │  │
│  │  │    /v1/responses         → ResponsesHandler                     │   │  │
│  │  │    /v1/embeddings        → EmbeddingsHandler                    │   │  │
│  │  │    /v1/models            → ModelsHandler                        │   │  │
│  │  │    /v1/messages          → AnthropicHandler                     │   │  │
│  │  │    /p/:provider/*        → PassthroughHandler (GoModel logic)   │   │  │
│  │  │    /health, /ready       → HealthHandler                        │   │  │
│  │  │    /metrics              → PrometheusHandler                    │   │  │
│  │  └────────────────────────────────────────────────────────────────┘   │  │
│  │                                                                        │  │
│  │  ┌──── Core Engine (GoModel-based) ───────────────────────────────┐   │  │
│  │  │                                                                 │   │  │
│  │  │  ┌─────────────┐    ┌─────────────────┐   ┌───────────────┐   │   │  │
│  │  │  │ Provider     │    │ GroupRouter     │   │ Fallback      │   │   │  │
│  │  │  │ Factory      │    │ (ours+GoModel)  │   │ Resolver      │   │   │  │
│  │  │  │ (GoModel)    │    │                 │   │ (GoModel)     │   │   │  │
│  │  │  └──────┬───────┘    └────────┬────────┘   └───────┬───────┘   │   │  │
│  │  │         │                     │                     │           │   │  │
│  │  │  ┌──────▼─────────────────────▼─────────────────────▼───────┐   │   │  │
│  │  │  │              LLMClient (GoModel)                          │   │   │  │
│  │  │  │  • Exponential Backoff + Jitter Retry                     │   │   │  │
│  │  │  │  • 3-State Circuit Breaker                                │   │   │  │
│  │  │  │  • OTel Hooks (ours)                                      │   │   │  │
│  │  │  └──────────────────────────────────────────────────────────┘   │   │  │
│  │  │                                                                 │   │  │
│  │  │  ┌──────────────────────────────────────────────────────────┐   │   │  │
│  │  │  │  Streaming: ObservedSSEStream (GoModel zero-copy)         │   │   │  │
│  │  │  │  + OTel Observer (ours) + Usage Observer (GoModel)        │   │   │  │
│  │  │  └──────────────────────────────────────────────────────────┘   │   │  │
│  │  │                                                                 │   │  │
│  │  │  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────┐   │   │  │
│  │  │  │ Guardrails  │  │ ResponseCache│  │ Budget/Usage         │   │   │  │
│  │  │  │ (GoModel)   │  │ (GoModel)    │  │ (GoModel)            │   │   │  │
│  │  │  └─────────────┘  └──────────────┘  └──────────────────────┘   │   │  │
│  │  └────────────────────────────────────────────────────────────────┘   │  │
│  │                                                                        │  │
│  │  ┌──── Our Extensions ────────────────────────────────────────────┐   │  │
│  │  │  • GroupScheduler (RR/Priority/Weighted + HealthStore)          │   │  │
│  │  │  • HealthProber (active HTTP probing every 30s)                 │   │  │
│  │  │  • ConfigSubscriber (Redis Pub/Sub → hot reload)                │   │  │
│  │  │  • ModelIdentityInjector (system prompt injection)              │   │  │
│  │  │  • KeyGroupBinding (API key → group ACL)                        │   │  │
│  │  └────────────────────────────────────────────────────────────────┘   │  │
│  └────────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐  │
│  │                Control Plane Service (port 8081)  [保留]                │  │
│  │  • Provider/Model/Group CRUD                                           │  │
│  │  • API Key Management + Key-Group Binding                              │  │
│  │  • Model Catalog Sync (LiteLLM)                                        │  │
│  │  • System Settings                                                     │  │
│  │  • Config Change → Redis Pub/Sub → Gateway Reload                      │  │
│  │  • Health Check Scheduler                                              │  │
│  └────────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
│  ┌──────────┐   ┌──────────────┐   ┌──────────────┐   ┌───────────────┐    │
│  │  Redis   │   │ PostgreSQL   │   │ OTel→Jaeger  │   │ Vue3 Console  │    │
│  └──────────┘   └──────────────┘   └──────────────┘   └───────────────┘    │
└──────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 关键设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| Web 框架 | **保留 Gin** | GoModel 核心逻辑零依赖 HTTP 框架，Echo 仅在表现层；保留 Gin 避免无谓迁移成本，团队已熟悉 |
| Provider 接口 | 采用 GoModel `core.Provider` | 支持更多 API 类型（Responses/Batch/File） |
| HTTP 客户端 | 采用 GoModel `llmclient` | 内置重试+熔断，生产验证 |
| 流式处理 | 采用 GoModel `ObservedSSEStream` | 零拷贝，GC 友好 |
| 路由模型 | GoModel Router + 我们的 GroupRouter 叠加 | 兼顾直连路由和分组调度 |
| 存储后端 | 保留 PostgreSQL + 新增可选 SQLite | 兼容现有部署，开发环境简化 |
| 配置同步 | 保留 Redis Pub/Sub 机制 | 实时性优于 GoModel 的手动刷新 |
| 认证 | 保留我们的多层 Auth | GoModel 的 master key 模式太简单 |
| 数据模型 | 扩展为 GoModel 的 `core.ChatRequest` 格式 | 支持 tool_calls, reasoning, 等完整字段 |

### 2.3 保留 Gin 的可行性分析

经过对 GoModel 源码的深入分析，其架构分层如下：

```
GoModel 代码依赖图:

  ┌─────────────────────────────────────────────────────┐
  │  HTTP 表现层 (依赖 Echo v5)                          │
  │  • internal/server (26 files)                       │
  │  • internal/admin (15 files)                        │
  │  • internal/responsecache/middleware (5 files)      │  ← 仅此层依赖框架
  │  • internal/auditlog/middleware (2 files)           │
  └────────────────────────┬────────────────────────────┘
                           │ 调用
  ┌────────────────────────▼────────────────────────────┐
  │  业务逻辑层 (零框架依赖, 纯 context.Context + 接口)   │
  │  • internal/core          (接口 + 类型)              │
  │  • internal/providers     (15 个 Provider adapter)   │
  │  • internal/llmclient     (HTTP 客户端 + 重试 + 熔断) │
  │  • internal/streaming     (SSE 零拷贝观察者)          │
  │  • internal/fallback      (智能降级)                 │
  │  • internal/workflows     (Policy 引擎)              │  ← 我们直接移植
  │  • internal/guardrails    (防护栏管道)                │
  │  • internal/budget        (预算管控)                  │
  │  • internal/usage         (用量追踪)                  │
  │  • internal/cache         (缓存抽象)                  │
  │  • ... (12+ more packages)                          │
  └─────────────────────────────────────────────────────┘
```

**结论**：GoModel 369 个生产文件中，仅 49 个（13%）依赖 Echo。所有我们需要移植的核心能力包（providers, llmclient, streaming, fallback, workflows, guardrails, budget, usage）**完全不依赖任何 HTTP 框架**。它们通过 `context.Context`、接口和标准库 `io.ReadCloser` 交互。

因此保留 Gin 只需在表现层做适配：
- GoModel 的 `echo.HandlerFunc` → 改写为 `gin.HandlerFunc`
- GoModel 的 `echo.MiddlewareFunc` → 改写为 `gin.HandlerFunc` + `c.Next()`
- 两者的 Context API 高度相似（`c.JSON`, `c.Param`, `c.Bind`, `c.Writer` 几乎 1:1）

**适配模式示例：**

```go
// GoModel 中间件 (Echo 模式)
func AuditMiddleware(cfg Config) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c *echo.Context) error {
            // capture request...
            err := next(c)
            // log audit...
            return err
        }
    }
}

// 我们的适配 (Gin 模式) - 逻辑完全相同
func AuditMiddleware(cfg Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        // capture request... (同样的逻辑)
        c.Next()
        // log audit... (同样的逻辑)
    }
}
```

---

## 三、核心模块设计

### 3.1 目录结构（迁移后）

```
internal/
├── core/                          # [NEW] 从 GoModel 移植, 核心接口和类型
│   ├── interfaces.go              # Provider, RoutableProvider, ModelLookup
│   ├── types.go                   # ChatRequest/Response, Message, Usage...
│   ├── model_selector.go          # ModelSelector 解析
│   ├── workflow.go                # ExecutionMode, Workflow, CapabilitySet
│   ├── context.go                 # Request-scoped context helpers
│   └── errors.go                  # GatewayError typed errors
│
├── llmclient/                     # [NEW] 从 GoModel 移植, HTTP 客户端
│   ├── client.go                  # Do/DoRaw/DoStream/DoPassthrough + retry
│   ├── circuit_breaker.go         # 3-state circuit breaker
│   ├── config.go                  # Client config (timeouts, retry params)
│   └── hooks.go                   # OnRequestStart/End hooks (OTel integration)
│
├── providers/                     # [NEW] 从 GoModel 移植, Provider 实现
│   ├── factory.go                 # ProviderFactory + Registration
│   ├── router.go                  # Router (model resolution + routing)
│   ├── registry.go                # ModelRegistry (model lookup)
│   ├── openai/                    # OpenAI adapter
│   ├── anthropic/                 # Anthropic adapter
│   ├── azure/                     # Azure OpenAI adapter
│   ├── bedrock/                   # AWS Bedrock adapter
│   ├── deepseek/                  # DeepSeek adapter
│   ├── gemini/                    # Google Gemini adapter
│   ├── vertex/                    # Google Vertex AI adapter
│   ├── groq/                      # Groq adapter
│   ├── minimax/                   # MiniMax adapter
│   ├── ollama/                    # Ollama adapter (self-hosted)
│   ├── vllm/                      # vLLM adapter (self-hosted)
│   ├── xai/                       # xAI adapter
│   ├── openrouter/                # OpenRouter adapter
│   ├── bailian/                   # [KEEP] 阿里百炼 adapter (我们特有)
│   └── litellm/                   # [KEEP] LiteLLM fallback adapter
│
├── streaming/                     # [NEW] 从 GoModel 移植
│   ├── observed_sse_stream.go     # Zero-copy SSE observer
│   ├── stream_buffer.go           # Pool-backed byte buffer
│   └── otel_observer.go           # [OUR] OTel stream observer
│
├── fallback/                      # [NEW] 从 GoModel 移植
│   ├── resolver.go                # Manual + Auto fallback
│   └── ranking.go                 # Chatbot Arena ranking data
│
├── guardrails/                    # [NEW] 从 GoModel 移植
│   ├── pipeline.go                # Ordered parallel/sequential execution
│   ├── system_prompt.go           # inject/override/decorator modes
│   └── llm_based_altering.go     # LLM-based content rewriting/PII
│
├── responsecache/                 # [NEW] 从 GoModel 移植
│   ├── simple.go                  # Redis exact-match cache
│   ├── semantic.go                # Vector similarity cache
│   └── middleware.go              # Cache hit/miss middleware
│
├── budget/                        # [NEW] 从 GoModel 移植
│   ├── types.go                   # Budget periods, limits
│   └── service.go                 # Per-user-path budget enforcement
│
├── usage/                         # [NEW] 从 GoModel 移植
│   ├── tracker.go                 # Token usage tracking
│   └── pricing.go                 # Cost calculation
│
├── auditlog/                      # [NEW] 从 GoModel 移植
│   ├── logger.go                  # Async audit logging
│   └── middleware.go              # Request/response capture
│
├── workflows/                     # [NEW] 从 GoModel 移植
│   ├── types.go                   # Scope, Payload, FeatureFlags
│   ├── store.go                   # Store interface
│   ├── service.go                 # Match + hierarchical resolution
│   └── compiler.go                # Workflow → Policy compilation
│
├── gateway/                       # [REFACTORED] 保留我们的差异化层
│   ├── server/                    # [KEEP] 保留 Gin 框架
│   │   ├── server.go             # Server lifecycle (Gin Engine)
│   │   ├── routes.go             # Route registration (Gin RouterGroup)
│   │   └── handler.go            # Handler implementations (delegates to router)
│   │
│   ├── grouprouter/              # [KEEP] 我们的分组路由增强
│   │   ├── group_router.go       # GroupRouter (wraps GoModel Router)
│   │   ├── scheduler.go          # RR/Priority/Weighted scheduling
│   │   ├── health_store.go       # Health status tracking
│   │   ├── health_prober.go      # Active HTTP probing
│   │   ├── failover.go           # Group-internal failover
│   │   └── identity_injector.go  # Model identity system prompt
│   │
│   ├── middleware/               # [MERGED] GoModel 逻辑 + Our MW (Gin HandlerFunc)
│   │   ├── request_id.go         # (保留现有)
│   │   ├── recover.go            # (保留现有)
│   │   ├── body_limit.go         # [NEW] GoModel 逻辑, Gin 适配
│   │   ├── request_snapshot.go   # [NEW] GoModel 逻辑, Gin 适配
│   │   ├── audit.go              # [NEW] GoModel 逻辑, Gin 适配
│   │   ├── auth.go               # [KEEP] 我们的多层认证
│   │   ├── guard.go              # [KEEP] 我们的限流/并发控制
│   │   ├── tracing.go            # [KEEP] 我们的 OTel tracing
│   │   └── workflow.go           # [NEW] GoModel 逻辑, Gin 适配
│   │
│   ├── config/                   # [KEEP] Viper config loading
│   └── infra/                    # [KEEP] Redis/OTel/ConfigLoader
│       ├── redis.go
│       ├── trace.go
│       ├── subscriber.go         # Redis pub/sub
│       └── auth_client.go        # Control plane auth verification
│
├── configsync/                   # [KEEP] Redis-based provider snapshot sync
│
├── controlplane/                 # [KEEP] 完整保留
│   ├── auth/
│   ├── event/
│   ├── handler/
│   ├── middleware/
│   ├── model/
│   ├── repository/
│   ├── router/
│   ├── seed/
│   └── service/
│
└── auth/                         # [KEEP] Shared auth module
```

### 3.2 核心接口定义（对齐 GoModel）

#### 3.2.1 Provider Interface

```go
// internal/core/interfaces.go
// 完全采用 GoModel 的 Provider 接口
package core

type Provider interface {
    ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    StreamChatCompletion(ctx context.Context, req *ChatRequest) (io.ReadCloser, error)
    ListModels(ctx context.Context) (*ModelsResponse, error)
    Responses(ctx context.Context, req *ResponsesRequest) (*ResponsesResponse, error)
    StreamResponses(ctx context.Context, req *ResponsesRequest) (io.ReadCloser, error)
    Embeddings(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}

// 可选能力接口（类型断言）
type PassthroughProvider interface { ... }
type NativeBatchProvider interface { ... }
type NativeFileProvider interface { ... }
type AvailabilityChecker interface {
    CheckAvailability(ctx context.Context) error
}
```

#### 3.2.2 GroupRouter（我们的扩展层）

```go
// internal/gateway/grouprouter/group_router.go
package grouprouter

// GroupRouter 包装 GoModel 的 Router，在其上叠加分组调度能力
type GroupRouter struct {
    baseRouter   *providers.Router       // GoModel 的直连路由
    scheduler    *Scheduler              // 我们的分组调度器
    healthStore  *HealthStore            // 我们的健康状态
    healthProber *HealthProber           // 我们的主动探测
    fallback     *fallback.Resolver      // GoModel 的 Fallback
    identityInj  *IdentityInjector       // 我们的身份注入
    groupCache   *GroupNameCache         // 分组名缓存
    keyBinder    *KeyGroupBinder         // Key-Group 绑定检查
}

// Route 统一路由入口
// 1. 判断 model 是否为 group name
//    - 是: 走 scheduler 分组调度 → 得到具体 model+provider
//    - 否: 走 GoModel baseRouter 直连
// 2. 调用 GoModel LLMClient 执行请求
// 3. 失败时: 组内 failover → GoModel fallback chain
func (gr *GroupRouter) ChatCompletion(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error)
func (gr *GroupRouter) StreamChatCompletion(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error)
```

#### 3.2.3 LLMClient 集成

```go
// internal/llmclient/hooks.go
// 我们扩展 GoModel 的 hooks 接口，注入 OTel span

type OTelHooks struct {
    tracer trace.Tracer
}

func (h *OTelHooks) OnRequestStart(ctx context.Context, req Request) context.Context {
    ctx, span := h.tracer.Start(ctx, "llm.upstream.call",
        trace.WithAttributes(
            attribute.String("provider", req.Provider),
            attribute.String("model", req.Model),
        ),
    )
    return ctx
}

func (h *OTelHooks) OnRequestEnd(ctx context.Context, resp *Response, err error) {
    span := trace.SpanFromContext(ctx)
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    }
    if resp != nil {
        span.SetAttributes(attribute.Int("status_code", resp.StatusCode))
    }
    span.End()
}
```

---

## 四、迁移策略

### 4.1 分阶段迁移计划

```
Phase 1: 基础层移植 (Week 1-2)
├── 移植 internal/core/ (接口 + 类型)
├── 移植 internal/llmclient/ (HTTP 客户端 + 熔断)
├── 移植 internal/streaming/ (SSE 观察者)
└── 单元测试通过

Phase 2: Provider 层重建 (Week 3-4)
├── 移植 internal/providers/ (Factory + Router + Registry)
├── 移植所有 Provider adapters (15个)
├── 保留 bailian + litellm adapter
├── 适配 Provider 发现（从我们的 Control Plane 加载配置）
└── 集成测试：单 Provider 直连转发

Phase 3: 分组路由集成 (Week 5)
├── 重构 GroupRouter 包装 GoModel Router
├── 保留 Scheduler + HealthStore + HealthProber
├── 集成 GoModel Fallback（组内 failover → 跨组降级）
├── 身份注入适配新 Message 格式
└── 集成测试：分组调度 + 降级

Phase 4: 增强能力移植 (Week 6-7)
├── 移植 internal/workflows/ (Policy 引擎)
├── 移植 internal/guardrails/ (防护栏)
├── 移植 internal/responsecache/ (双层缓存)
├── 移植 internal/budget/ + internal/usage/
├── 移植 internal/auditlog/
└── 集成测试：各子系统独立验证

Phase 5: Server 层适配 (Week 8)
├── Handler 层对接新 core.ChatRequest/Response 类型
├── 流式 Handler 切换为 io.ReadCloser + io.Copy 模式
├── 新增 GoModel 逻辑的 Gin Middleware（RequestSnapshot/Audit/Workflow）
├── 新增 Passthrough 路由
├── 新增 Responses API / Anthropic Messages API / Embeddings API
└── API 兼容性测试

Phase 6: 联调 + 灰度 (Week 9-10)
├── Control Plane 适配新 Provider 配置格式
├── 端到端测试
├── 性能基准对比 (latency, throughput, memory)
├── 灰度部署方案
└── 文档更新
```

### 4.2 兼容性保障

| 外部接口 | 兼容性策略 |
|----------|-----------|
| `POST /v1/chat/completions` | 100% 向后兼容，请求/响应格式不变 |
| `GET /v1/models` | 向后兼容 + 新增字段 |
| 客户端 OpenAI SDK | 无需修改 |
| Control Plane API | 无变化（保留现有 GORM 模型） |
| Redis Pub/Sub 协议 | 无变化 |
| API Key 格式 | 无变化 (`sk-` prefix) |
| 运维控制台 | 无变化 + 后续新增 Workflow/Guardrail 管理页 |

---

## 五、关键设计细节

### 5.1 GoModel Router + GroupRouter 融合

请求路由的核心决策逻辑：

```
Request(model="xxx")
    │
    ▼
┌─────────────────────┐
│ GroupNameCache.IsGroup("xxx")? │
└──────────┬──────────┘
           │
     ┌─────┴─────┐
     │YES        │NO
     ▼            ▼
┌──────────┐  ┌──────────────────┐
│Scheduler │  │GoModel Router    │
│.Route()  │  │.ResolveModel()   │
│(RR/Pri/W)│  │(name→type→qual)  │
└─────┬────┘  └────────┬─────────┘
      │                 │
      ▼                 ▼
┌──────────────────────────────┐
│  Resolved: provider + model  │
│  → IdentityInjector (group)  │
│  → Guardrails Pipeline       │
│  → ResponseCache Check       │
│  → Budget Check              │
└──────────────┬───────────────┘
               │
               ▼
┌──────────────────────────────┐
│  LLMClient.Do/DoStream()     │
│  (retry + circuit breaker)   │
└──────────────┬───────────────┘
               │
         ┌─────┴─────┐
         │OK         │FAIL
         ▼            ▼
   ┌──────────┐  ┌──────────────────────┐
   │ Return   │  │ 1. Group Failover    │
   │ Response │  │ 2. GoModel Fallback  │
   └──────────┘  │    (manual→auto)     │
                 └──────────────────────┘
```

### 5.2 LLMClient 配置映射

从我们现有的 Control Plane 配置映射到 GoModel `llmclient.Config`：

```go
// 当前 ProviderEntry (Control Plane 下发)
type ProviderEntry struct {
    ProviderID   uint
    ProviderName string
    ProviderType string
    BaseURL      string
    APIKey       string  // 已解密
    Models       []ModelEntry
}

// 映射为 GoModel ProviderConfig
func toGoModelConfig(entry ProviderEntry) providers.ProviderConfig {
    return providers.ProviderConfig{
        Name:    entry.ProviderName,
        Type:    entry.ProviderType,
        BaseURL: entry.BaseURL,
        APIKey:  entry.APIKey,
        Models:  mapModels(entry.Models),
        Resilience: &providers.ResilienceConfig{
            MaxRetries:     3,
            InitialBackoff: 500 * time.Millisecond,
            MaxBackoff:     10 * time.Second,
            BackoffFactor:  2.0,
            JitterFactor:   0.2,
            CircuitBreaker: &providers.CircuitBreakerConfig{
                FailureThreshold: 5,
                SuccessThreshold: 3,
                Timeout:          30 * time.Second,
            },
        },
    }
}
```

### 5.3 健康探测 + 熔断器协同

两个机制互补而非互斥：

```
                     ┌─────────────────────────────┐
                     │    Health State Machine      │
                     │                             │
                     │  Active Probe (30s)         │
                     │    ↓ probe fail ×3          │
  Circuit Breaker    │  ┌─────────┐               │
  (per-LLMClient)   │  │Unhealthy│◄──────────┐   │
       ┌───────┐    │  └────┬────┘            │   │
       │Closed │    │       │ probe ok ×3     │   │
       └───┬───┘    │       ▼                 │   │
  fail ×5  │        │  ┌─────────┐            │   │
       ┌───▼───┐    │  │ Healthy │────────────┘   │
       │ Open  │    │  └─────────┘  fail threshold│
       └───┬───┘    │                             │
  timeout  │        └─────────────────────────────┘
       ┌───▼─────┐
       │Half-Open│   Circuit Breaker: 快速保护（秒级）
       └─────────┘   Health Probe:    持续恢复检测（30s级）
```

- **Circuit Breaker** 在 LLMClient 层保护单个请求链路（秒级响应）
- **HealthProber** 在 Scheduler 层做全局路由决策（30s 粒度）
- 当 CB 打开时，请求立即失败 → 触发 Failover/Fallback
- 当 HealthProber 标记 Unhealthy 时，Scheduler 直接跳过该模型（不消耗 CB 探针）

### 5.4 流式处理迁移

**Before (Channel-based):**
```go
// 当前模式: Provider → chan StreamChunk → Handler SSE encode
func (p *DeepSeekProvider) ChatStream(ctx, req) (<-chan model.StreamChunk, error) {
    ch := make(chan model.StreamChunk, 32)
    go func() { /* parse SSE → ch */ }()
    return ch, nil
}
```

**After (io.ReadCloser + Observer):**
```go
// GoModel 模式: Provider → io.ReadCloser → ObservedSSEStream → Gin response
func (p *DeepSeekProvider) StreamChatCompletion(ctx, req) (io.ReadCloser, error) {
    resp, err := p.client.DoStream(ctx, request)
    // 返回原始 HTTP body reader
    return resp, err
}

// Server 层 (Gin Handler)
func (h *Handler) handleStreamChat(c *gin.Context, req *core.ChatRequest) {
    stream, err := h.router.StreamChatCompletion(c.Request.Context(), req)
    if err != nil {
        respondError(c, err)
        return
    }
    
    // 包装 Observer（OTel + Usage 提取）
    observed := streaming.NewObservedSSEStream(stream,
        h.usageObserver,
        h.otelObserver,
    )
    defer observed.Close()
    
    // 零拷贝写入 HTTP response
    c.Header("Content-Type", "text/event-stream")
    c.Header("Cache-Control", "no-cache")
    c.Header("X-Accel-Buffering", "no")
    c.Status(http.StatusOK)
    
    // io.Copy 零拷贝直传
    io.Copy(c.Writer, observed)
    c.Writer.Flush()
}
```

### 5.5 配置热推送适配

GoModel 的 Provider 配置来自 YAML 文件，我们改为从 Redis Snapshot 动态加载：

```go
// internal/gateway/infra/provider_loader.go

type ProviderLoader struct {
    factory    *providers.ProviderFactory
    subscriber *Subscriber  // Redis pub/sub
    snapshot   *configsync.SnapshotStore
    current    atomic.Value // *providers.Router
}

func (pl *ProviderLoader) Start(ctx context.Context) {
    // 1. 启动时从 Redis 加载 snapshot
    snap := pl.snapshot.Load(ctx)
    router := pl.buildRouter(snap.Entries)
    pl.current.Store(router)
    
    // 2. 监听变更事件
    go pl.subscriber.Subscribe(ctx, "config:change", func(event ChangeEvent) {
        newSnap := pl.snapshot.Load(ctx)
        if newSnap.Version > snap.Version {
            newRouter := pl.buildRouter(newSnap.Entries)
            pl.current.Store(newRouter)  // 原子替换
        }
    })
    
    // 3. 30s 兜底对账
    go pl.reconcileLoop(ctx, 30*time.Second)
}

func (pl *ProviderLoader) buildRouter(entries []ProviderEntry) *providers.Router {
    registry := providers.NewModelRegistry()
    for _, entry := range entries {
        cfg := toGoModelConfig(entry)
        provider, _ := pl.factory.Create(cfg)
        registry.Register(entry.ProviderName, provider, cfg.Models)
    }
    router, _ := providers.NewRouter(registry)
    return router
}
```

### 5.6 数据库存储适配

GoModel 使用自己的 Storage 抽象（SQLite/PG/Mongo）。我们保留 PostgreSQL + GORM 作为 Control Plane 的存储，同时为 GoModel 的新子系统（Workflow/Budget/AuditLog）提供 PostgreSQL backend：

```go
// GoModel 的 storage 接口与我们的 GORM 共存
// 
// Control Plane: 继续用 GORM (model.Provider, model.Model, model.Group, ...)
// Gateway 新子系统: 用 GoModel 的 storage.PostgreSQL backend
//   - workflows → workflows 表
//   - budgets → budgets 表  
//   - audit_logs → audit_logs 表
//   - usage → usage 表
//
// 共享同一个 PostgreSQL 实例，但表互不冲突

func initGatewayStorage(dbURL string) storage.Storage {
    return storage.NewPostgreSQL(dbURL, storage.PostgreSQLConfig{
        MigrationsDir: "migrations/gateway",  // 新的迁移目录
        SchemaPrefix:  "gw_",                 // 表名前缀避免冲突
    })
}
```

---

## 六、API 扩展（新增能力）

迁移后我们将自动获得以下新 API：

| 新增路由 | 说明 |
|----------|------|
| `POST /v1/responses` | OpenAI Responses API |
| `GET/DELETE /v1/responses/:id` | Response 生命周期 |
| `POST /v1/messages` | Anthropic Messages API 原生支持 |
| `POST /v1/embeddings` | Embedding 接口 |
| `POST /v1/batches` | Batch 批量处理 |
| `POST /v1/files` | File 上传 |
| `/p/:provider/*` | Passthrough 直通代理 |
| `/admin/workflows` | Workflow CRUD |
| `/admin/guardrails` | Guardrail CRUD |
| `/admin/budgets` | Budget CRUD |

---

## 七、风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Channel→ReadCloser 流式改造影响 Anthropic/Bedrock adapter | 流式中断 | 逐 Provider 迁移 + 每个 adapter 独立集成测试 |
| GoModel 代码更新导致我们 fork 版本滞后 | 无法享受上游修复 | 保持最小化修改，用 wrapper 而非直改 |
| Control Plane 配置格式需适配 GoModel Provider | 管理端改动 | 用适配层（toGoModelConfig）隔离 |
| 新增 PostgreSQL 表与现有表冲突 | 迁移失败 | 表名前缀 `gw_` + 独立迁移目录 |
| 双层容错（CB + HealthProber）可能互相干扰 | 误判 | CB 状态对 Scheduler 不可见；Scheduler 决策优先 |
| GoModel 中间件逻辑适配 Gin HandlerFunc 签名 | 行为差异 | 逐个 MW 迁移 + 对比测试，Gin/Echo 的 Context API 高度相似 |

---

## 八、性能预期

| 指标 | 当前 | 迁移后预期 | 理由 |
|------|------|-----------|------|
| 非流式 P99 延迟 | ~50ms overhead | ~30ms overhead | 减少 JSON 解析/重构 |
| 流式首字节 | ~20ms overhead | ~10ms overhead | 零拷贝，无 channel 切换 |
| 流式内存/req | ~64KB avg | ~16KB avg | SSE observer 不缓存完整响应 |
| 吞吐量 | ~2000 QPS | ~2800 QPS | 减少 goroutine 数量 + 零拷贝流 |
| 缓存命中场景 | N/A (无缓存) | <5ms P99 | Redis exact-match 直接返回 |
| 故障恢复时间 | 30s (等探测) | <3s (CB 触发) | Circuit Breaker 秒级响应 |

---

## 九、迁移验收标准

### Phase 1-2 验收
- [ ] 所有现有 Provider (Bailian/DeepSeek/Anthropic/Bedrock/LiteLLM) 通过原有集成测试
- [ ] 新增 Provider (OpenAI/Azure/Gemini/Groq) 通过基础冒烟测试
- [ ] LLMClient 重试/熔断单元测试覆盖率 > 90%

### Phase 3 验收
- [ ] 分组调度 + Failover + Fallback 端到端测试通过
- [ ] 身份注入在新 Message 格式下正常工作
- [ ] 健康探测 + CB 协同无互相干扰

### Phase 4 验收
- [ ] Workflow 匹配规则正确（scope 优先级）
- [ ] Guardrail 管道串并行执行正确
- [ ] 响应缓存命中/穿透逻辑正确
- [ ] Budget 超额拒绝正确

### Phase 5 验收
- [ ] 所有现有 API 100% 向后兼容（请求/响应格式不变）
- [ ] 新增 API 符合 OpenAI 规范
- [ ] Middleware 栈行为与迁移前一致（认证、限流）

### Phase 6 验收
- [ ] 性能基准 ≥ 迁移前水平
- [ ] 灰度环境无 P0/P1 故障
- [ ] Control Plane + 运维控制台功能无退化

---

## 十、后续演进

迁移完成后可继续推进的优化：

1. **Admin Dashboard 嵌入** - 参考 GoModel 的 Admin SPA，为 Workflow/Guardrail/Budget 增加运维控制台页面
2. **语义缓存上线** - 接入 Qdrant/pgvector 向量库
3. **Conversation Store** - 支持多轮对话管理
4. **Model Aliases** - 支持友好模型名映射
5. **Pricing Overrides** - 支持自定义定价覆盖
6. **Prometheus Metrics** - 利用 GoModel 的 hooks 机制暴露指标
7. **pprof 集成** - 生产环境性能诊断
8. **Swagger 文档** - 自动生成 API 文档
