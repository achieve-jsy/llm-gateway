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

## CI/CD 注入

### GitLab CI

在 `.gitlab-ci.yml` 的 deploy 阶段通过 `export` 注入：

```yaml
deploy-prod:
  script:
    - |
      export APP_ENV="prod"
      export DATABASE_URL="$PROD_DATABASE_URL"
      export REDIS_ADDR="$PROD_REDIS_ADDR"
      docker compose up -d
```

敏感变量在 GitLab CI/CD Settings → Variables 中配置。

### Docker Compose

在 `docker-compose.yml` 中引用环境变量：

```yaml
services:
  control-plane:
    environment:
      DATABASE_URL: '${DATABASE_URL}'
      REDIS_ADDR: '${REDIS_ADDR}'
      JWT_SECRET: '${JWT_SECRET}'
```

## 配置文件搜索路径

Viper 按以下顺序搜索 `{APP_ENV}.yaml`：

1. `./config/`
2. `../config/`
3. `../../config/`

Docker 容器中 config 目录在 `COPY --from=builder /app/config ./config` 复制到工作目录。
