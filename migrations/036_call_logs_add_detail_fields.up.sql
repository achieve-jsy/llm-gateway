-- 036_call_logs_add_detail_fields.up.sql
-- 为 call_logs 补充明细字段：首 token 延迟、User-Agent、IP、推理强度、分项成本

ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS first_token_ms    INT,
    ADD COLUMN IF NOT EXISTS user_agent        TEXT,
    ADD COLUMN IF NOT EXISTS ip_address        VARCHAR(64),
    ADD COLUMN IF NOT EXISTS reasoning_effort  VARCHAR(16),
    ADD COLUMN IF NOT EXISTS input_cost_usd    NUMERIC(18,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS output_cost_usd   NUMERIC(18,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_read_cost_usd  NUMERIC(18,8) NOT NULL DEFAULT 0;