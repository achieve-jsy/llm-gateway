---
name: test-deploy-ops
description: 测试环境 CI/CD 部署运维指南。用于排查测试环境部署失败、容器 unhealthy、数据库迁移缺失、环境变量配置等问题。当测试环境部署出现异常时使用此 skill。
---

# 测试环境部署运维 (test-deploy-ops)

## 1. 触发条件

**push 到 `test` 分支** 且变更包含以下路径之一时自动触发：

```
internal/**/*  cmd/**/*  go.mod  go.sum         # Go 后端
web/**/*                                        # Vue 前端
deploy/docker/**/*  docker-compose.yml          # 构建/部署配置
config/**/*                                     # 环境配置
atlas-migrations/**/*                           # 数据库迁移（Atlas）
.gitlab-ci.yml                                  # CI 配置变更
```

以下路径变更**不会**触发流水线（避免浪费资源）：

```
docs/**/*  skills/**/*  *.md  AGENTS.md  README.md
.gitignore  .env.example  .opencode/**/*
```

全流程（stage 顺序：`build → migrate → deploy → verify`）：

```
build-gateway-test       ─┐
build-control-plane-test ─┤─→ migrate-test ─→ deploy-app-test ─→ verify-test
build-web-test           ─┘                        │
                                            deploy-web-test ──┘
```

> **注意**：`changes:` 规则作用在每个 job 上，各 job 共享同一份变更清单。如果变更只涉及 docs/skills/README 等，整个流水线不会执行任何 job。如果纯配置变更（如 `docker-compose.yml`），所有 job 都会运行，但 deploy 拉镜像失败时不中断（fallback 到已有镜像）。
>
> **`migrate-test` 不受 `changes:` 限制**：只要 push 到 `test` 分支就会执行（规则仅 `if: $CI_COMMIT_BRANCH == "test"`），确保 schema 始终先于部署收敛。它通过 SSH 到测试主机本机执行 Atlas 迁移（见第 6 节）。

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
config/test.yaml（Viper，gateway 使用，APP_ENV=test）
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

## 6. 数据库迁移（Atlas，自动执行）

迁移已统一由 **Atlas** 管理，文件在 **`atlas-migrations/`** 目录（不再是 `migrations/`，也不再用编号 `NNN_*.up.sql` + 手动 psql）：

- `atlas-migrations/*.sql` — 版本化迁移（如 `20260609082623_baseline.sql`）
- `atlas-migrations/atlas.sum` — 校验和清单，改动迁移后必须同步（`atlas migrate hash`）
- 已执行版本记录在库内的 `atlas_schema_revisions` 表

### CI 自动迁移机制

`migrate` stage 在 `build` 之后、`deploy` 之前自动执行，**只要 push 到 `test` 分支就运行**（不受 `changes:` 限制）：

- **`migrate-test`**（test 分支）：Runner 内下载 atlas 二进制 → SSH 到测试主机 `192.0.2.10`，把 atlas 与 `atlas-migrations/` scp 到 `/tmp/atlas-migrate`，在**测试主机本机**执行 `atlas migrate apply`。
  > 之所以走 SSH：测试库地址是 `127.0.0.1:5432`（host 网络下的宿主机本地库），CI Runner 容器无法直连，必须在测试主机本机执行。
- **`migrate-prod`**（main 分支）：prod 库 `192.0.2.10:5432` 对 Runner 可路由，直接在 Runner 内执行。

迁移连接串从 `config/test.yaml` 的 `database.url` 解析（`postgres://...@127.0.0.1:5432/llm_gateway_test`）。

### 手动执行迁移（排查/补救）

正常无需手动操作；若 CI 失败需手动补迁移，**在测试主机上**执行（那里才能连到 `127.0.0.1:5432`）：

```bash
# 在测试主机 192.0.2.10 上
atlas migrate apply \
  --dir "file://atlas-migrations" \
  --url "postgres://llm_gateway_test:CHANGE_ME_PASSWORD@127.0.0.1:5432/llm_gateway_test?sslmode=disable"

# 只查看待应用的迁移、不实际执行
atlas migrate status \
  --dir "file://atlas-migrations" \
  --url "postgres://llm_gateway_test:CHANGE_ME_PASSWORD@127.0.0.1:5432/llm_gateway_test?sslmode=disable"
```

### 常见报错

| 错误 | 含义 | 处理 |
|------|------|------|
| `dial tcp 127.0.0.1:5432: connect: connection refused` | 在 CI Runner 内直连测试库（错误），或测试主机 PG 未起 | 迁移须在测试主机本机执行（`migrate-test` 已走 SSH）；检查宿主机 PG |
| `checksum mismatch` / `atlas.sum` 校验失败 | 改了迁移文件但没更新 `atlas.sum` | 本地 `atlas migrate hash` 后重新提交 |
| `connected database is not clean` | 目标库已有非 Atlas 管理的表 | 用 `atlas migrate apply --baseline <version>` 设定基线 |
| `column xxx does not exist`（应用日志） | 迁移未跑到对应版本 | 在测试主机执行 `atlas migrate apply` 收敛 |

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
| `WRONGPASS invalid username-password pair` | Redis 密码错误 | 更新 `docker-compose.yml` 和 `config/test.yaml` 中的 `REDIS_PASSWORD` |
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
export IMAGE_TAG=test-<commit_sha> REGISTRY=192.0.2.10:5000 APP_ENV=test
docker compose -p llm-test up -d --remove-orphans
docker compose -p llm-test ps
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
| `.gitlab-ci.yml` | CI 流水线定义，测试分支自动触发（含 `migrate-test`） |
| `docker-compose.yml` | 部署配置，`network_mode: host`，所有环境变量默认值 |
| `config/test.yaml` | 测试环境 Viper 配置（gateway 读取，control-plane 部分读取）；migrate 的库连接串也从此解析 |
| `atlas-migrations/` | Atlas 迁移文件 + `atlas.sum`，CI `migrate` stage 自动执行 |
| `cmd/control-plane/main.go` | 直接 `os.Getenv`，不走 Viper，环境变量必须注入 |
| `cmd/gateway/main.go` | 走 `config.Load()`（Viper），环境变量可覆盖 yaml |
