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
测试 191.121: 宿主 Nginx → blue/green 应用 → 本机 PG/Redis
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
| `migrate-prod` | tag `v*.*.*`（手动） | `alpine:3.21`（Runner 下载 atlas） | prod 库 `192.0.2.10:5432` 对 Runner 可路由，**直接在 Runner 内**执行 |
| `migrate-test` | `test`（+`changes`） | `deploy-tools:latest` | test 库地址是 `127.0.0.1:5432`（host 网络的宿主机本地库），Runner 连不上，**SSH 到测试机本机**执行 |

> 两个 migrate job 的 `rules` 与对应 deploy job 完全一致：`migrate-test` = `deploy-app-test`（test 分支 + `changes` 实质变更），`migrate-prod` = `deploy-prod`（tag `v*.*.*` + 手动）。即「要部署时才迁移」，无实质变更的 push（如纯文档）不会触发迁移。

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

## 模型等级预算灰度顺序

1. 全部 Gateway 支持 v2 后部署，七个开关仅保留 `legacy_total_write_enabled=true`。
2. 开启 `master_enabled`、`accounting_enabled`、`shadow_enabled`，保持正数、静态零和 v2 总预算拦截关闭。
3. 从 `call_logs` 重建当前日/周/月计数；连续一个最长预算周期满足 `max(0.1%, $1/day)` 对账阈值。
4. 开启 `zero_block_enabled`。
5. 关闭影子并开启 `positive_enforcement_enabled`。
6. 开启 `v2_total_enforcement_enabled`，继续双写旧总预算一个完整周期。
7. 稳定后关闭 `legacy_total_write_enabled`，旧 key 按 TTL 自然过期。

回滚时先恢复影子，再关闭 v2 总预算拦截。除非静态配置错误，不关闭静态零；完全关闭等级功能使用 `master_enabled=false`。回滚不删除 v2 key，也不修改调用日志。

## 调用链正文（`/datalog`）运行手册

> 适用对象：启用正文直写（规格 §4.2 方案 C）的网关 —— `TRACE_BODY_ROOT` 指向
> `/datalog/trace-archive-v4`。蓝绿切换 / 回滚 / 功能降级另见 `deploy/blue-green/README.md`
> （那份剧本随发版落到宿主）。本节的每条判定都给出**可粘贴命令**。

### ⛔ 前置条件（缺一不可；不满足时下面的观察断言恒真）

1. **写侧已启用**：`TRACE_BODY_ROOT` **非空**（`deploy/blue-green/docker-compose.{blue,green}.yml`
   里 `# TRACE_BODY_ROOT: /datalog/trace-archive-v4` 那行**已取消注释**）；
2. **root 目录在场**：`/datalog/trace-archive-v4` 必须存在 —— `NewTraceSpooler` 在**构造期**
   用 `rootExists`（`internal/gateway/observability/spooler.go:144`）要求它存在。

**功能关闭时本节全部观察断言恒真（什么都没证明）**：`TRACE_BODY_ROOT` 为空 ⇒ `cmd/gateway/main.go:275`
的 `sink` 是 **nil** ⇒ `TraceSpooler` **根本不构造** ⇒ 根身份护栏（`checkRoot`）从不运行 ⇒
期望的 `root_unavailable` / `root-identity-mismatch` ERROR **永不产生**。

**位置（本项是门禁，不是功能关闭阶段能交付的验收）**：读侧就绪（D73 硬门）→ 启用写侧
（取消注释 `TRACE_BODY_ROOT` + 重建非活跃颜色容器）→ **立即执行**下面这一节。

### 撤掉挂载（闩锁）

判定：`checkRoot` 在每个 tick 复检根目录身份；tick 粒度 = `TRACE_BODY_FLUSH_INTERVAL`（生产 `1m`）
⇒ 撤掉挂载后 **≤1 个 tick**（≤1m）内闩锁：**不再写盘**、spool 不再出现新文件、此后每一行正文
记 `body_missing=true` 且四个定位键全 NULL。

观察（生产；假设被观察颜色 green、容器名 `llm-green-gateway`）：

```
docker logs llm-green-gateway --since 15m 2>&1 | grep -E 'trace-spool.*(root_unavailable|root-identity-mismatch)'
docker exec llm-green-gateway ls -l /datalog/trace-archive-v4/spool | tail    # 不应再出现新文件
docker logs llm-green-gateway --since 15m 2>&1 | grep -E 'body_missing_rows='
```

判定：日志出现 ERROR（`reason=root_unavailable` 或 `root-identity-mismatch`）**且** spool 不再有新文件。

⛔ **停止点 A：验证窗口内禁止切换蓝绿流量 —— 这是丢正文防护，不是「仅降级」**。从撤挂载到
恢复完成之间：**不得**运行 `deploy/blue-green/deploy-blue-green.sh`（含 `--rollback`）、**不得**
改 nginx 的 `NGINX_UPSTREAM` / `upstream-*.conf`、**不得**让任何业务请求落到被 `umount` 的颜色上；
窗口内必须有人值守（典型 ≤10 分钟）。被 `umount` 的颜色此刻正处在下面第 ② 种降级（丢正文）⇒
切流到它 = 按行丢正文。

### 两种降级不对称（承重口径，别记反）

- **① `sink == nil`**（功能关闭，或构造期失败）⇒ 正文走既有路径落 PG 的 jsonb 列 ⇒ **无损**；
- **② `sink != nil` 但 spool 写入失败 / 根身份闩锁**（例如挂载消失）⇒ 该行 `body_missing=true`、
  四个定位键全 NULL，正文**既不进 `/datalog` 也不回退 jsonb** ⇒ **丢正文**。

因此「把写侧关掉让正文回落 jsonb」是**事后补救动作**（关掉 `TRACE_BODY_ROOT` →
`up -d --force-recreate` 重建容器 ⇒ 从那一刻起恢复无损语义），**不是「切流无损」**：
闩锁窗口内已经写下的行**无法找回**，且这是一次**静默降级**（不报错、`body_ref_*` 全 NULL）
⇒ 必须记进上线记录并按事故复盘。

### 这个实验只在没有真实流量的地方做（实验本身在丢正文）

被 `umount` 的颜色进入闩锁后每一行正文都**永久丢失** ⇒ 只允许：
① **测试环境**（A3 专用回环实例，直接 `umount /srv/trace-a3`）；或
② 生产上**已确认没有任何真实流量**的颜色/窗口（已摘流 + 维护窗口）。

**执行前留证「流量为零」**：抄录窗口内落向该颜色端口（8080/8082）的 nginx access 请求计数为 0
（或抄录该颜色处于「非活跃且已摘流」的部署状态），并抄录闩锁前的 `body_missing` 基线计数。
**无法留证 ⇒ 不执行。**

**执行后留证「累计丢失行数」**：① 网关日志里 `body_missing_rows=<n>` 的增量（与 `spool_failures`
同一条汇总）；或 ②

```sql
SELECT count(*) FROM call_traces
 WHERE body_missing IS TRUE
   AND created_at >= '<umount 时刻>' AND created_at < '<恢复时刻>';
```

**期望增量 = 0**（已摘流）；非 0 ⇒ 实验在真实流量下执行了，必须按事故复盘并写明丢失行数，
且该次实验**不得**作为验收证据。抄录这些读数之前，先确认活跃颜色 `/health` 仍 200：
`curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9080/health`，且活跃颜色
（`docker logs llm-blue-gateway`）**无**相关 ERROR。

⛔ **停止点 B**：确认观察、活跃颜色、丢失行数（=0）全部符合预期后，才解除停止点 A。

### 换盘 / 改挂载点（`dev` / `fstype` 变化）⇒ 必须重启才能恢复写盘

身份基线是**进程启动时**取的。换盘或改挂载点后设备的 `dev` / `fstype` 变了 ⇒ 身份与基线
**永久**不匹配 ⇒ 网关会一直闩锁到**重启**为止 —— 这是**已知**形态，不是 bug（保守侧是闩锁：
响亮、可恢复）。处置二选一：① 蓝绿切到**对端颜色**（零宕机重启）；② 重启该颜色容器。
**必须重启后才能恢复写盘** —— 只重新挂载同一个路径不会重建身份基线。

### 关停：6s 排空上限

关停编排给「最后一批落盘」的预算是 **`traceSpoolCloseWait` = 6s**（`cmd/gateway/main.go`）。
**6s 内没排空** ⇒ 该批降级为 `body_missing=true`，并打一条 ERROR：

```
docker logs llm-blue-gateway --since 5m 2>&1 | grep -E 'trace-spool'
```

处置：确认那是**正常关停**的残余窗口（`traceSpoolCloseWait` 只用于避免关停死锁）而不是盘故障；
若反复出现，检查写入侧的关停 flush 是否被慢盘拖住。

### 功能关闭 = 逐字节相同（自证无损）

`TRACE_BODY_ROOT` 为空（功能关闭）时，写入路径与新部署之前**逐字节相同**：`body_ref_*` 四列全 NULL、
`body_missing=false`、正文出现在 jsonb 列。自证 SQL（可粘贴）：

```sql
-- ① body_ref_part IS NULL AND body_missing = false 的行数应等于总行数
SELECT count(*) FILTER (WHERE body_ref_part IS NULL AND body_missing = false) AS plain_rows,
       count(*) AS total_rows
  FROM call_traces
 WHERE ts >= :off_since;
-- ② 正文出现在 jsonb 列
SELECT count(*) AS jsonb_rows
  FROM call_traces
 WHERE ts >= :off_since AND request_body IS NOT NULL;
```

### 保留口径与空间预算

保留口径由 `minAge 3h` 起步：未封块 spool 约 `3h × 写入速率`，再加上**已封产物**与余量。
当前生产基线（**2026-10-08**：`/datalog` **407G 已用 / 153G 可用** —— 引用它时必须标注这个日期，
空间会变）；余量检查命令：

```
df -h /datalog
```

余量判据：剩余空间 ≥ **5 天保留量**（见 `deploy/blue-green/README.md` 的硬门条件③）；
低于该线时先降 `TRACE_BODY_BLOCK_MB` 或摘流，不要让盘满自行闩锁。

### sealer 侧：心跳、`too_many_rows` 与隔离件

- **心跳（轮次结束）**：`trace-sealer` 每轮结束打一条 `trace-sealer round`
  （`--poll-interval 5m`，见 `deploy/systemd/trace-sealer.service`）。
  **>3×poll（= 15m）没有这条日志 ⇒ 告警**：

  ```
  journalctl -u trace-sealer --since '-20m' | grep -c 'trace-sealer round'   # 期望 ≥ 3
  ```

- **`too_many_rows`**：一块的行数越过 `MaxTrailerEntries` ⇒ sealer 保留 spool、不产生产物，
  轮次汇总里出现 `reason=too_many_rows`（ERROR 级）。人工动作：**拆块**、或**调小
  `TRACE_BODY_BLOCK_MB` 并重新标定**（不要只调大块大小把问题推远）：

  ```
  journalctl -u trace-sealer --since '-6h' | grep -E 'too_many_rows|trace-sealer 轮次告警'
  ```

- **隔离件**：写失败后当前块被毒化隔离，spool 目录里出现 `part-NNNNNN.jsonl.zst` —— 后缀是骗人的，
  内容是**明文** JSONL chunks。读侧两条精确路径都不命中它 ⇒ 一律报 `ErrNotArchived`（500 类
  「元数据与存储不一致」，需人工介入），**不得**把它当产物去解码。处置：**不得**原地删除
  （它是唯一现场）⇒ 先 `cp` 到 root 之外备份，再按事故复盘；是否需要抢救其中已完整写的行，
  由复盘决定。

### 禁止事项（逐条）

- 不得在功能开启期间停止 `trace-sealer`（未封 spool 必须继续被封）；
- 不得在验证窗口内切换蓝绿流量（见停止点 A / B）；
- 不得把 `TRACE_BODY_*` 放进全局 `config/prod.yaml`（两个颜色会写进同一个 `inst=` 目录）。
