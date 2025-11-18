-- 033_create_usage_daily.up.sql
-- 按天聚合表：rollup cron 从 call_logs 聚合写入

CREATE TABLE IF NOT EXISTS usage_daily (
    stat_date          DATE   NOT NULL,
    key_id             TEXT   NOT NULL,
    user_id            TEXT   NOT NULL DEFAULT '',
    dept_id            TEXT   NOT NULL DEFAULT '',
    studio_id          TEXT   NOT NULL DEFAULT '',
    model              TEXT   NOT NULL,
    req_count          INT    NOT NULL DEFAULT 0,
    success_count      INT    NOT NULL DEFAULT 0,
    prompt_tokens      BIGINT NOT NULL DEFAULT 0,
    cached_tokens      BIGINT NOT NULL DEFAULT 0,
    cache_write_tokens BIGINT NOT NULL DEFAULT 0,
    completion_tokens  BIGINT NOT NULL DEFAULT 0,
    reasoning_tokens   BIGINT NOT NULL DEFAULT 0,
    est_cost_usd       NUMERIC(18,6) NOT NULL DEFAULT 0,
    PRIMARY KEY (stat_date, key_id, dept_id, studio_id, model)
);

CREATE INDEX IF NOT EXISTS idx_usage_daily_dept ON usage_daily (dept_id, stat_date);