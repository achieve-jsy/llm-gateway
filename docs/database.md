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
| 测试 PG / Redis | **测试机 `191.120` 本机** | 同上 |
| 备份归档 | **运维机 `12.15`** | 各环境每日 `pg_dump` 推送，异机留存 |

```
运维 12.15: 备份归档（每日 pg_dump 推送）

生产 191.99:
  宿主 Nginx → blue/green 应用 → 本机 PG (127.0.0.1:5432)
                                → 本机 Redis (127.0.0.1:6379)

测试 191.120:
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
| `migration.sql` 文件 | `atlas-migrations/2026..._xxx.sql`（Atlas 版本化目录 + `atlas.sum` 校验和） |
| `_prisma_migrations` 表 | `atlas_schema_revisions` 表 |
| `prisma migrate deploy` | `atlas migrate apply` —— 按版本表从落后处补齐 |

### 开发者工作流（已实施）

**项目现已配置 Atlas 环境，开发者可使用简化命令执行迁移操作。**

#### 1. 修改 GORM model 后生成迁移

```bash
# 使用 make target 生成增量迁移（name 参数必须提供）
make atlas-diff name=add_user_email

# 等价于完整命令：
atlas migrate diff add_user_email --env local
```

生成的迁移文件会保存在 `atlas-migrations/` 目录，文件名格式为 `20260609123456_add_user_email.sql`。

#### 2. 审查生成的 SQL

检查新生成的迁移文件，确保：
- SQL 语句符合预期
- 使用了 `IF NOT EXISTS` / `IF EXISTS` 保证幂等性
- 遵循 Expand-Contract 原则（只加不减）

#### 3. 查看迁移状态

```bash
# 查看当前数据库迁移状态
make atlas-status

# 等价于：
atlas migrate status --env local
```

#### 4. 应用迁移到本地开发库

```bash
# 应用所有待执行的迁移
make atlas-apply

# 等价于：
atlas migrate apply --env local
```

#### 5. 提交迁移文件

审查通过后，将迁移文件提交到版本控制：

```bash
git add atlas-migrations/
git commit -m "feat(db): add user email column"
```

#### 常用命令速查

| 命令 | 说明 |
|------|------|
| `make atlas-diff name=xxx` | 生成迁移（name 必填） |
| `make atlas-apply` | 应用迁移到本地库 |
| `make atlas-status` | 查看迁移状态 |

> **注意**：
> - 迁移文件生成后必须人工审查，不要盲目执行
> - 本地开发使用 `--env local`，CI/CD 使用生产环境配置
> - Atlas 配置文件为 `atlas.hcl`，定义了 GORM loader 和环境配置

### 部署期工作流（CI/CD）

CI 在 `migrate` stage（deploy 之前）执行 `atlas migrate apply`，对比 `atlas_schema_revisions` 从落后版本顺序补齐；幂等、带锁、可重入。按环境网络可达性拆成两个 job：

```bash
# migrate-prod（main 分支）：prod 库可路由，Runner 内直接执行
atlas migrate apply \
  --dir "file://atlas-migrations" \
  --url "$DATABASE_URL" \
  --baseline "$ATLAS_BASELINE"

# migrate-test（test 分支）：test 库在测试机本机（127.0.0.1:5432），
# Runner 无法直连，故 SSH 到测试机本机执行同样的命令。
```

- `$DATABASE_URL` 从 `config/<env>.yaml` 的 `database.url` 解析。
- `$ATLAS_BASELINE`（=`atlas-migrations/<时间戳>_baseline.sql` 的时间戳）：线上库已有 schema，首次执行把该版本标记为已应用而不执行其 SQL；revision 表建立后再带此参数为无操作（幂等）。**切勿对空库使用**。
- 详见 [部署指南 · migrate 双 job](./deployment.md#migrate-双-job按环境拆分)。

> 开发期 `atlas migrate diff` 用 `--dev-url` 指向临时 dev 库做 schema 规范化与 diff，**不会动生产库**；diff 出来的 SQL 仍是人可审查的版本化文件，比 GORM `AutoMigrate` 安全得多。

### 备选方案对比

| 方案 | 声明式 diff | 版本表 | 回滚 | 生产安全 | 说明 |
|------|------------|--------|------|----------|------|
| **Atlas + GORM**（推荐） | ✅ 自动 | ✅ `atlas_schema_revisions` | ✅ | 高（diff 生成可审查 SQL） | 最贴近 Prisma，复用现有 model |
| golang-migrate | ❌ 手写 SQL | ✅ `schema_migrations` | ✅ `.down.sql` | 高 | 传统、稳、但要手写增量 |
| GORM `AutoMigrate` | ✅ 自动 | ❌ 无 | ❌ | **低**（只加不删、可能误判、不可控） | **不建议生产** |

### 迁移文件规范

1. **幂等性**：统一使用 `IF NOT EXISTS`、`IF EXISTS`
2. **命名**：由 Atlas 自动生成 `<时间戳>_description.sql`（如 `20260609082623_baseline.sql`），**不要手动改名**；改动迁移文件后须 `atlas migrate hash` 同步 `atlas.sum`
3. **基线（baseline）**：线上库已有数据和表，首次接管用 `atlas migrate apply --baseline <版本>` 把现状标成"已应用到该版本"（CI 已通过 `$ATLAS_BASELINE` 配置，幂等）
4. **执行位置**：CI `migrate` stage 独立一次性步骤，在 deploy 之前执行（不要让应用在启动时自行迁移）

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
