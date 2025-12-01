# 部署指南

> 适用范围: LLM Gateway 项目

## 环境拓扑

### 资源分布

| 角色 | IP | 职责 |
|------|----|------|
| 运维服务器 | `192.0.2.10` | GitLab Runner、镜像仓库（`:5000`）、备份归档 |
| 生产服务器 | `192.0.2.10` | 生产应用容器（gateway / control-plane） |
| 测试服务器 | `192.0.2.10` | 测试应用容器 |

### 系统组成

```
运维 12.15: GitLab Runner + Registry + 备份归档
生产 191.99: 宿主 Nginx → blue/green 应用 → 本机 PG/Redis
测试 191.120: 宿主 Nginx → blue/green 应用 → 本机 PG/Redis
```

- **gateway** (`:8080`)：对外 `/v1/*`，鉴权 → 模型组调度 → 转发上游。不直连 PG，配置来自 Redis snapshot / 控制面 HTTP。
- **control-plane** (`:8081`)：管理后台 API、OAuth、Catalog 同步、CRUD。持久化在 PostgreSQL。
- **web**：Vue3 SPA，构建产物由宿主机 Nginx 静态托管。
- 分流由宿主机 **Nginx** 完成：`/v1/*`→gateway，`/api/* /auth/*`→control-plane，`/*`→静态。

### 架构图

```
                     ┌─────────────────────────────────┐
                     │         宿主机 nginx            │
                     │   HTTPS + 路径反代 + 静态托管   │
                     └─────────────┬───────────────────┘
            ┌──────────────────────┼──────────────────────┐
            ▼                      ▼                      ▼
    /v1/* → gateway:8080    /api/* → cp:8081      /* → web/dist
            │                      │
            └────────┬─────────────┘
                     ▼
              ┌──────────────┐
              │    Redis     │  snapshot + pub/sub
              └──────────────┘
                     │
              ┌──────────────┐
              │  PostgreSQL  │  control-plane only
              └──────────────┘
```

## CI/CD 流水线

### 触发规则

| 触发条件 | 目标环境 | 审批 |
|----------|----------|------|
| `test` 分支 push | 测试 (192.0.2.10) | 自动 |
| `v*.*.*` tag push | 生产 (192.0.2.10) | 手动 |

### 目标流水线

```
push test / tag v* → build(镜像+web) → migrate(atlas apply) → deploy(blue-green.sh)
  → 健康检查+冒烟 → [通过] Nginx切流reload → 停旧色
                  → [失败] 销毁新色，线上不动
```

### 流水线阶段

```yaml
stages:
  - build
  - migrate      # 数据库迁移
  - deploy       # 蓝绿部署
  - verify       # 健康检查 + 冒烟
```

1. **build**：多阶段 Docker 构建 → 推送到内部 Registry `192.0.2.10:5000`
2. **migrate**：独立 job，在 deploy 之前执行 `atlas migrate apply`（见下方双 job 说明）
3. **deploy**：SSH 到目标机 `docker compose up -d` 拉起新镜像
4. **verify**：健康检查 `/health` 和 `/healthz`

### migrate 双 job（按环境拆分）

迁移连接串从对应环境的 `config/<env>.yaml` 的 `database.url` 解析。因网络可达性不同，拆成两个 job：

| job | 触发分支 | Runner 镜像 | 连接方式 |
|-----|----------|-------------|----------|
| `migrate-prod` | `main` | `alpine:3.21`（Runner 下载 atlas） | prod 库 `192.0.2.10:5432` 对 Runner 可路由，**直接在 Runner 内**执行 |
| `migrate-test` | `test` | `deploy-tools:latest` | test 库地址是 `127.0.0.1:5432`（host 网络的宿主机本地库），Runner 连不上，**SSH 到测试机本机**执行 |

> `migrate-test` 不受 `changes:` 限制，只要 push 到 `test` 就执行，确保 schema 先于部署收敛。

```yaml
# 直连（prod）：Runner 内执行
migrate-prod:
  stage: migrate
  image: alpine:3.21
  script:
    - export DATABASE_URL=$(grep -A1 ^database: config/prod.yaml | tail -1 | cut -d'"' -f2)
    - atlas migrate apply --dir "file://atlas-migrations" --url "$DATABASE_URL" --baseline "$ATLAS_BASELINE"

# SSH（test）：把 atlas 二进制 + atlas-migrations/ scp 到测试机，本机执行
migrate-test:
  stage: migrate
  image: ${REGISTRY}/deploy-tools:latest
  script:
    - DATABASE_URL=$(grep -A1 ^database: config/test.yaml | tail -1 | cut -d'"' -f2)
    - scp atlas-bin/atlas atlas-migrations ${TEST_USER}@${TEST_HOST}:/tmp/atlas-migrate/
    - ssh ${TEST_USER}@${TEST_HOST} "/tmp/atlas-migrate/atlas migrate apply
        --dir 'file:///tmp/atlas-migrate/atlas-migrations' --url '$DATABASE_URL' --baseline '$ATLAS_BASELINE'"
```

> **`--baseline $ATLAS_BASELINE`**（=`atlas-migrations/<时间戳>_baseline.sql` 的时间戳）：线上库已有 schema，首次执行时把该版本标记为已应用且不执行其 SQL；revision 表建立后再带此参数为无操作（幂等）。**切勿对空库使用**，否则不会建表。

### 构建产物

| 镜像 | Dockerfile | 构建参数 |
|------|------------|----------|
| `llm-gateway` | `deploy/docker/gateway.Dockerfile` | `APP_ENV=test/prod` |
| `llm-control-plane` | `deploy/docker/control-plane.Dockerfile` | `APP_ENV=test/prod` |
| `web-dist.tar.gz` | Node 构建 artifact | - |

## 蓝绿部署

### 方案概述

单机双栈：同机两套 compose project（blue/green）+ 不同端口 + 宿主 Nginx upstream 原子切流。

```
宿主 Nginx upstream → active:  blue  (gw:8080  cp:8081)
                    → standby: green (gw:8082  cp:8083)
blue + green 共享本机 PG/Redis
```

### 发布顺序

```
migrate（Expand 兼容） → 起空闲色 → 健康检查/冒烟 → Nginx 切流 reload → 停旧色
                                           └─ 失败 → 销毁新色，线上不受影响
```

- `gateway` 已配 `stop_grace_period: 120s`，切流后再停旧色可优雅排空在途请求。

### docker-compose.yml 参数化

端口、project 名参数化（`GATEWAY_PORT`/`CP_PORT` 由 `.env` 注入）

### Nginx 切流配置

```nginx
# /etc/nginx/conf.d/llm-active.conf —— 部署脚本只改这两行
upstream gw_backend { server 127.0.0.1:8080; }   # 切换改 8082
upstream cp_backend { server 127.0.0.1:8081; }   # 切换改 8083
```

### deploy/blue-green.sh

判断空闲色 → 起新色 → 健康检查 + 冒烟 → 切 Nginx → 观察 → 停旧色；任意环节失败则销毁新色、流量不动。

### 回滚能力

| 场景 | 操作 | 影响 |
|------|------|------|
| 健康检查失败 | 销毁新色，旧色继续服务 | 零影响 |
| 切流后发现异常 | 改 `llm-active.conf` 指回旧色 + `nginx -s reload` | 秒级回滚 |
| 数据库迁移问题 | Expand-Contract 保证向后兼容，旧版应用仍可读写 | 可安全回切旧色 |

## 容器部署

### Docker Compose

| 文件 | 用途 | 服务 |
|------|------|------|
| `docker-compose.yml` | 生产部署 | gateway + control-plane |
| `deploy/docker-compose.yml` | 带可观测性 | + otel-collector + jaeger |

### 启动命令

```bash
# 生产部署（仅核心服务）
docker compose up -d

# 带可观测性（Jaeger trace）
docker compose --profile observability up -d

# 查看状态
docker compose ps
```

### 资源限制

| 服务 | 内存限制 | 优雅停机超时 |
|------|----------|--------------|
| gateway | 1GB | 120s |
| control-plane | 512MB | 30s |

### 健康检查

| 服务 | 端点 | 检查方式 |
|------|------|----------|
| gateway | `/health` | `wget --spider` |
| control-plane | `/healthz` | `wget --spider` |

## 容器时区

容器镜像基于 Alpine，默认 **UTC**。设置为北京时间：

### Dockerfile 配置

```dockerfile
# gateway.Dockerfile / control-plane.Dockerfile（已装 tzdata）
ENV TZ=Asia/Shanghai
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

# web.Dockerfile（需额外安装 tzdata）
RUN apk add --no-cache tzdata
ENV TZ=Asia/Shanghai
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
```

### docker-compose.yml（可选）

```yaml
services:
  gateway:
    environment:
      TZ: Asia/Shanghai
  control-plane:
    environment:
      TZ: Asia/Shanghai
```

### 验证

```bash
docker exec <container> date
# 期望输出: Fri Jun 05 2026 15:30:00 CST  （北京时间，非 UTC）
```

> PostgreSQL 时间字段为 `TIMESTAMP WITH TIME ZONE`（存 UTC、按会话时区展示），属规范做法，默认不动。仅应用层日志/展示需北京时间。

## Nginx 配置

### 路由规则

```nginx
# LLM Gateway API（OpenAI 兼容接口）
location /v1/ {
    proxy_pass http://gw_backend;
    proxy_buffering off;           # SSE streaming 支持
    proxy_read_timeout 300s;
}

# Control Plane 管理 API
location /api/ {
    proxy_pass http://cp_backend;
}

# OAuth 认证
location /auth/ {
    proxy_pass http://cp_backend;
}

# OAuth 前端回调（SPA 路由）
location = /auth/callback {
    try_files /index.html =404;
}

# Vue SPA 回退
location / {
    try_files $uri $uri/ /index.html;
}
```

### 配置文件位置

| 环境 | 配置文件 | 说明 |
|------|----------|------|
| 开发 | `deploy/nginx-dev.conf` | macOS 本地开发 |
| 测试 | `deploy/docker/nginx-test.conf` | 192.0.2.10 |
| 生产 | `deploy/docker/nginx.conf` | HTTPS + 域名 |

### 生产部署

```bash
# 软链到 nginx 配置目录
ln -s /opt/llm-gateway/deploy/docker/nginx.conf /etc/nginx/conf.d/aimeter.conf
nginx -t && systemctl reload nginx
```

## 前端部署

前端为 Vue3 SPA，构建后为纯静态文件，由 nginx 直接托管。

```bash
# 构建
cd web && npm ci && npm run build

# 部署（将 dist/ 内容复制到目标目录）
tar -czf web-dist.tar.gz -C web/dist .
scp web-dist.tar.gz user@host:/tmp/
ssh user@host "tar -xzf /tmp/web-dist.tar.gz -C /var/www/aimeter/dist/"
```

## 本地开发

```bash
# 启动基础设施（Postgres + Redis）
make dev-infra

# 启动 Gateway
make dev

# 启动 Control-Plane（另一个终端）
make dev-cp

# 启动前端（另一个终端）
cd web && npm run dev

# 带可观测性（OTel + Jaeger + Langfuse）
make dev-infra-full
```

## 安全要点

- migrate 必须在切流量**之前**执行
- migrate 不可由蓝绿双实例在启动时争抢执行
- deploy 失败 → 销毁新色，旧色不受影响
- 密钥从 CI Variables 来，不写进仓库（详见 [配置体系](./configuration.md)）
