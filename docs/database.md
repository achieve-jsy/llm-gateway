# 数据库

## 数据库选型

| 项目 | 选型 |
|------|------|
| 数据库 | PostgreSQL |
| ORM | GORM v1.31.1 |
| 连接池 | MaxOpenConns=25, MaxIdleConns=10 |

## 部署位置

- **开发/测试**：192.0.2.10:5432
- **生产**：内部网络，通过 `DATABASE_URL` 环境变量注入

> **Gateway 不直连数据库** — 这是核心架构约束。Gateway 只通过 Redis snapshot 和内部 HTTP API 获取数据。

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

### 当前状态

**项目暂无自动化迁移工具**。迁移文件位于 `migrations/` 目录，需手动执行。

### 手动执行迁移

```bash
# 执行单个迁移
psql $DATABASE_URL -f migrations/NNN_xxx.sql

# 批量执行（按序号排序）
for f in migrations/*.sql; do psql $DATABASE_URL -f "$f"; done
```

### 迁移文件命名规范

```
NNN_description.sql        # up 迁移
NNN_description.up.sql     # up 迁移（显式）
NNN_description.down.sql   # down 迁移（回滚）
```

- 编号 `NNN` 递增，三位数字
- 新增迁移时选择下一个可用编号
- 同时更新 `internal/controlplane/model/` 中的 GORM 模型

### 迁移文件清单

当前共 52 个文件，编号 001-041：

| 编号 | 文件 | 说明 |
|------|------|------|
| 001 | create_providers | 厂商渠道表 |
| 002 | create_models | 模型表 |
| 003 | create_provider_definitions | 厂商定义 + 内置 seed |
| 007 | create_model_tags | 模型标签 |
| 011 | create_model_catalog | Catalog 能力/价格真源 |
| 016 | create_auth_identity_tables | 部门/用户 |
| 018 | add_api_keys | API Key |
| 022 | add_model_groups | 模型组 |
| 024 | create_key_group_bindings | Key↔组绑定 |
| 027-030 | announcements | 系统公告 |
| 032 | create_call_logs | 调用明细 |
| 033 | create_usage_daily | 按天聚合 |

完整列表见 `migrations/` 目录。

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

## Expand-Contract 模式

对于需要修改列类型或重命名的破坏性变更，采用 Expand-Contract：

1. **Expand**：添加新列，保持旧列
2. **Migrate**：应用代码写入两列，迁移脚本填充新列
3. **Contract**：确认无旧数据依赖后，删除旧列

示例：`025_models_name_provider_unique.sql` 将单列唯一改为联合唯一，需先添加约束再删除旧约束。
