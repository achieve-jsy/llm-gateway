# 部署指南

## 架构拓扑

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

## 环境一览

| 环境 | 域名/地址 | Gateway | Control-Plane | 前端目录 |
|------|----------|---------|---------------|----------|
| **开发** | localhost:5173 | 8080 | 8081 | Vite dev server |
| **测试** | 192.0.2.10:8090 | 4010 | 4011 | /var/www/aimeter-test/dist/ |
| **生产** | aimeter.xk-devops.com | 8080 | 8081 | /data/web/aimeter/dist/ |

## CI/CD 流水线（GitLab CI）

### 触发规则

| 触发条件 | 目标环境 | 审批 |
|----------|----------|------|
| `test` 分支 push | 测试 (192.0.2.10) | 自动 |
| `v*.*.*` tag push | 生产 (192.0.2.10) | 手动 |

### 流水线阶段

```
test → build → deploy → verify
```

1. **test**：`go vet` + `go test`（当前已禁用）
2. **build**：多阶段 Docker 构建 → 推送到内部 Registry `192.0.2.10:5000`
3. **deploy**：SSH 到目标机 → `docker compose up -d`
4. **verify**：健康检查 `/health` 和 `/healthz`

### 构建产物

| 镜像 | Dockerfile | 构建参数 |
|------|------------|----------|
| `llm-gateway` | `deploy/docker/gateway.Dockerfile` | `APP_ENV=staging/prod` |
| `llm-control-plane` | `deploy/docker/control-plane.Dockerfile` | `APP_ENV=staging/prod` |
| `web-dist.tar.gz` | Node 构建 artifact | - |

## 容器部署

### Docker Compose

项目提供两个 compose 文件：

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

## Nginx 配置

### 路由规则

```nginx
# LLM Gateway API（OpenAI 兼容接口）
location /v1/ {
    proxy_pass http://gateway;
    proxy_buffering off;           # SSE streaming 支持
    proxy_read_timeout 300s;
}

# Control Plane 管理 API
location /api/ {
    proxy_pass http://control_plane;
}

# OAuth 认证
location /auth/ {
    proxy_pass http://control_plane;
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
| 测试 | `deploy/docker/nginx-test.conf` | 192.0.2.10:9080 |
| 生产 | `deploy/docker/nginx.conf` | HTTPS + 域名 |

### 生产部署

```bash
# 软链到 nginx 配置目录
ln -s /opt/llm-gateway/deploy/docker/nginx.conf /etc/nginx/conf.d/aimeter.conf
nginx -t && systemctl reload nginx
```

## 时区

容器内时区通过 Alpine `tzdata` 包支持，默认 UTC。如需修改：

```dockerfile
ENV TZ=Asia/Shanghai
```

或在 docker-compose 中：

```yaml
environment:
  TZ: Asia/Shanghai
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
