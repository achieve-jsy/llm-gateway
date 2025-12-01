-- 032_create_call_logs.up.sql
-- 调用明细表：gateway-api 异步埋点写入，observability-hub rollup 聚合消费

CREATE TABLE IF NOT EXISTS call_logs (
    request_id         TEXT PRIMARY KEY,
    ts                 TIMESTAMPTZ NOT NULL,
    key_id             TEXT,
    user_id            TEXT,
    dept_id            TEXT,
    studio_id          TEXT,
    model              TEXT NOT NULL,
    routed_model       TEXT,
    group_name         TEXT,
    provider           TEXT,
    prompt_tokens      INT NOT NULL DEFAULT 0,
    cached_tokens      INT NOT NULL DEFAULT 0,
    cache_write_tokens INT NOT NULL DEFAULT 0,
    completion_tokens  INT NOT NULL DEFAULT 0,
    reasoning_tokens   INT NOT NULL DEFAULT 0,
    total_tokens       INT NOT NULL DEFAULT 0,
    status             INT,
    upstream_status    INT,
    duration_ms        INT,
    stream             BOOLEAN NOT NULL DEFAULT false,
    failover_used      BOOLEAN NOT NULL DEFAULT false,
    usage_missing      BOOLEAN NOT NULL DEFAULT false,
    input_price_snapshot  NUMERIC(14,6),
    output_price_snapshot NUMERIC(14,6),
    est_cost_usd       NUMERIC(18,6) NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_call_logs_key_ts   ON call_logs (key_id, ts);
CREATE INDEX IF NOT EXISTS idx_call_logs_dept_ts  ON call_logs (dept_id, ts);
CREATE INDEX IF NOT EXISTS idx_call_logs_model_ts ON call_logs (model, ts);
CREATE INDEX IF NOT EXISTS idx_call_logs_ts       ON call_logs (ts);