# LLM Gateway

> 统一大模型网关：用一个 OpenAI 兼容入口，把请求按「模型组」调度到 20+ 家厂商，并在管理台统一管理渠道、模型、Key、配额与用量。

<p>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white">
  <img alt="Vue" src="https://img.shields.io/badge/Vue-3-4FC08D?logo=vuedotjs&logoColor=white">
  <img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white">
  <img alt="Redis" src="https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white">
  <img alt="Docker" src="https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white">
</p>

## 这是什么

一套可自托管的多模型统一接入层：客户端只认一个 **OpenAI 兼容接口**，网关负责鉴权、限流、模型组调度、上游转发与流式回传；管理台负责渠道、模型、Key、模型组、配额与调用分析。

相比「一个 Key 打天下」的裸代理，它解决多团队共用时的三个问题：

1. **入口统一**：OpenAI / Claude / Bedrock / Gemini / 通义 / DeepSeek / 自建 vLLM 等上游收敛成一个 `/v1` 接口，换厂商不改业务代码。
2. **调度可控**：对外的模型名是「模型组」，组内多模型按 round_robin / priority / weighted 分流，并支持 fallback 兜底。
3. **成本可算**：能力、价格、上下文由 `model_catalog`（从 LiteLLM 同步）统一提供，调用日志落到 PostgreSQL，可按 Key / 模型 / 部门统计。

## 核心特性

- **OpenAI 兼容**：`/v1/chat/completions`、`/v1/embeddings`、`/v1/models`、`/v1/model/info`（LiteLLM 兼容能力查询），支持 SSE 流式转发。
- **模型组调度**：组内多模型按策略分流，单模型失败可 fallback；组能力按「成员全部支持才算支持、上下文取最小正值」的保守语义聚合。
- **多渠道多厂商**：同一厂商类型可挂多个渠道，支持凭证加密存储、启停、健康探测。
- **Key 与配额**：Key 由控制面签发与吊销，网关照多层缓存校验（本地 5s + Redis 15s），吊销事件主动失效。
- **配置热更新**：控制面写 Redis snapshot 并 pub/sub `config:change`，网关照单原子重建 Provider Router，无需重启。
- **管理台**：渠道 / 模型 / Catalog / 模型组 / Key / 用量看板 / 健康 / 公告等页面（Vue 3 + Element Plus）。
- **可观测性**：结构化日志 + OTLP 链路追踪（请求入口 span + 每次上游调用子 span，记录模型、Provider、token 用量）。
- **OpenCode 接入**：一键下发 OpenCode 配置与模型发现插件，自动发现网关模型并带上真实能力标记。

## 架构

```
  客户端 / SDK / OpenCode              浏览器 (Vue 3 SPA)
          |                                 |
          +---------------+-----------------+
                          v
                      nginx 反代
        /v1/*  |      /api/* /auth/*  |    /* (web/dist)
               v                      v
     +------------------+   +-----------------------+
     |  gateway  :8080  |   |  control-plane :8081  |
     |  鉴权 -> 模型组调度|<- |  渠道/模型/Key/组 管理  |
     |  -> providers    |   |  Catalog 同步 / 配置下发 |
     +--------+---------+   +-----------+-----------+
              | provider snapshot       |
              +----------  Redis  <------+  config:change
              v                         v
        上游厂商 (20+)             PostgreSQL
```

- **gateway**（`:8080`）：数据面。**不直连数据库**，配置来自 Redis snapshot 或控制面内部接口。
- **control-plane**（`:8081`）：控制面。管理 API、OAuth/SSO、Catalog 同步、配置下发，持久化在 PostgreSQL。
- **web**：管理台 SPA（Vue 3 + Element Plus + Vite），构建产物由 nginx 静态托管，不 embed 进 Go 二进制。

## 支持的厂商

在 `internal/providers/<vendor>/` 实现 `core.Provider`，并在 `cmd/gateway/main.go` 的 `buildProviderFactory()` 注册：

| 类型 | 说明 |
|------|------|
| `openai` / `openai-compatible` / `litellm` | OpenAI 及任意 OpenAI 兼容代理 |
| `anthropic` | Claude 系列 |
| `bedrock` | AWS Bedrock（Claude / Titan / Nova） |
| `gemini` / `vertex` | Google Gemini / Vertex AI |
| `azure` | Azure OpenAI |
| `bailian` / `dashscope` | 阿里云百炼 / 通义 |
| `deepseek` / `minimax` / `zai` / `xai` / `groq` / `openrouter` / `oracle` | 厂商官方 API |
| `ollama` / `vllm` | 本地 / 自托管推理 |

## 快速开始

### 前置要求

- Go 1.22+
- PostgreSQL、Redis（自行准备）
- Node.js 18+（前端开发）

### 1. 配置

```bash
cp .env.example .env   # 填入 DATABASE_URL / REDIS_ADDR / INTERNAL_TOKEN / JWT_SECRET 等
```

配置加载顺序：**环境变量 > `config/{APP_ENV}.yaml` > 代码默认值**（`APP_ENV` 默认 `dev`）。

### 2. 启动

```bash
make dev-all     # 并行拉起 control-plane + gateway + web(Vite :5173)，Ctrl-C 全部停止
```

或单独启动：

```bash
make dev         # gateway
make dev-cp      # control-plane
make dev-web     # 前端（首次需 cd web && npm install）
```

### 3. 验证

```bash
curl http://localhost:8080/health

curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <你的 API Key>" \
  -d '{"model":"<模型组名>","messages":[{"role":"user","content":"Hello"}]}'
```

Swagger：`http://localhost:8080/swagger/index.html`（生成用 `make swagger`）。

## API 一览

| 路径 | 说明 |
|------|------|
| `GET /health` / `GET /ready` | 存活 / 就绪探针 |
| `GET /v1/models` | 当前可用模型（组）列表 |
| `GET /v1/model/info` | LiteLLM 兼容的能力 / 价格 / 上下文查询 |
| `POST /v1/chat/completions` | 对话补全（支持流式） |
| `POST /v1/embeddings` | 向量化 |
| `POST /v1/p/:provider/*` | 指定渠道透传 |
| `POST /auth/refresh` / `/auth/logout` | 令牌刷新 / 登出 |
| `GET /.well-known/jwks.json` | JWT 公钥 |

控制面：`/api/v1/*`（管理面，需 JWT）、`/internal/v1/*`（网关内部调用，校验 `X-Internal-Token`）。

## 管理台能力

渠道管理 · 模型与 Catalog 同步 · 模型组与调度策略 · Key 签发/吊销与配额 · 用量看板（按用户/部门/模型） · 健康检查 · 公告与通知 · OpenCode 配置下发。

## 项目结构

```
cmd/
  gateway/            # 数据面入口
  control-plane/      # 控制面入口
internal/
  gateway/            # 网关逻辑：鉴权中间件、调度、转发、配置热加载
  controlplane/       # 控制面：handler / service / repository / model / router
  providers/          # 各厂商 Adapter + factory + registry + router
  core/               # 共享领域类型与 Provider 接口
  auth/               # 可嵌入的 JWT / OIDC / Key 校验
  configsync/         # 控制面 -> 网关的 provider snapshot
  llmclient/          # 上游 HTTP 客户端（重试 / 熔断）
  fallback/           # fallback 候选链
  cache/ modeldata/ streaming/ observability/
migrations/           # PostgreSQL 迁移（按序号）
web/                  # 管理台（Vue 3 + Element Plus + Vite）
deploy/               # Dockerfile / nginx / 编排
tests/                # 单元 / 组件 / 集成 / 契约测试
docs/                 # 架构、部署、数据库、可观测性等文档
```

## 开发命令

| 命令 | 说明 |
|------|------|
| `make build` | 编译 gateway + control-plane 到 `bin/` |
| `make test` | 单元测试 + 组件测试 |
| `make lint` | golangci-lint |
| `make swagger` | 重新生成 Swagger 文档 |
| `make dev-all` / `dev` / `dev-cp` / `dev-web` | 开发启动 |
| `make clean` | 清理构建产物 |

集成测试：`go test ./tests/... -tags=integration -count=1`（需 Docker）。

## 可观测性

```bash
OTLP_ENABLED=true OTLP_ENDPOINT=localhost:4317
docker compose --profile observability up -d   # Jaeger + OTel Collector
# UI: http://localhost:16686
```

## 文档

- [AGENTS.md](AGENTS.md)：代码地图、路由边界、关键机制与开发约定
- [docs/architecture.md](docs/architecture.md)：系统架构与分层
- [docs/deployment.md](docs/deployment.md)：部署与 nginx 路由
- [docs/database.md](docs/database.md)：数据库与迁移规范
- [docs/observability.md](docs/observability.md)：日志与链路追踪
- [docs/opencode-models-discovery.md](docs/opencode-models-discovery.md)：OpenCode 插件

## 说明

本项目以现状开源，仅作学习与参考。仓库内不含任何真实凭证，部署时请通过环境变量注入密钥；生产使用前请自行审计配置与安全策略。