---
name: test-deploy-ops
description: 测试环境 CI/CD 部署运维指南。用于排查测试环境部署失败、容器 unhealthy、数据库迁移缺失、环境变量配置等问题。当测试环境部署出现异常时使用此 skill。
---

# 测试环境部署运维 (test-deploy-ops)

## 1. 触发条件

**push 到 `test` 分支** 自动触发全流程：

```
build-gateway-test    ─┐
build-control-plane-test ─┤─→ deploy-app-test → deploy-web-test → verify-test
build-web-test        ─┘
```

## 2. 关键参数

| 参数 | 值 |
|------|-----|
| 测试服 IP | `192.0.2.10` |
| 部署目录 | `/data/llm-gateway` |
| Web 目录 | `/data/llm-gateway/web` |
| Gateway 端口 | `8080` |
| Control Plane 端口 | `8081` |
| Nginx 端口 | `9080` |
| Registry | `192.0.2.10:5000` |

## 3. 架构说明

**`network_mode: host`** — 容器直接使用宿主机网络栈：
- 容器内 `127.0.0.1` = 宿主机本地，PG/Redis 可直连
- 端口直接暴露在宿主机上，无需 `ports` 映射
- 容器间互访用 `127.0.0.1`（不用 docker DNS）
- Nginx 直接 `proxy_pass http://127.0.0.1:8080`

## 4. 配置来源优先级

```
环境变量（docker-compose environment）
    ↓ 覆盖
config/staging.yaml（Viper，gateway 使用）
    ↓ 覆盖
代码默认值
```

**重要**：`control-plane` 的 `main.go` 直接调用 `os.Getenv`，**不经过 Viper**。
所有业务配置必须通过 `docker-compose.yml` 的 `environment` 传入容器。

以下变量通过 `os.Getenv` 直接读取（必须作为环境变量传入）：

| 变量 | 读取位置 |
|------|----------|
| `DATABASE_URL` | `infra/postgres.go` |
| `REDIS_PASSWORD` | `infra/redis.go` |
| `JWT_SECRET` | `middleware/middleware.go` |
| `ENCRYPTION_KEY` | `pkg/crypto/manager.go` |
| `INTERNAL_AUTH_TOKEN` | `handler/group_handler.go` |
| `OAUTH_CLIENT_ID/SECRET/REDIRECT_URI/SCOPES` | `auth/config.go` |

## 5. 测试环境变量

### 数据库（宿主机本地 PostgreSQL）

```
DATABASE_URL=host=127.0.0.1 port=5432 user=llm_gateway_test password=CHANGE_ME_PASSWORD dbname=llm_gateway_test sslmode=disable
```

### Redis（宿主机本地 Redis）

```
REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=redis_bGNhwx
REDIS_DB=0
```

### 认证 / 加密

```
JWT_SECRET=d8f7a3b2c1e9f4567890abc123def456
ENCRYPTION_KEY=12345678901234567890123456789012
INTERNAL_AUTH_TOKEN=dev-test-token
```

### OAuth

```
OAUTH_ISSUER_URL=https://oauth.example.net/
OAUTH_CLIENT_ID=5514f6bb0a
OAUTH_CLIENT_SECRET=CHANGE_ME_OAUTH_SECRET
OAUTH_REDIRECT_URI=http://192.0.2.10:9080/auth/callback
OAUTH_SCOPES=openid profile email
FRONTEND_URL=http://192.0.2.10:9080
```

## 6. 数据库迁移

迁移文件在 `migrations/` 目录，**没有自动运行机制**，需手动执行。

### 检查当前版本

在测试服上查询已执行的最高迁移号：

```sql
-- 如果有迁移记录表
SELECT MAX(version) FROM schema_migrations;
-- 或直接检查某列是否存在
SELECT column_name FROM information_schema.columns
WHERE table_name='models' AND column_name='price_mode';
```

### 手动执行缺失迁移

```bash
# 连接测试库
psql "host=127.0.0.1 port=5432 user=llm_gateway_test password=CHANGE_ME_PASSWORD dbname=llm_gateway_test"

# 执行缺失的迁移（按序号顺序）
\i /path/to/migrations/035_price_mode.up.sql
```

或通过 psql 命令行直接执行：

```bash
psql "host=127.0.0.1 port=5432 user=llm_gateway_test password=CHANGE_ME_PASSWORD dbname=llm_gateway_test" \
  -f migrations/035_price_mode.up.sql
```

### 常见报错

| 错误 | 含义 | 处理 |
|------|------|------|
| `column models.price_mode does not exist` | 迁移未执行到 035 | 手动跑 `035_price_mode.up.sql` |
| `relation "xxx" does not exist` | 建表迁移未执行 | 从头按序号执行迁移 |
| `already exists` | 迁移重复执行 | 忽略，或跳过该文件 |

## 7. 常见故障排查

### 容器 unhealthy

```bash
# 在测试服上查日志
docker logs llm-gw-gateway --tail 50
docker logs llm-gw-control-plane --tail 50
```

常见原因及处理：

| 错误关键词 | 原因 | 处理 |
|-----------|------|------|
| `WRONGPASS invalid username-password pair` | Redis 密码错误 | 更新 `docker-compose.yml` 和 `config/staging.yaml` 中的 `REDIS_PASSWORD` |
| `failed to connect to PostgreSQL` | DB 连不上或密码错 | 检查 `DATABASE_URL` |
| `INTERNAL_AUTH_TOKEN is missing` | 环境变量未传 | 检查 docker-compose `environment` 节 |
| `connection refused 127.0.0.1:6379` | Redis 未启动或未监听 | 在宿主机上 `systemctl status redis` |
| `column xxx does not exist` | 数据库迁移缺失 | 手动执行对应迁移文件 |

### 容器启动卡死（Recreate 耗时 2+ 分钟）

旧容器 `stop_grace_period` 过长导致。当前已设为 `30s`，如仍卡：

```bash
docker kill llm-gw-gateway llm-gw-control-plane
```

### 手动重新部署

```bash
cd /data/llm-gateway
export IMAGE_TAG=test-<commit_sha> REGISTRY=192.0.2.10:5000 APP_ENV=staging
docker compose up -d --remove-orphans
docker compose ps
```

## 8. 重启顺序

control-plane 先启动（写 Redis snapshot），gateway 后启动（读 snapshot）：

```bash
docker restart llm-gw-control-plane
sleep 5
docker restart llm-gw-gateway
```

## 9. 关键文件索引

| 文件 | 职责 |
|------|------|
| `.gitlab-ci.yml` | CI 流水线定义，测试分支自动触发 |
| `docker-compose.yml` | 部署配置，`network_mode: host`，所有环境变量默认值 |
| `config/staging.yaml` | 测试环境 Viper 配置（gateway 读取，control-plane 部分读取） |
| `migrations/` | SQL 迁移文件，需手动执行 |
| `cmd/control-plane/main.go` | 直接 `os.Getenv`，不走 Viper，环境变量必须注入 |
| `cmd/gateway/main.go` | 走 `config.Load()`（Viper），环境变量可覆盖 yaml |
