---
name: prod-blue-green-deploy
description: 生产环境蓝绿部署操作指南。用于执行生产发布（蓝绿切换）、回滚、首次迁移至蓝绿体系、排查部署失败。当用户需要发布生产环境、回滚生产版本、或排查生产部署问题时使用此 skill。
---

# 生产环境蓝绿部署 (prod-blue-green-deploy)

> 本 skill 供 AI 在「生产发布」「生产回滚」「首次迁移至蓝绿体系」和「排查生产部署故障」时遵循。关联 skill：`gitlab-cicd`（流水线/变量操作）、`atlas-db-migration`（数据库迁移）、`test-deploy-ops`（测试环境发布）。

---

## 0. 架构概览

```
                     nginx (:443 / :9080)
                          │
          include /opt/llm-gateway/upstream-active.conf
                          │
          ┌───────────────┼───────────────┐
          ▼ (active)                      ▼ (standby)
   ┌─────────────────┐            ┌─────────────────┐
   │ BLUE             │            │ GREEN            │
   │ gw:8080 cp:8081  │            │ gw:8082 cp:8083  │
   │ Redis DB 2       │            │ Redis DB 3       │
   └─────────────────┘            └─────────────────┘
          │                              │
          └──────── PostgreSQL (共享) ────┘
```

- **BLUE**：gateway:8080 + control-plane:8081 + Redis DB 2
- **GREEN**：gateway:8082 + control-plane:8083 + Redis DB 3
- **隔离**：Redis DB 分开（key 不互通），PostgreSQL 共享（预期行为）
- **切换**：nginx reload（毫秒级），旧环境 drain 600s 让 SSE 长连接自然结束

---

## 1. 生产部署路径

### 1.1 常规发布（CI/CD 自动化，推荐）

```bash
# 1. 确保代码已在 test 分支验证通过
# 2. 打 tag
git tag v1.x.x
git push origin v1.x.x

# 3. GitLab CI 自动触发：
#    build-gateway-prod → build-control-plane-prod → build-web-prod
#    ↓
#    migrate-prod（手动点击执行）
#    ↓
#    deploy-prod（手动点击执行，内含蓝绿切换 + 600s drain）
#    deploy-web-prod（手动点击执行，前端静态文件）
```

CI 的 `deploy-prod` job 会：
1. SCP 蓝绿文件到 192.0.2.10:/opt/llm-gateway/
2. docker pull 新版镜像
3. 执行 `deploy-blue-green.sh`（自动判断颜色、启动、健康检查、nginx 切换、drain、停旧环境）

**Job timeout = 20 分钟**（drain 600s + 启动/检查开销）。

### 1.2 手动发布（紧急修复/调试用）

SSH 到生产机直接执行：

```bash
ssh root@192.0.2.10 "
  set -e
  cd /opt/llm-gateway
  export IMAGE_TAG=<目标版本tag>
  export REGISTRY=192.0.2.10:5000
  export APP_ENV=prod
  export DATABASE_URL='host=192.0.2.10 port=5432 user=llm_gateway password=CHANGE_ME_PASSWORD dbname=llm_gateway_prod sslmode=disable'
  export REDIS_ADDR=192.0.2.10:6379
  export REDIS_PASSWORD=CHANGE_ME_REDIS_PASSWORD
  export REDIS_DB=2
  export JWT_SECRET=d8f7a3b2c1e9f4567890abc123def456
  export ENCRYPTION_KEY=12345678901234567890123456789012
  export INTERNAL_AUTH_TOKEN=CHANGE_ME_INTERNAL_TOKEN
  export OAUTH_CLIENT_ID=5514f6bb0a
  export OAUTH_CLIENT_SECRET=CHANGE_ME_OAUTH_SECRET
  export OAUTH_REDIRECT_URI=https://aimeter.xk-devops.com/auth/callback
  export OAUTH_ISSUER_URL=https://oauth.example.net/
  export FRONTEND_URL=https://aimeter.xk-devops.com
  bash deploy-blue-green.sh
"
```

---

## 2. 回滚

### 2.1 CI 回滚（推荐）

在 GitLab 该 tag 的 pipeline 中，手动点击 `rollback-prod` job。

### 2.2 手动回滚（紧急）

```bash
ssh root@192.0.2.10 "cd /opt/llm-gateway && bash deploy-blue-green.sh --rollback"
```

回滚流程：
1. 读取当前 active 颜色
2. 健康检查对端环境（必须还在运行）
3. nginx reload 切回对端
4. 等待 600s drain
5. 停掉原 active 环境

**注意**：回滚要求对端容器仍在运行。如果旧环境已被 stop，则无法回滚，需走手动发布流程。

---

## 3. 首次迁移（从旧 docker-compose 到蓝绿体系）

> 仅首次切换时执行一次，之后不需要。

### 前提条件

- [x] `/opt/llm-gateway/` 下已有蓝绿文件
- [x] nginx 配置已改为 `include /opt/llm-gateway/upstream-active.conf;`
- [x] GitLab Protected Tags 已设置 `v*`
- [x] GitLab CI/CD 变量 `PROD_*` 已创建

### 执行步骤

```bash
ssh root@192.0.2.10 "
  set -e

  # 1. 停旧容器（会有 15-30 秒停机）
  echo '=== 停止旧容器 ==='
  docker stop llm-gw-gateway llm-gw-control-plane
  docker rm llm-gw-gateway llm-gw-control-plane

  # 2. 启动蓝绿
  echo '=== 启动蓝绿部署 ==='
  cd /opt/llm-gateway
  export IMAGE_TAG=latest
  export REGISTRY=192.0.2.10:5000
  export APP_ENV=prod
  export DATABASE_URL='host=192.0.2.10 port=5432 user=llm_gateway password=CHANGE_ME_PASSWORD dbname=llm_gateway_prod sslmode=disable'
  export REDIS_ADDR=192.0.2.10:6379
  export REDIS_PASSWORD=CHANGE_ME_REDIS_PASSWORD
  export REDIS_DB=2
  export JWT_SECRET=d8f7a3b2c1e9f4567890abc123def456
  export ENCRYPTION_KEY=12345678901234567890123456789012
  export INTERNAL_AUTH_TOKEN=CHANGE_ME_INTERNAL_TOKEN
  export OAUTH_CLIENT_ID=5514f6bb0a
  export OAUTH_CLIENT_SECRET=CHANGE_ME_OAUTH_SECRET
  export OAUTH_REDIRECT_URI=https://aimeter.xk-devops.com/auth/callback
  export OAUTH_ISSUER_URL=https://oauth.example.net/
  export FRONTEND_URL=https://aimeter.xk-devops.com
  bash deploy-blue-green.sh
"
```

首次部署时脚本检测到无蓝绿容器 → 自动选 BLUE → 启动 → 健康检查 → 切 nginx → 完成（无 drain）。

### 验证

```bash
curl -sf https://aimeter.xk-devops.com/health        # gateway
curl -sf https://aimeter.xk-devops.com/api/v1/settings/public  # control-plane
```

---

## 4. 部署脚本详解 (deploy-blue-green.sh)

### 流程

```
1. 读取 .active-color 文件（或自动检测）
2. 确定目标颜色（blue↔green 交替）
3. docker compose -f docker-compose.{target}.yml -p llm-{target} up -d
4. 健康检查（最多 90s：30 次 × 3s 间隔）
5. cp upstream-{target}.conf → upstream-active.conf
6. nginx -t && nginx -s reload
7. 保存状态到 .active-color
8. sleep 600（drain 旧环境的 SSE 长连接）
9. docker compose -p llm-{old} down --timeout 600
```

### 关键参数

| 参数 | 值 | 说明 |
|------|-----|------|
| DRAIN_TIMEOUT | 600s | 等待存量 SSE 流结束 |
| HEALTH_RETRIES | 30 | 健康检查重试次数 |
| HEALTH_INTERVAL | 3s | 每次重试间隔 |
| stop_grace_period | 600s | 容器收到 SIGTERM 后的等待时间 |

### 状态文件

- `/opt/llm-gateway/.active-color`：记录当前活跃颜色（`blue` 或 `green`）
- `/opt/llm-gateway/upstream-active.conf`：当前生效的 nginx upstream

---

## 5. 文件清单

```
/opt/llm-gateway/                    ← 生产机部署目录
├── deploy-blue-green.sh             ← 部署/回滚入口脚本
├── docker-compose.blue.yml          ← BLUE: gw:8080, cp:8081, Redis DB 2
├── docker-compose.green.yml         ← GREEN: gw:8082, cp:8083, Redis DB 3
├── upstream-active.conf             ← nginx include 的文件（脚本自动覆写）
├── upstream-blue.conf               ← upstream 模板
├── upstream-green.conf              ← upstream 模板
├── nginx-prod.conf.example          ← nginx 完整配置参考
├── .active-color                    ← 状态文件（脚本自动维护）
└── docker-compose.yml               ← 旧配置（迁移后可删）

/etc/nginx/sites-available/
└── example-ai-meter.conf           ← nginx 主配置
    └── include /opt/llm-gateway/upstream-active.conf;
```

源码位置：`deploy/blue-green/`

---

## 6. 环境隔离设计

| 资源 | BLUE | GREEN | 说明 |
|------|------|-------|------|
| Gateway 端口 | 8080 | 8082 | 环境变量 GATEWAY_PORT |
| CP 端口 | 8081 | 8083 | 环境变量 CONTROL_PLANE_PORT |
| Redis DB | 2 | 3 | key 隔离，snapshot 不互通 |
| Redis pub/sub | 跨 DB 共享 | 跨 DB 共享 | 事件会跨 DB 传播但 key 读取隔离，无实际影响 |
| PostgreSQL | 共享 | 共享 | 预期行为，数据统一 |
| JWT Secret | 共享 | 共享 | 用户无需重新登录 |
| GATEWAY_URL | http://127.0.0.1:8080 | http://127.0.0.1:8082 | CP 健康检查指向自己的 gateway |

---

## 7. 故障排查

### 7.1 部署脚本报错

| 错误 | 原因 | 解决 |
|------|------|------|
| `failed health check after 90s` | 新容器启动失败 | `docker logs llm-{color}-gateway` 查看原因 |
| `nginx config test failed` | upstream 文件语法错误 | `cat /opt/llm-gateway/upstream-active.conf` 检查 |
| `port already in use` | 旧容器未停/端口被占 | `ss -tlnp \| grep 808` 定位占用进程 |
| 权限不足 | 非 root 执行 | 必须 root 或 sudo |

### 7.2 部署后服务异常

| 现象 | 排查 |
|------|------|
| 502 Bad Gateway | 新容器挂了：`docker ps` 确认状态 |
| 请求超时 | nginx upstream 指向了错误端口：`cat /opt/llm-gateway/upstream-active.conf` |
| 管理面登录失败 | JWT_SECRET 不一致：检查环境变量 |
| 模型调用失败 | Provider snapshot 未初始化：等 CP 启动后自动刷新（约 10s） |
| 旧 SSE 流中断 | drain 时间不够或旧容器被过早 kill：检查 stop_grace_period |

### 7.3 回滚失败

| 错误 | 原因 | 解决 |
|------|------|------|
| `is not running, cannot rollback` | 对端容器已停 | 只能手动发布：走 §1.2 |
| 回滚后仍有问题 | 数据库已被新版本迁移改动 | 需要数据库回滚（谨慎操作） |

### 7.4 查看当前状态

```bash
ssh root@192.0.2.10 "
  echo '=== Active Color ==='
  cat /opt/llm-gateway/.active-color 2>/dev/null || echo 'none'
  echo ''
  echo '=== Running Containers ==='
  docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}' | grep llm
  echo ''
  echo '=== Upstream Config ==='
  cat /opt/llm-gateway/upstream-active.conf
  echo ''
  echo '=== Health Check ==='
  curl -sf http://localhost:8080/health && echo ' gw:8080 OK' || echo ' gw:8080 FAIL'
  curl -sf http://localhost:8082/health && echo ' gw:8082 OK' || echo ' gw:8082 FAIL'
  curl -sf http://localhost:8081/healthz && echo ' cp:8081 OK' || echo ' cp:8081 FAIL'
  curl -sf http://localhost:8083/healthz && echo ' cp:8083 OK' || echo ' cp:8083 FAIL'
"
```

---

## 8. GitLab CI/CD 变量（PROD_*）

| 变量 | protected | masked | 说明 |
|------|-----------|--------|------|
| `PROD_DATABASE_URL` | ✅ | ❌ | PG 连接串（含空格，不可 mask） |
| `PROD_REDIS_ADDR` | ✅ | ❌ | `192.0.2.10:6379` |
| `PROD_REDIS_PASSWORD` | ✅ | ✅ | Redis 密码 |
| `PROD_REDIS_DB` | ✅ | ❌ | `2`（BLUE 的 DB） |
| `PROD_JWT_SECRET` | ✅ | ✅ | JWT 签名密钥 |
| `PROD_ENCRYPTION_KEY` | ✅ | ✅ | 加密密钥 |
| `PROD_OAUTH_CLIENT_ID` | ✅ | ✅ | OAuth Client ID |
| `PROD_OAUTH_CLIENT_SECRET` | ✅ | ✅ | OAuth Client Secret |
| `PROD_INTERNAL_AUTH_TOKEN` | ✅ | ✅ | 内部认证 token |
| `PROD_OAUTH_REDIRECT_URI` | ✅ | ❌ | OAuth 回调 URL |
| `PROD_FRONTEND_URL` | ✅ | ❌ | 前端 URL |
| `PROD_OAUTH_ISSUER_URL` | ✅ | ❌ | OAuth Issuer URL |

所有 `protected=true` 变量仅在受保护 tag（`v*`）的 pipeline 中注入。

---

## 9. 铁律

1. **先迁移数据库，再部署代码**：`migrate-prod` 必须在 `deploy-prod` 之前执行。
2. **不要手动编辑 `.active-color`**：脚本自动维护，手动改会导致状态不一致。
3. **不要直接 `docker stop` 蓝绿容器**：用脚本的 drain 机制，否则会断开 SSE 流。
4. **回滚前确认对端还在运行**：drain 600s 后对端会被 stop，此时无法回滚。
5. **密钥轮换后同步更新 GitLab 变量和 `/opt/llm-gateway/` 的环境**。
6. **GREEN 用 Redis DB 3**：不要改回 DB 2，否则会跟 BLUE 冲突。
