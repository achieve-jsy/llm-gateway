-- 032_add_usage_fields.sql
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
