# 系统架构

## 总体架构

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

## 服务边界

| 服务 | 端口 | 职责 | 数据存储 |
|------|------|------|----------|
| **gateway** | 8080 | OpenAI 兼容接口、鉴权、模型组调度、转发上游、流式响应 | Redis（snapshot 读取）|
| **control-plane** | 8081 | 管理后台 API、OAuth/SSO、CRUD、Catalog 同步、配置下发 | PostgreSQL + Redis |
| **web** | nginx 静态 | Vue3 管理面 SPA | 无 |

### Gateway 不直连数据库

**这是核心架构约束**。Gateway 只通过以下方式获取配置：
1. **Redis Snapshot**：Provider 配置由 control-plane 写入 Redis，gateway 监听 `config:change` pub/sub + 30s reconcile
2. **内部 HTTP API**：鉴权调用 `POST /internal/v1/auth/verify-key`，带双层缓存（本地 5s + Redis 15s）

## 分层结构

### Gateway 分层

```
cmd/gateway/main.go
    ├── internal/gateway/server/router.go      # 路由注册
    ├── internal/gateway/middleware/           # 中间件链（Auth/Guard/Tracing/Logger）
    ├── internal/gateway/handler/              # HTTP Handler
    ├── internal/gateway/orchestrator/         # 调用编排（非流式/流式）
    ├── internal/gateway/scheduler/            # 模型组调度（round_robin/priority/weighted）
    ├── internal/gateway/loader/               # Provider 热加载
    ├── internal/gateway/infra/                # 基础设施（Redis/KeyCache/AuthClient/Tracer）
    └── internal/providers/                    # 厂商 Adapter
```

### Control-Plane 分层

```
cmd/control-plane/main.go
    ├── internal/controlplane/router/          # 路由注册
    ├── internal/controlplane/middleware/      # 中间件（JWT/InternalAuth）
    ├── internal/controlplane/handler/         # HTTP Handler
    ├── internal/controlplane/service/         # 业务逻辑
    ├── internal/controlplane/repository/      # GORM 数据访问
    ├── internal/controlplane/model/           # 领域模型
    └── internal/controlplane/infra/           # 基础设施（Postgres）
```

## 依赖方向

```
handler → service → repository → model
           ↓
         infra
```

- **Handler**：HTTP 协议适配，参数校验，调用 Service
- **Service**：业务逻辑，事务编排，调用 Repository
- **Repository**：数据访问，GORM 查询
- **Model**：领域实体，表结构映射
- **Infra**：外部资源连接（DB/Redis/HTTP Client）

## 关键机制

### 模型组调度

```
请求 model=gpt-4 → 查找 Group(name=gpt-4) → 获取组内 Models → 调度策略选择 → 调用上游
                                                   ↓
                                        round_robin / priority / weighted
```

- **模型组（Group）**：对外暴露的模型名，组内可挂多个具体模型
- **调度策略**：`round_robin`（轮询）、`priority`（优先级，前面失败才用后面）、`weighted`（按权重分配）
- **Fallback**：组内模型失败时自动切换到下一个候选

### Provider 热加载

1. Control-plane 修改 Provider → 调用 `SaveProviderSnapshot()` 写 Redis + pub `config:change`
2. Gateway `ProviderLoader` 监听 pub/sub + 30s reconcile
3. 原子重建 `providers.Router`，不中断服务

### 能力聚合

模型组能力 = 组内全部成员能力的**交集**（保守语义）
- `supports_vision`：全部支持才为 true
- `supports_tools`：全部支持才为 true
- `max_tokens`：取**最小正值**

## 路由边界

### Gateway 路由（`internal/gateway/server/router.go`）

| 路径 | 鉴权 | 说明 |
|------|------|------|
| `/health` `/ready` | 无 | 健康检查 |
| `/swagger/*` | 无 | API 文档 |
| `/v1/models` | API Key | 模型列表 |
| `/v1/model/info` | API Key | 模型详情（LiteLLM 兼容） |
| `/v1/chat/completions` | API Key | 聊天补全 |
| `/v1/embeddings` | API Key | 向量嵌入 |
| `/v1/p/:provider/*path` | API Key | 直传指定厂商 |
| `/.well-known/jwks.json` | 无 | JWT 公钥 |
| `/auth/refresh` `/auth/logout` | JWT | Token 刷新/登出 |
| `/internal/v1/auth/verify-key` | Internal Token | Key 验证（供自身调用） |

### Control-Plane 路由（`internal/controlplane/router/router.go`）

| 路径 | 鉴权 | 说明 |
|------|------|------|
| `/api/v1/*` | JWT | 管理面 API |
| `/api/v1/auth/*` | 无/OAuth | OAuth 认证 |
| `/api/v1/settings/public` | 无 | 公开设置 |
| `/api/v1/opencode-config/*` | API Key | OpenCode 配置下发 |
| `/internal/v1/*` | Internal Token | 内部 API（gateway 调用） |
| `/healthz` | 无 | 健康检查 |
