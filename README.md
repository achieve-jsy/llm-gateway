# LLM Gateway

统一大模型网关 — 通过标准化的 OpenAI 兼容接口，统一对接多家大模型厂商服务。

## 架构概览

```
┌──────────────┐     ┌─────────────────────────────────────────────────┐
│   客户端     │     │                LLM Gateway                       │
│  (OpenAI SDK)│────▶│  Gateway API ──▶ Orchestrator ──▶ Provider       │
└──────────────┘     │       │                             │            │
                     │       ▼                             ▼            │
                     │  Redis Cache              ┌─────────────────┐   │
                     │                           │ Bailian/DeepSeek│   │
                     │  Control-Plane ◀─ DB      │ Anthropic/Bedrock│  │
                     │  (模型 & 渠道管理)         │ LiteLLM          │  │
                     └─────────────────────────────────────────────────┘
```

项目包含两个服务：

- **gateway-api**（端口 8080）：对外暴露 OpenAI 兼容接口，处理路由分发、负载均衡和流式转发
- **control-plane**（端口 8081）：管理后台，负责渠道/模型注册、配置下发、ops-console 前端

## 支持的厂商

| Provider | 说明 |
|----------|------|
| Bailian (百炼/通义) | 阿里云 DashScope OpenAI 兼容模式 |
| DeepSeek | DeepSeek 官方 API |
| Anthropic | Claude 系列模型 |
| Bedrock | AWS Bedrock (Claude/Titan/Nova 等) |
| LiteLLM | 通用代理，可接入 100+ 模型 |

## 快速开始

### 前置要求

- Go 1.22+
- Docker & Docker Compose
- Node.js 18+（前端开发）

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

## 项目结构

```
.
├── cmd/
│   ├── gateway/          # Gateway API 入口
│   └── control-plane/    # Control-Plane 入口
├── config/               # 环境配置文件 (dev/staging/prod)
├── internal/
│   ├── gateway/          # Gateway 核心逻辑
│   │   ├── config/       # Viper 配置加载
│   │   ├── handler/      # HTTP Handler (chat, models, health)
│   │   ├── infra/        # Redis, OTLP, ConfigLoader
│   │   ├── middleware/   # RequestID, Logger, Auth, Tracing
│   │   ├── orchestrator/ # 请求编排 (同步/流式)
│   │   ├── provider/     # Provider 接口 & 各厂商 Adapter
│   │   └── server/       # Gin 路由注册
│   └── controlplane/     # Control-Plane 核心逻辑
│       ├── handler/      # Admin API Handler
│       ├── model/        # 数据模型 (Provider, Model, Tag)
│       ├── repository/   # GORM 数据访问层
│       └── service/      # 业务服务层
├── migrations/           # SQL 迁移文件
├── web/                  # 前端 (Vue 3 + Element Plus)
├── deploy/               # 部署配置 (OTel Collector 等)
├── api/swagger/          # Swagger 注解入口
├── docker-compose.dev.yml
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
