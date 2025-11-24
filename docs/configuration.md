# 配置体系

## 配置优先级

从高到低：

1. **环境变量**（如 `GATEWAY_PORT`、`DATABASE_URL`）
2. **YAML 配置文件**（`config/{APP_ENV}.yaml`）
3. **代码默认值**

## 环境选择

通过 `APP_ENV` 环境变量选择配置文件：

| APP_ENV | 配置文件 | 用途 |
|---------|----------|------|
| `dev`（默认）| `config/dev.yaml` | 本地开发 |
| `staging` | `config/staging.yaml` | 测试环境 |
| `prod` | `config/prod.yaml` | 生产环境 |

## 三层分离原则

| 层 | 内容 | 落位 |
|----|------|------|
| 非敏感、环境无关 | 业务默认值 | `config/{env}.yaml`（进镜像） |
| 环境相关 | 端口、URL、库名、镜像 tag | 目标机 `/opt/llm-gateway/.env`（CI 生成） |
| **密钥** | DB/Redis 密码、`JWT_SECRET`、`ENCRYPTION_KEY`、OAuth secret、`INTERNAL_TOKEN` | **GitLab CI/CD Variables（Masked + Protected）** → CI 生成 `.env`，**绝不进 Git/镜像** |

## YAML 配置结构

### Gateway（`internal/gateway/config/config.go`）

```yaml
server:
  port: "8080"                    # 监听端口

redis:
  addr: "localhost:6379"          # Redis 地址
  password: ""                    # Redis 密码
  db: 0                           # Redis DB

otlp:
  endpoint: "localhost:4317"      # OTel Collector 地址
  enabled: false                  # 是否启用追踪

provider:
  internal_token: "xxx"           # 内部 API Token
  control_plane_url: "http://127.0.0.1:8081"  # 控制面地址

auth:
  url: "http://127.0.0.1:8081"    # 鉴权服务地址
  token: "xxx"                    # 内部 Token

gateway:
  url: "http://127.0.0.1:8080"    # Gateway 自身地址（用于回调）
```

### Control-Plane（`internal/controlplane/config/config.go`）

```yaml
controlplane:
  port: "8081"                    # 监听端口

database:
  url: "host=localhost port=5432 user=postgres password=xxx dbname=llm_gateway sslmode=disable"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

oauth:
  issuer_url: "https://oauth.example.com/"
  client_id: "xxx"
  client_secret: "xxx"
  redirect_uri: "https://app.example.com/auth/callback"
  scopes: "openid profile email"

jwt:
  secret: "change-in-production"  # JWT 签名密钥

frontend:
  url: "http://localhost:5173"    # 前端地址（CORS）
```

## 环境变量清单

### 核心配置

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `APP_ENV` | 环境选择 | `dev` |
| `GATEWAY_PORT` | Gateway 端口 | `8080` |
| `CONTROL_PLANE_PORT` | Control-Plane 端口 | `8081` |

### 数据库

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DATABASE_URL` | 完整 DSN（优先） | - |
| `DB_HOST` | 数据库主机 | `localhost` |
| `DB_PORT` | 数据库端口 | `5432` |
| `DB_USER` | 数据库用户 | `postgres` |
| `DB_PASSWORD` | 数据库密码 | `postgres` |
| `DB_NAME` | 数据库名 | `llm_gateway` |
| `DB_SSLMODE` | SSL 模式 | `disable` |

### Redis

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` |
| `REDIS_PASSWORD` | Redis 密码 | - |
| `REDIS_DB` | Redis DB | `0` |

### 认证

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `JWT_SECRET` | JWT 签名密钥 | - |
| `INTERNAL_AUTH_TOKEN` | 内部 API Token | - |
| `ENCRYPTION_KEY` | AES 加密密钥（32字节） | - |

### OAuth

| 变量 | 说明 |
|------|------|
| `OAUTH_ISSUER_URL` | OIDC Issuer URL |
| `OAUTH_CLIENT_ID` | OAuth Client ID |
| `OAUTH_CLIENT_SECRET` | OAuth Client Secret |
| `OAUTH_REDIRECT_URI` | OAuth 回调 URL |
| `OAUTH_SCOPES` | OAuth Scopes |

### 可观测性

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `OTLP_ENABLED` | 启用追踪 | `false` |
| `OTLP_ENDPOINT` | OTel Collector | `localhost:4317` |

### 其他

| 变量 | 说明 |
|------|------|
| `FRONTEND_URL` | 前端地址（CORS） |
| `GATEWAY_URL` | Gateway 地址 |
| `CATALOG_REMOTE_URL` | LiteLLM Catalog 远程地址 |

## GitLab CI/CD Variables 配置

密钥必须配置在 GitLab CI/CD Settings → Variables，启用 **Masked + Protected**：

| 变量名 | 说明 | 要求 |
|--------|------|------|
| `PROD_DATABASE_URL` | 生产数据库连接串 | Masked |
| `PROD_REDIS_PASSWORD` | 生产 Redis 密码 | Masked |
| `PROD_JWT_SECRET` | JWT 签名密钥 | Masked + Protected |
| `PROD_ENCRYPTION_KEY` | 数据加密密钥 | Masked + Protected |
| `PROD_OAUTH_CLIENT_SECRET` | OAuth 客户端密钥 | Masked + Protected |
| `PROD_INTERNAL_TOKEN` | 内部服务通信令牌 | Masked + Protected |
| `TEST_*` | 对应测试环境变量 | Masked |

## CI 部署时生成 .env

```bash
cat > /opt/llm-gateway/.env <<EOF
APP_ENV=prod
IMAGE_TAG=${CI_COMMIT_TAG}
GATEWAY_PORT=${TARGET_GW_PORT}
CP_PORT=${TARGET_CP_PORT}
DATABASE_URL=${PROD_DATABASE_URL}
JWT_SECRET=${PROD_JWT_SECRET}
ENCRYPTION_KEY=${PROD_ENCRYPTION_KEY}
EOF
chmod 600 /opt/llm-gateway/.env
docker compose --env-file .env -p llm-${TARGET_COLOR} up -d --wait
```

## 本地开发注入

### 方式一：.env 文件

项目根目录创建 `.env` 文件（已在 `.gitignore` 中）：

```bash
# .env
DATABASE_URL="host=192.0.2.10 port=5432 user=llm_gateway password=xxx dbname=llm_gateway sslmode=disable"
REDIS_ADDR="192.0.2.10:6379"
REDIS_PASSWORD="xxx"
JWT_SECRET="dev-secret"
INTERNAL_AUTH_TOKEN="dev-token"
```

两个服务的 `main.go` 都会调用 `godotenv.Load()` 自动加载。

### 方式二：direnv

安装 [direnv](https://direnv.net/) 后创建 `.envrc`：

```bash
# .envrc
export DATABASE_URL="..."
export REDIS_ADDR="..."
```

执行 `direnv allow` 后自动注入当前 shell。

### 方式三：IDE 配置

在 GoLand/VSCode 的 Run Configuration 中配置 Environment Variables。

## Docker Compose 引用

在 `docker-compose.yml` 中引用环境变量：

```yaml
services:
  control-plane:
    environment:
      DATABASE_URL: '${DATABASE_URL}'
      REDIS_ADDR: '${REDIS_ADDR}'
      JWT_SECRET: '${JWT_SECRET}'
```

**重要**：compose 中**禁止**使用弱默认值 `${SECRET:-xxx}` 兜底，应改为 `${SECRET:?must be set}`（缺失即报错，杜绝静默弱密钥）。

## 配置文件搜索路径

Viper 按以下顺序搜索 `{APP_ENV}.yaml`：

1. `./config/`
2. `../config/`
3. `../../config/`

Docker 容器中 config 目录在 `COPY --from=builder /app/config ./config` 复制到工作目录。

## 密钥轮换

| 密钥 | 轮换难度 | 说明 |
|------|----------|------|
| DB/Redis 密码 | 低 | 改密码 + 重启即可 |
| `JWT_SECRET` | 低 | 轮换后旧 token 立即失效 |
| `INTERNAL_TOKEN` | 低 | 同步更新即可 |
| `ENCRYPTION_KEY` | **高** | 需配套历史数据重加密流程 |
| OAuth secret | 中 | 需在 OAuth 提供商同步更新 |

> ⚠️ 仓库历史已泄露的密钥（尤其 `ENCRYPTION_KEY`，关系到存量数据加解密）建议轮换。`ENCRYPTION_KEY` 轮换需单独评估。
