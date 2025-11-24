# 数据库

## 选型

| 项目 | 选型 |
|------|------|
| 数据库 | PostgreSQL |
| ORM | GORM v1.31.1 |
| 连接池 | MaxOpenConns=25, MaxIdleConns=10 |

> **Gateway 不直连数据库** — 这是核心架构约束。Gateway 只通过 Redis snapshot 和内部 HTTP API 获取数据。

## 部署位置

### 目标架构

| 数据 | 落位 | 持久化 |
|------|------|--------|
| 生产 PG / Redis | **生产机 `191.99` 本机** | `/data/pg`、`/data/redis` 绑定卷 |
| 测试 PG / Redis | **测试机 `191.121` 本机** | 同上 |
| 备份归档 | **运维机 `12.15`** | 各环境每日 `pg_dump` 推送，异机留存 |

```
运维 12.15: 备份归档（每日 pg_dump 推送）

生产 191.99:
  宿主 Nginx → blue/green 应用 → 本机 PG (127.0.0.1:5432)
                                → 本机 Redis (127.0.0.1:6379)

测试 191.121:
  宿主 Nginx → blue/green 应用 → 本机 PG (127.0.0.1:5432)
                                → 本机 Redis (127.0.0.1:6379)
```

- 新增 `docker-compose.db.yml`（每环境一套 PG + Redis），应用经 `127.0.0.1:5432` / `127.0.0.1:6379` 连本机。
- 收益：环境物理隔离、同机低延迟、运维机回归本职。

### 迁移步骤

1. 在目标机创建 `docker-compose.db.yml`，启动空 PG + Redis
2. 从 `12.15` 执行 `pg_dump` 生产库/测试库
3. 分别导入目标机新实例
4. 修改应用 DSN 指向 `127.0.0.1`
5. 验证功能正常
6. 下线 `12.15` 旧库实例

## 连接配置

优先级从高到低：

1. **环境变量 `DATABASE_URL`**（完整 DSN）
   ```
   host=localhost port=5432 user=postgres password=xxx dbname=llm_gateway sslmode=disable
   ```

2. **分项环境变量**
   ```bash
   DB_HOST=localhost
   DB_PORT=5432
   DB_USER=postgres
   DB_PASSWORD=xxx
   DB_NAME=llm_gateway
   DB_SSLMODE=disable
   ```

3. **YAML 配置**（`config/{APP_ENV}.yaml`）
   ```yaml
   database:
     url: "host=localhost port=5432 ..."
   ```

4. **代码默认值**：localhost:5432/postgres/postgres/llm_gateway/disable

## 迁移机制

### 选型：Atlas + GORM Provider

Go 生态里最贴近 Prisma 的是 **Atlas**。本项目已用 GORM，且 model 的 struct tag 完整，可直接把 **GORM model 当作 schema 单一事实源**：

| Prisma | Atlas（本项目） |
|--------|----------------|
| `schema.prisma`（声明式） | `internal/controlplane/model/*.go` 的 GORM 结构体（声明式 desired state） |
| `prisma migrate dev` | `atlas migrate diff` —— 对比 model 与已有迁移目录，**自动生成增量 SQL** |
| `migration.sql` 文件 | `migrations/2026..._xxx.sql`（Atlas 版本化目录 + `atlas.sum` 校验和） |
| `_prisma_migrations` 表 | `atlas_schema_revisions` 表 |
| `prisma migrate deploy` | `atlas migrate apply` —— 按版本表从落后处补齐 |

### 开发期工作流

```bash
# 1) 改 GORM model 后，自动 diff 出增量迁移
atlas migrate diff <变更名> \
  --dir "file://migrations" \
  --to "ent://internal/controlplane/model" \
  --dev-url "docker://postgres/16/dev?search_path=public"

# 2) 校验 + 查看将生成的 SQL
atlas migrate validate --dir "file://migrations"
```

### 部署期工作流

```bash
# 对比 atlas_schema_revisions，从落后版本开始顺序执行；幂等、带锁、可重入
atlas migrate apply \
  --dir "file://migrations" \
  --url "$DATABASE_URL" \
  --allow-dirty=false
```

> Atlas 用 `--dev-url` 指向一个临时 dev 库做 schema 规范化与 diff，**不会动生产库**；diff 出来的 SQL 仍是人可审查的版本化文件，比 GORM `AutoMigrate` 安全得多。

### 备选方案对比

| 方案 | 声明式 diff | 版本表 | 回滚 | 生产安全 | 说明 |
|------|------------|--------|------|----------|------|
| **Atlas + GORM**（推荐） | ✅ 自动 | ✅ `atlas_schema_revisions` | ✅ | 高（diff 生成可审查 SQL） | 最贴近 Prisma，复用现有 model |
| golang-migrate | ❌ 手写 SQL | ✅ `schema_migrations` | ✅ `.down.sql` | 高 | 传统、稳、但要手写增量 |
| GORM `AutoMigrate` | ✅ 自动 | ❌ 无 | ❌ | **低**（只加不删、可能误判、不可控） | **不建议生产** |

### 迁移文件规范

1. **幂等性**：统一使用 `IF NOT EXISTS`、`IF EXISTS`
2. **命名**：`NNN_description.sql`（编号三位数递增）
3. **基线（baseline）**：线上库已有数据和表，需用 `atlas migrate apply --baseline <版本>` 把现状标成"已应用到 N"
4. **执行位置**：CI 中独立一次性步骤，在切流量之前执行（不要让蓝绿双实例在启动时争抢迁移）

## Expand-Contract 模式

蓝绿期间新旧两版应用共享同一个库，**破坏性变更必须拆成多次发版**：

| 阶段 | 动作 | 例：列 `a` → `b` |
|------|------|------------------|
| Expand（本次） | 只加不减，新旧兼容 | 加列 `b`，应用双写 `a`/`b` |
| 回填 | 数据迁移 | `UPDATE ... SET b = a` |
| Contract（下次，确认稳定后） | 删旧 | `DROP COLUMN a` |

**禁止**单次发版：
- `DROP COLUMN`
- `RENAME COLUMN`
- 加无默认值的 `NOT NULL`
- 收窄类型

→ 写进开发规范并在 code review 卡口。

## 核心表结构

### 业务表（control-plane）

| 表名 | 用途 |
|------|------|
| `providers` | 厂商渠道配置（加密凭证、超时、重试） |
| `models` | 模型注册（上游ID、能力、价格、权重） |
| `model_groups` | 模型组（调度策略、failover 配置） |
| `group_models` | 组内模型关联 |
| `api_keys` | API Key（hash、加密存储、过期、权限） |
| `key_group_bindings` | Key↔组绑定（多对多） |
| `model_catalog` | LiteLLM 同步的能力/价格真源 |
| `provider_definitions` | 厂商定义注册表 |

### 可观测性表（gateway 写入）

| 表名 | 用途 |
|------|------|
| `call_logs` | 调用明细（身份/路由/Token/耗时/成本） |
| `call_traces` | 请求 Trace（请求/响应体 JSONB） |
| `usage_daily` | 按天聚合（每小时 rollup） |

### 其他表

| 表名 | 用途 |
|------|------|
| `departments` / `users` | OIDC 用户身份 |
| `dev_users` | 本地开发用户 |
| `system_configs` | 系统配置 KV |
| `health_check_configs/logs` | 健康检测 |
| `announcements` / `announcement_reads` | 系统公告 |

## 设计规范

### 主键策略

- 所有表使用 **UUID** 主键，`DEFAULT gen_random_uuid()`
- GORM 模型字段：`ID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`

### JSONB 字段

| 表 | 字段 | 用途 |
|---|---|---|
| `provider_definitions` | `credential_schema` | 凭证表单 schema |
| `provider_definitions` | `vendor_options` | 厂商特有选项 |
| `model_catalog` | `raw_payload` | LiteLLM 原始响应 |
| `call_traces` | `request_body` / `response_body` | 请求/响应体 |

### 软删除

仅 `model_groups` 表使用 GORM 软删除（`deleted_at` 字段）。其他表为硬删除。

### 价格单位

统一为 **USD per 1,000,000 tokens**（与 LiteLLM 一致）。

### 加密存储

| 表 | 字段 | 加密方式 |
|---|---|---|
| `providers` | `encrypted_credentials` | AES-256-GCM |
| `api_keys` | `encrypted_key` | AES-256-GCM |

密钥管理见 `encryption_keys` 表（DEK 版本轮换）。
