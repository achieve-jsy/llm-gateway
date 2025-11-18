# LLM Gateway

统一大模型网关 — 通过标准化的 OpenAI 兼容接口，统一对接多家大模型厂商服务。

## 架构概览

```
  客户端 / OpenCode            浏览器 (Vue SPA)
        │                            │
        └──────────────┬─────────────┘
                       ▼
                   nginx 反代
        /v1/* │      /api/* /auth/* │     /* (web/dist)
              ▼                     ▼
   ┌──────────────────┐   ┌──────────────────────┐
   │ gateway  :8080   │   │ control-plane :8081  │
   │ 鉴权→模型组调度  │◀──│ 渠道/模型/Key/组管理 │
   │ →providers.Router│   │ Catalog 同步/配置下发│
   └────────┬─────────┘   └──────────┬───────────┘
            │  provider snapshot      │
            ├──────────  Redis  ◀─────┤  config:change pub/sub
            ▼                         ▼
     上游厂商 (17+)             PostgreSQL
```

项目包含两个 Go 服务 + 一个前端：

- **gateway**（端口 8080）：对外暴露 OpenAI 兼容接口（`/v1/*`），鉴权 → 模型组调度 → 转发上游、流式转发。不直连数据库，配置来自 Redis snapshot / 控制面。
- **control-plane**（端口 8081）：管理后台 API、OAuth/SSO、渠道/模型/Key/模型组管理、Catalog 同步、配置下发。持久化在 PostgreSQL。
- **web**（Vue3 + Element Plus）：管理面 SPA，构建到 `web/dist`，由 nginx 静态托管。

> `/v1`、`/api`、`/auth`、`/`（前端）的分流由宿主机 **nginx** 完成（见 `deploy/docker/nginx*.conf`）。
>
> 架构与开发约定详见 [`AGENTS.md`](AGENTS.md)，OpenCode 模型发现插件详见 [`docs/opencode-models-discovery.md`](docs/opencode-models-discovery.md)。

## 支持的厂商

通过 `internal/providers/<vendor>/` 适配，在 `cmd/gateway/main.go` 的 `buildProviderFactory()` 注册：

| Provider 类型 | 说明 |
|----------|------|
| `openai` / `litellm` / `openai-compatible` | OpenAI 及任意 OpenAI 兼容代理 |
| `anthropic` | Claude 系列 |
| `bedrock` | AWS Bedrock（Claude/Titan/Nova 等） |
| `gemini` / `vertex` | Google Gemini / Vertex AI |
| `azure` | Azure OpenAI |
| `bailian`（`dashscope`） | 阿里云百炼/通义 |
| `deepseek` / `minimax` / `zai` / `xai` / `groq` / `openrouter` / `oracle` | 各厂商官方 API |
| `ollama` / `vllm` | 本地/自托管推理 |

> 对外的「模型名」通常是**模型组（Group）**：一个组内可挂多个具体模型，按 round_robin / priority / weighted 策略调度，并支持 fallback。模型能力（图像输入/工具调用/上下文/价格）由 `model_catalog`（从 LiteLLM 同步）提供。

## 快速开始

### 前置要求

- Go 1.22+
- Docker & Docker Compose
- Node.js 18+（前端开发）
- Bun（仅在改 `tools/opencode-models-discovery` 插件时需要）

### 1. 启动基础设施

```bash
# 启动 Postgres + Redis
make dev-infra

# 如需可观测性组件（Jaeger + OTel Collector）
make dev-infra-full
```

### 2. 配置环境

项目使用 Viper 配置体系，按 `APP_ENV` 环境变量加载对应配置文件：

```bash
config/
├── dev.yaml      # 开发环境（默认）
├── staging.yaml  # 预发环境
└── prod.yaml     # 生产环境
```

环境变量可覆盖 YAML 中的任何配置项。关键环境变量参考 `.env.example`。

### 3. 启动服务

```bash
# 启动 gateway-api
make dev

# 启动 control-plane（另一个终端）
make dev-cp
```

### 4. 验证

```bash
# 健康检查
curl http://localhost:8080/health

# 查看可用模型
curl -H "Authorization: Bearer your-api-key" http://localhost:8080/v1/models

# 聊天补全
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## API 文档

启动服务后访问 Swagger UI：

```
http://localhost:8080/swagger/index.html
```

生成/更新 Swagger 文档：

```bash
make swagger
```

## OpenCode 接入

控制面可一键下发 OpenCode 配置：在管理面「我的 Key」处生成配置脚本，脚本会把内嵌的模型发现插件下载到 `~/.config/opencode/plugins/`，并把对应 provider 配成自动发现网关模型、并通过 `/v1/model/info`（LiteLLM 兼容）带上真实能力（图像输入 / 工具调用 / 推理 / 上下文上限）。

完整架构、能力聚合规则与排查方法见 [`docs/opencode-models-discovery.md`](docs/opencode-models-discovery.md)。

## 项目结构

```
.
├── cmd/
│   ├── gateway/          # Gateway 入口
│   ├── control-plane/    # Control-Plane 入口
│   ├── opencode-helper/  # macOS URL Scheme 助手
│   └── bedrock_test/     # Bedrock 连通性调试
├── config/               # 环境配置 (dev/staging/prod.yaml)
├── internal/
│   ├── gateway/          # Gateway 进程逻辑 (config/handler/infra/orchestrator/scheduler/server/binding/loader)
│   ├── controlplane/     # 控制面 (handler/service/repository/model/router/middleware/event/seed)
│   ├── providers/        # 各厂商 Adapter + factory + registry + router
│   ├── core/             # 共享领域类型与 Provider 接口
│   ├── auth/             # 可嵌入的 JWT/OIDC/Key 验证库
│   ├── configsync/       # 控制面→网关的 provider snapshot (Redis)
│   ├── llmclient/        # 上游 HTTP 客户端（重试/熔断）
│   ├── fallback/         # fallback 候选链解析
│   └── cache/ modeldata/ streaming/ httpclient/ llmclientconfig/
├── migrations/           # PostgreSQL SQL 迁移 (按序号)
├── web/                  # 前端 (Vue 3 + Element Plus + Vite)，构建到 web/dist
├── tools/
│   └── opencode-models-discovery/  # OpenCode 模型发现插件 (fork)
├── deploy/docker/        # Dockerfile + nginx 配置
├── docs/                 # 架构/设计文档
├── tests/                # contract / integration 测试
├── api/swagger/          # Swagger 注解入口
├── docker-compose.yml
├── AGENTS.md
├── Makefile
└── go.mod
```

## 开发命令

| 命令 | 说明 |
|------|------|
| `make build` | 编译 gateway + control-plane |
| `make test` | 运行所有测试 |
| `make lint` | 代码静态检查 |
| `make swagger` | 生成 Swagger 文档 |
| `make dev` | 启动开发环境 (gateway) |
| `make dev-cp` | 启动开发环境 (control-plane) |
| `make dev-infra` | 启动 Postgres + Redis |
| `make dev-infra-full` | 启动全部基础设施 (含 Jaeger/Langfuse) |
| `make dev-down` | 停止所有容器 |
| `make clean` | 清理构建产物 |

## 可观测性

启用 OTLP 追踪：

```bash
# 在 config/dev.yaml 中设置或通过环境变量
OTLP_ENABLED=true
OTLP_ENDPOINT=localhost:4317
```

启动 Jaeger UI 查看 trace：

```bash
make dev-infra-full
# 打开 http://localhost:16686
```

Gateway 会自动为每个请求创建入口 span，并在调用上游模型时创建子 span，记录模型名、Provider、token 用量等信息。

## 配置优先级

从高到低：

1. 环境变量（如 `REDIS_ADDR`、`GATEWAY_PORT`）
2. `config/{APP_ENV}.yaml` 配置文件
3. 代码中的默认值

## License

Internal use only.
