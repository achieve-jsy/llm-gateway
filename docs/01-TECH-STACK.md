# 模型网关 — 技术架构文档

| 项 | 值 |
| :--- | :--- |
| 文档版本 | v0.1 |
| 最后更新 | 2026-05-20 |
| 状态 | 初稿（待评审） |
| 相关文档 | [`00-PRD.md`](./00-PRD.md) · [`02-ROADMAP.md`](./02-ROADMAP.md) |

> 本文件是项目的**技术架构总览**，提供「选什么 / 为什么 / 怎么搭」的全景视图。模块级的详细设计（HLD / Design / Data Model / Contracts）由 `xspec/modules/<module>/` 通过 xflow 工作流单独维护。

---

## 1. 技术栈总览

| 层 | 技术选型 | 说明 |
| :--- | :--- | :--- |
| **后端框架** | Go 1.22+ / Gin | 轻量、生态成熟，符合"极致聚焦"定位 |
| **多厂商转发** | `voocel/litellm`（Go 原生）+ 厂商原生 SDK（百炼 / DeepSeek / AWS Bedrock） | 见 §3 多厂商策略 |
| **统一协议** | OpenAI 兼容 API（`/v1/chat/completions`, `/v1/embeddings`, `/v1/models`） | 业务方零成本切换 |
| **元数据库** | PostgreSQL 15+ | 用户 / 部门 / Key / 模型组 / 限额 / 套餐、**用量日聚合**等关系数据 |
| **缓存 / 限流** | Redis 7+ | TPM / RPM / 并发计数器、模型列表缓存、KV 缓存 |
| **可观测后端** | **OTLP exporter（默认对接 Langfuse v3+）** | 调用 Trace / Span / Log 全部走 OTLP 协议出口，对端可换 Langfuse / SigNoz / Jaeger / Tempo 等任意兼容工具 |
| **业务指标 / 运维监控** | Prometheus + Grafana（可选） | 网关自身 QPS / 错误率 / 延迟 histogram 等业务指标 |
| **认证** | 公司 SSO（OIDC）+ JWT 内部会话 | 详见 §5 |
| **前端管理后台** | Vue 3 + TypeScript + Element Plus + Vite + Pinia + Vue Router | |
| **部署** | Docker + Docker Compose（初期）→ K8s（远期） | V0.1 – V1.0 全部使用 Docker Compose |
| **CI/CD** | GitLab CI（公司内部） | 镜像构建 + 自动部署 + 单元 / 集成测试 |
| **API 文档** | Swagger / OpenAPI 3.0（swaggo/swag 自动生成） | |
| **配置管理** | Viper + 环境变量 + 配置文件分层 | dev / staging / prod 三套 |

## 2. 系统总体架构

```
                       ┌────────────────────────────────┐
                       │      用户 / 业务系统 / 脚本     │
                       │  (走 OpenAI 兼容 SDK / curl)   │
                       └──────────────┬─────────────────┘
                                      │
                              HTTPS / OpenAI 协议
                                      │
        ┌─────────────────────────────▼──────────────────────────────┐
        │                    模型网关 (Go / Gin)                     │
        │                                                            │
        │  ┌────────────┐  ┌────────────┐  ┌────────────────────┐   │
        │  │ 鉴权 + Key │  │ 路由 + 限流│  │ 模型组 + 负载均衡  │   │
        │  │  (中间件)  │  │ TPM/RPM/并发│  │  (多决策策略)      │   │
        │  └────────────┘  └────────────┘  └────────────────────┘   │
        │                                                            │
        │  ┌─────────────────────────────────────────────────────┐   │
        │  │           厂商适配层（Adapter Interface）           │   │
        │  └────┬─────────────┬─────────────┬──────────────┬─────┘   │
        │       │             │             │              │         │
        │   百炼 SDK     DeepSeek SDK   AWS Bedrock    voocel/litellm│
        │                                                  (兜底)    │
        └──┬─────────────┬─────────────┬──────────────┬──────────────┘
           │             │             │              │
        ┌──▼──────────────────────────────────────────▼─────┐
        │   阿里百炼  |  DeepSeek  |  AWS Bedrock  |  ...   │
        └───────────────────────────────────────────────────┘

        ┌──────────────────────────┐    ┌──────────────────────────┐
        │       元数据：Postgres   │    │   计数 / 缓存：Redis     │
        │  用户 / Key / 模型组 /   │    │  TPM/RPM/并发 计数器     │
        │  部门权限 / 限额套餐 /   │    │  模型列表缓存 / KV cache │
        │  用量日聚合              │    │  渠道健康度              │
        └──────────────────────────┘    └──────────────────────────┘

        ┌──────────────────────────────────────────────────────────────┐
        │   观测出口：OTLP exporter（traces + logs + metrics）         │
        │                          │                                   │
        │                          ▼                                   │
        │   ┌──────────────────────────────────────────────────────┐   │
        │   │  Langfuse v3+（默认推荐，专为 LLM 调用追溯设计）     │   │
        │   │   — 或任意 OTLP 兼容后端：SigNoz / Jaeger / Tempo /  │   │
        │   │     OpenObserve …（运维侧可替换，网关无需改代码）   │   │
        │   └──────────────────────────────────────────────────────┘   │
        │                                                              │
        │   网关进程内并行暴露 Prometheus /metrics（可选）：           │
        │     gateway_requests_total / ttft_seconds / errors_total ... │
        └──────────────────────────────────────────────────────────────┘

        ┌──────────────────────────┐
        │  前端管理后台 (Vue 3 +   │
        │   Element Plus + Vite)   │
        │  Key 管理 / 模型组 /     │
        │  限额 / 看板 / 日志查询  │
        └──────────────────────────┘
```

## 3. 多厂商集成策略（核心决策）

### 3.1 LiteLLM Go 生态澄清

> ⚠️ **关键事实**：LiteLLM 官方项目（[BerriAI/litellm](https://github.com/BerriAI/litellm)）只有 Python 实现。Go 生态有几个**社区移植版**：

| 库 | 类型 | 说明 |
| :--- | :--- | :--- |
| `voocel/litellm` | **Go 原生多厂商客户端** | 仿 LiteLLM API 设计，Go 实现，无 Python 依赖，支持 OpenAI/Claude/Gemini 等 |
| `andrejsstepanovs/go-litellm` | **LiteLLM Proxy 的 Go 客户端** | 调用 LiteLLM Python Proxy 的 SDK，仍需部署 Python 服务 |
| `recally-io/go-litellm` | **Go 原生统一网关库** | 100+ 厂商 OpenAI 兼容封装，与本项目定位接近 |

### 3.2 推荐策略：分层 + 渐进收敛

```
┌─────────────────────────────────────────────────────────────┐
│                  Provider Adapter Interface                 │
│  type Provider interface {                                  │
│      Chat(ctx, req) (Resp, error)                           │
│      ChatStream(ctx, req) (StreamReader, error)             │
│      Embeddings(...)                                        │
│      Models(ctx) ([]Model, error)                           │
│      Health(ctx) error                                      │
│  }                                                          │
└─────────────────────────────────────────────────────────────┘
              │                  │                 │
   ┌──────────┴───────┐ ┌────────┴───────┐ ┌──────┴────────┐
   │ 原生厂商 SDK     │ │ voocel/litellm │ │ 自实现 HTTP    │
   │（百炼/DS/AWS）   │ │（兜底冷门厂商） │ │（特殊协议厂商）│
   └──────────────────┘ └────────────────┘ └───────────────┘
```

| 阶段 | 策略 | 理由 |
| :--- | :--- | :--- |
| **V0.1 – V0.2** | 全部走 `voocel/litellm`（或 `recally-io/go-litellm`） | 快速跑通 MVP，验证抽象接口 |
| **V0.3 – V0.6** | 核心厂商（百炼 / DeepSeek / AWS Bedrock）改用**原生 SDK** | 国产厂商特性（如百炼私有协议、流式 SSE 差异）原生 SDK 兼容更好；冷门厂商继续走 `voocel/litellm` 兜底 |
| **V0.7+** | 完全 Go 自主可控，必要时局部 fork | 不被任何上游卡脖子 |

> **不引入 LiteLLM Python Proxy**：避免维护两个语言栈、增加部署复杂度，与"架构精简"原则冲突。

### 3.3 厂商接入优先级（与 V1.0 范围对齐）

| 优先级 | 厂商 | 模型示例 | 接入版本 |
| :--- | :--- | :--- | :--- |
| P0 | 阿里百炼 + 阿里 Token Plan | Qwen-Max / Qwen-Plus / Qwen-Turbo | V0.1 |
| P0 | DeepSeek | deepseek-chat / deepseek-reasoner | V0.1 |
| P0 | AWS Bedrock | Claude Sonnet / Haiku | V0.2 |
| P1 | OpenAI 兼容（自建 / 第三方） | GPT-4o / o1 等 | V0.3 |
| P2 | 字节豆包 / Moonshot / 智谱 | 国产其他厂商 | V1.x |

## 4. 数据存储设计

> **设计原则**：网关本体只持有**业务必需的状态**（用户 / Key / 限额 / 用量日聚合）和**实时计数**（Redis），所有**调用明细 / Trace** 通过 OTLP 出口到外部可观测后端（默认 Langfuse），保持架构精简、可替换。

### 4.1 PostgreSQL（结构化元数据 + 用量日聚合）

主要表族（详细字段定义在 `xspec/modules/<module>/data-model.md`）：

| 表族 | 关键表 |
| :--- | :--- |
| 身份 / 组织 | `users`, `departments`, `roles`, `user_roles` |
| Key 管理 | `api_keys`, `api_key_devices`, `api_key_audit_logs` |
| 模型抽象 | `providers`, `provider_credentials`, `models`, `model_groups`, `model_group_members` |
| 权限 | `permission_model_groups`, `dept_model_group_permissions` |
| 限额 | `quotas`, `token_plans`, `quota_consumption_periods` |
| **用量聚合** | `usage_daily`（user_id / dept_id / model_group / model × 日） |

> `usage_daily` 由网关进程**异步定时聚合**（每分钟 flush 进程内累计计数器 → upsert 到 Postgres），供前端用量页 / 部门看板 / 限额扣减查询，**避免对 Langfuse 形成查询热依赖**。

### 4.2 Redis（实时计数 / 缓存）

| 用途 | Key Pattern | TTL |
| :--- | :--- | :--- |
| TPM 限流 | `rl:tpm:{key_id}:{model}:{minute_bucket}` | 120s |
| RPM 限流 | `rl:rpm:{key_id}:{model}:{minute_bucket}` | 120s |
| 并发计数 | `rl:concurrent:{key_id}:{model}` | 调用结束 - 1 |
| 模型列表缓存 | `cache:models:{user_id}` | 60s |
| KV 缓存（Prompt 命中） | `cache:llm:{hash(req)}` | 可配置（默认 1h） |
| 渠道健康度 | `health:provider:{provider}:{model}` | 30s |
| **用量当日累计**（聚合 buffer） | `usage:buf:{date}:{user_id}:{model}` | 当日 |

### 4.3 OTLP 出口 + Langfuse（调用 Trace / 日志 / 指标）

> 网关不自建任何 OLAP 数据库存日志，**所有调用细节通过 OpenTelemetry SDK 出口**，对端可换任意 OTLP-compatible 后端。

#### 4.3.1 出口协议与数据形态

| 数据类型 | 内容 | OTLP 通道 |
| :--- | :--- | :--- |
| **Trace / Span** | 一次完整调用链：网关入口 → 鉴权 → 限流 → 路由决策 → 上游厂商调用 → 响应/流式块 | `traces` |
| **Log** | 错误堆栈、关键事件（限流命中、熔断、降级） | `logs`（与 trace_id 关联） |
| **Metric** | TTFT histogram、QPS、错误率、Token 数 | `metrics`（也通过 Prometheus `/metrics` 暴露给运维） |
| **LLM 专属属性** | model / provider / tokens_in / tokens_out / cost / cached / user_id / dept_id / key_id | Span attributes（遵循 [Langfuse GenAI 语义约定](https://langfuse.com/docs/opentelemetry)） |

#### 4.3.2 推荐后端：Langfuse

理由：
- **专为 LLM 调用追溯设计**：内置 Trace / Generation / Session / User 视图，比通用 APM 更对口
- **原生 OTLP 兼容**（v3+）：网关只发标准 OTLP，运维侧自行部署 Langfuse 即可
- **开源自部署**：可与公司合规要求对齐（敏感 Prompt 不出公司网络）
- **支持评测 / Cost 归因 / Prompt 版本对比** 等 LLM 专属能力，未来落地「使用习惯分析」零成本扩展

#### 4.3.3 出口策略

- **Sampling**：默认 100% 采样（LLM 调用频率远低于普通 RPC，单条价值高）；可配置按模型 / 用户降采样
- **批量发送**：OTLP exporter 内置 batch + queue，避免阻塞业务请求
- **降级**：OTLP collector 不可达时，缓存在本地环形 buffer（最多 N 分钟），恢复后补发；持续不可达则丢弃并打告警，**绝不阻塞业务**
- **Prompt 脱敏**：在 OTLP exporter 拦截层做（可配置策略：完全不记录 / 截断 / 关键字脱敏 / 完整记录）

#### 4.3.4 后端可替换性

> 网关不感知具体后端。运维若希望换为 SigNoz / Jaeger（无 LLM 语义）/ OpenObserve / Tempo + Loki，只需改 OTLP collector 的下游路由，**网关无需任何改动**。

## 5. 认证与鉴权架构

```
┌─────────┐  ① OIDC 登录  ┌──────────────┐
│ 员工浏览│ ────────────► │ 公司 SSO     │
│   器    │ ◄──── code ── │ (OIDC / IDP) │
└─────────┘               └──────────────┘
     │
     │ ② code → 网关后端换 JWT
     ▼
┌─────────────────────┐
│ 网关后端            │
│ - 校验 OIDC token   │
│ - 写入 users 表     │
│ - 签发内部 JWT      │
└──────────┬──────────┘
           │ JWT
           ▼
   [Web UI 管理 Key / 看板]

────────── 业务侧 / 脚本调用 ──────────

┌────────────┐  Authorization: Bearer sk-...  ┌──────────────────┐
│ 业务系统   │ ────────────────────────────►  │ 网关 /v1/chat... │
│ (Go/Py/JS) │                                │ - Key 校验       │
└────────────┘                                │ - 部门权限校验   │
                                              │ - 限流 + 路由    │
                                              └──────────────────┘
```

- **两类身份**：浏览器登录（OIDC + JWT）｜ 业务调用（API Key）
- **统一身份源**：API Key 必须绑定到 SSO 用户，所有用量可追溯到员工
- **权限决策点**：调用时按 `Key → User → Department → 部门权限模型组` 链路决策能否调用某个模型组

## 6. 关键决策记录（ADR 简版）

> 完整 ADR 在 `xspec/modules/<module>/design.md` 的「设计决策」章节。这里只列项目级关键决策。

| # | 决策 | 理由 |
| :-- | :--- | :--- |
| ADR-001 | 后端用 Go + Gin，不用 Kratos / Fiber | 轻量足够、社区生态最广，符合"极致聚焦"定位 |
| ADR-002 | 不引入 LiteLLM Python Proxy | 避免多语言栈维护成本；用 Go 原生 SDK + voocel/litellm 替代 |
| ADR-003 | 前端 Vue 3 + Element Plus | 团队熟悉度高，管理后台开发效率最优 |
| ADR-004 | 元数据用 PostgreSQL 不用 MySQL | JSONB 字段 + CTE 查询能力对模型组 / 限额配置更友好 |
| ADR-005 | 调用 Trace / 日志走 OTLP → Langfuse，**不自建 ClickHouse** | 网关聚焦核心能力，可观测后端用成熟产品（Langfuse 专为 LLM 设计）；OTLP 标准协议保证后端可替换；省去自维护 OLAP 集群成本 |
| ADR-006 | TPM/RPM/并发用 Redis Lua 脚本原子计数 | 单实例下毫秒级、跨进程一致，避免 DB 竞争 |
| ADR-007 | 部署 V1.0 前用 Docker Compose 单机 | 控制运维复杂度；MVP 阶段单机性能足够（1000+ QPS） |
| ADR-008 | 暴露 OpenAI 兼容协议作为唯一对外接口 | 业务方零成本切换；可直接复用所有 OpenAI SDK |

## 7. 项目目录结构（建议）

```
llm_gateway/
├── cmd/                            # 启动入口
│   ├── gateway/main.go             #   网关主服务
│   └── admin/main.go               #   管理后台 API 服务（可与 gateway 合并）
├── internal/
│   ├── adapter/                    # 厂商适配层
│   │   ├── bailian/
│   │   ├── deepseek/
│   │   ├── bedrock/
│   │   └── litellm/                #   voocel/litellm 兜底
│   ├── router/                     # 模型组路由 + 负载均衡
│   ├── ratelimit/                  # TPM/RPM/并发限流
│   ├── auth/                       # SSO + JWT + Key 校验
│   ├── quota/                      # 限额 + 套餐 + 用量聚合 flusher
│   ├── telemetry/                  # OTLP exporter + Prometheus metrics + 脱敏
│   ├── domain/                     # 领域模型
│   ├── repo/                       # 数据访问（Postgres / Redis）
│   └── api/                        # HTTP handler + middleware
├── pkg/                            # 可对外复用的工具
├── web/                            # Vue 3 前端
│   ├── src/
│   ├── package.json
│   └── vite.config.ts
├── deploy/
│   ├── docker-compose.yml          # 一键拉起所有依赖
│   ├── docker-compose.dev.yml
│   └── Dockerfile.gateway
│   └── Dockerfile.web
├── migrations/                     # Postgres schema 版本化（golang-migrate）
├── scripts/                        # 运维脚本
├── docs/                           # 项目级文档（本目录）
├── xspec/                          # xflow 设计制品
├── go.mod
└── README.md
```

## 8. 性能基线目标（V1.0）

| 指标 | 目标 | 验证方式 |
| :--- | :--- | :--- |
| 单实例 QPS | ≥ 1000（纯转发） | wrk / k6 压测 |
| 网关额外延迟（P95） | ≤ 20ms（不含上游） | 压测对比 |
| TTFT P95 | 相比 LiteLLM 下降 ≥ 30% | 同模型 A/B 对比 |
| 接口可用率 | ≥ 99.9% | 月度统计 |
| 启动时间 | ≤ 5s | 启动到健康检查通过 |

## 9. 风险与缓解

| 风险 | 缓解措施 |
| :--- | :--- |
| `voocel/litellm` 社区维护强度未知 | 抽象 Provider 接口屏蔽底层；必要时 fork 自维护 |
| 国产厂商 SDK 兼容性不稳定 | 适配层做版本固定，关键厂商安排回归测试 |
| Langfuse 不可达导致 trace 丢失 | OTLP exporter 本地 buffer + 重试；持续不可达打告警但**不阻塞业务请求** |
| Langfuse 自部署运维资源 | 单实例起步即可（Langfuse 自带 ClickHouse + Postgres）；可由运维 / 数据团队接管 |
| OIDC 对接公司 SSO 阻塞 | V0.1 提供本地账号回退，V0.2 完成正式对接 |
| Docker Compose 单机扩展瓶颈 | V1.x 引入 K8s + 多副本 + Redis 集群 |

## 10. 变更历史

| 版本 | 日期 | 变更说明 |
| :--- | :--- | :--- |
| v0.1 | 2026-05-20 | 初稿：技术栈、架构图、多厂商策略、存储设计、关键 ADR |
| v0.2 | 2026-05-20 | **去除自建 ClickHouse**：调用 Trace / 日志改走 OTLP → Langfuse；用量统计改为 Postgres `usage_daily` 聚合表（异步 flush）；ADR-005 同步修订；目录结构 `observ/` → `telemetry/` |
