# gateway-core 数据模型变更 — CHG-20260603-001

> 基线：xspec/modules/gateway-core/spec.md (v1.0) — 首次数据模型变更

## 变更摘要

call_logs 表新增 16 列（token 分桶 / 延迟细分 / 成本分桶 / provider 路由 / 元数据标记）；新建 call_payloads 表（调用内容存储）。

## call_logs 表变更

### 新增列

```sql
ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS reasoning_tokens      INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cached_tokens         INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS ttft_ms               INTEGER,
    ADD COLUMN IF NOT EXISTS duration_ms           INTEGER,
    ADD COLUMN IF NOT EXISTS provider_duration_ms  INTEGER,
    ADD COLUMN IF NOT EXISTS provider_type         VARCHAR(50),
    ADD COLUMN IF NOT EXISTS provider_model_id     UUID,
    ADD COLUMN IF NOT EXISTS is_stream             BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS session_id            VARCHAR(128),
    ADD COLUMN IF NOT EXISTS prompt_est_cost       DECIMAL(12,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS completion_est_cost   DECIMAL(12,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reasoning_est_cost    DECIMAL(12,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cached_est_cost       DECIMAL(12,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_est_cost        DECIMAL(12,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS usage_missing         BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS pricing_missing       BOOLEAN NOT NULL DEFAULT false;
```

### 新增索引

```sql
CREATE INDEX IF NOT EXISTS idx_call_logs_stat_date  ON call_logs(stat_date);
CREATE INDEX IF NOT EXISTS idx_call_logs_key_date   ON call_logs(key_id, stat_date);
CREATE INDEX IF NOT EXISTS idx_call_logs_dept_date  ON call_logs(dept_id, stat_date);
```

### 新增列说明

| 列名 | 类型 | 用途 | 采集来源 |
| :--- | :--- | :--- | :--- |
| `reasoning_tokens` | INTEGER | 推理 token 数（思考模型） | StreamUsageParser / 响应解析 |
| `cached_tokens` | INTEGER | 缓存命中 token 数 | StreamUsageParser / 响应解析 |
| `ttft_ms` | INTEGER | 首 token 延迟（毫秒） | Orchestrator 计时 |
| `duration_ms` | INTEGER | 端到端总延迟（毫秒） | Orchestrator 计时 |
| `provider_duration_ms` | INTEGER | 上游实际耗时（毫秒） | 响应 Header `x-request-duration` |
| `provider_type` | VARCHAR(50) | 厂商类型 | Provider Router |
| `provider_model_id` | UUID | 实际命中的模型 DB ID | Provider Router |
| `is_stream` | BOOLEAN | 是否流式调用 | 请求参数 |
| `session_id` | VARCHAR(128) | 会话 ID | 请求 Header `X-Session-Id` |
| `prompt_est_cost` | DECIMAL(12,8) | Prompt 估算成本（USD） | CostEstimator |
| `completion_est_cost` | DECIMAL(12,8) | Completion 估算成本（USD） | CostEstimator |
| `reasoning_est_cost` | DECIMAL(12,8) | Reasoning 估算成本（USD） | CostEstimator |
| `cached_est_cost` | DECIMAL(12,8) | Cached 估算成本（USD） | CostEstimator |
| `total_est_cost` | DECIMAL(12,8) | 总估算成本（USD） | CostEstimator |
| `usage_missing` | BOOLEAN | usage 数据是否缺失 | StreamUsageParser |
| `pricing_missing` | BOOLEAN | 定价是否缺失 | MemoryPricingCache |

### 完整字段列表（变更后）

| 字段 | 类型 | 约束 | 变更类型 |
| :--- | :--- | :--- | :---: |
| `request_id` | UUID | PK, NOT NULL | 已有 |
| `key_id` | UUID | NOT NULL | 已有 |
| `user_id` | UUID | NOT NULL | 已有 |
| `dept_id` | UUID | NULLABLE | 已有 |
| `studio_id` | UUID | NULLABLE | 已有（预留） |
| `model` | VARCHAR(100) | NOT NULL | 已有 |
| `stat_date` | DATE | NOT NULL | 已有 |
| `prompt_tokens` | INTEGER | NOT NULL, DEFAULT 0 | 已有 |
| `completion_tokens` | INTEGER | NOT NULL, DEFAULT 0 | 已有 |
| `status` | VARCHAR(20) | NOT NULL, DEFAULT 'success' | 已有 |
| `created_at` | TIMESTAMP | NOT NULL, DEFAULT NOW() | 已有 |
| `reasoning_tokens` | INTEGER | NOT NULL, DEFAULT 0 | **新增** |
| `cached_tokens` | INTEGER | NOT NULL, DEFAULT 0 | **新增** |
| `ttft_ms` | INTEGER | NULLABLE | **新增** |
| `duration_ms` | INTEGER | NULLABLE | **新增** |
| `provider_duration_ms` | INTEGER | NULLABLE | **新增** |
| `provider_type` | VARCHAR(50) | NULLABLE | **新增** |
| `provider_model_id` | UUID | NULLABLE | **新增** |
| `is_stream` | BOOLEAN | NOT NULL, DEFAULT false | **新增** |
| `session_id` | VARCHAR(128) | NULLABLE | **新增** |
| `prompt_est_cost` | DECIMAL(12,8) | NOT NULL, DEFAULT 0 | **新增** |
| `completion_est_cost` | DECIMAL(12,8) | NOT NULL, DEFAULT 0 | **新增** |
| `reasoning_est_cost` | DECIMAL(12,8) | NOT NULL, DEFAULT 0 | **新增** |
| `cached_est_cost` | DECIMAL(12,8) | NOT NULL, DEFAULT 0 | **新增** |
| `total_est_cost` | DECIMAL(12,8) | NOT NULL, DEFAULT 0 | **新增** |
| `usage_missing` | BOOLEAN | NOT NULL, DEFAULT false | **新增** |
| `pricing_missing` | BOOLEAN | NOT NULL, DEFAULT false | **新增** |

## call_payloads 表（新建）

```sql
CREATE TABLE IF NOT EXISTS call_payloads (
    request_id         UUID PRIMARY KEY,
    messages           JSONB NOT NULL,
    response_text      TEXT,
    system_fingerprint VARCHAR(255),
    tools              JSONB,
    byte_size          INTEGER NOT NULL DEFAULT 0,
    truncated          BOOLEAN NOT NULL DEFAULT false,
    storage_backend    VARCHAR(20) NOT NULL DEFAULT 'pg',
    created_at         TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_call_payloads_req_id ON call_payloads(request_id);
```

### 字段说明

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `request_id` | UUID PK | 与 call_logs 一一对应 |
| `messages` | JSONB | 完整请求 messages 数组（含 system/tool） |
| `response_text` | TEXT | 响应正文（流式已重组） |
| `system_fingerprint` | VARCHAR(255) | 系统指纹 |
| `tools` | JSONB | 工具定义 |
| `byte_size` | INTEGER | 内容字节数 |
| `truncated` | BOOLEAN | 是否因超过 256KB 而截断 |
| `storage_backend` | VARCHAR(20) | 存储后端（`pg` / 将来 `oss`） |
| `created_at` | TIMESTAMP | 创建时间 |

## 迁移文件

迁移脚本：`migrations/NNN_add_usage_fields.sql`

```sql
-- NNN_add_usage_fields.sql
-- 变更：CHG-20260603-001 — 用量采集与成本估算
-- 描述：call_logs 新增分桶/延迟/成本列；新建 call_payloads 表

-- 1. call_logs 新增列
ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS reasoning_tokens      INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cached_tokens         INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS ttft_ms               INTEGER,
    ADD COLUMN IF NOT EXISTS duration_ms           INTEGER,
    ADD COLUMN IF NOT EXISTS provider_duration_ms  INTEGER,
    ADD COLUMN IF NOT EXISTS provider_type         VARCHAR(50),
    ADD COLUMN IF NOT EXISTS provider_model_id     UUID,
    ADD COLUMN IF NOT EXISTS is_stream             BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS session_id            VARCHAR(128),
    ADD COLUMN IF NOT EXISTS prompt_est_cost       DECIMAL(12,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS completion_est_cost   DECIMAL(12,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reasoning_est_cost    DECIMAL(12,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cached_est_cost       DECIMAL(12,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_est_cost        DECIMAL(12,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS usage_missing         BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS pricing_missing       BOOLEAN NOT NULL DEFAULT false;

-- 2. call_logs 新增索引
CREATE INDEX IF NOT EXISTS idx_call_logs_stat_date  ON call_logs(stat_date);
CREATE INDEX IF NOT EXISTS idx_call_logs_key_date   ON call_logs(key_id, stat_date);
CREATE INDEX IF NOT EXISTS idx_call_logs_dept_date  ON call_logs(dept_id, stat_date);

-- 3. 新建 call_payloads 表
CREATE TABLE IF NOT EXISTS call_payloads (
    request_id         UUID PRIMARY KEY,
    messages           JSONB NOT NULL,
    response_text      TEXT,
    system_fingerprint VARCHAR(255),
    tools              JSONB,
    byte_size          INTEGER NOT NULL DEFAULT 0,
    truncated          BOOLEAN NOT NULL DEFAULT false,
    storage_backend    VARCHAR(20) NOT NULL DEFAULT 'pg',
    created_at         TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_call_payloads_req_id ON call_payloads(request_id);
```

## 注意事项

- 所有新增列使用 `ADD COLUMN IF NOT EXISTS` + `DEFAULT` 值，对已有数据无影响
- `DECIMAL(12,8)` 精度：12 位总长度（整数最多 4 位 + 小数 8 位），可表示 $0.00000001 ~ $9999.99999999
- `DEFAULT 0` 保证已有行在新增列上的查询不返回 NULL
- `call_payloads.messages` 使用 JSONB 存储完整 messages 数组，支持 JSON 查询
