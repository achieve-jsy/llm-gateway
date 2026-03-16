CREATE TABLE IF NOT EXISTS model_daily_stats (
    stat_date           DATE        NOT NULL,
    model               TEXT        NOT NULL,
    studio_id           TEXT        NOT NULL DEFAULT '',
    req_count           INT         NOT NULL DEFAULT 0,
    success_count       INT         NOT NULL DEFAULT 0,
    user_count          INT         NOT NULL DEFAULT 0,
    dept_count          INT         NOT NULL DEFAULT 0,
    active_days         INT         NOT NULL DEFAULT 0,
    prompt_tokens       BIGINT      NOT NULL DEFAULT 0,
    cached_tokens       BIGINT      NOT NULL DEFAULT 0,
    cache_write_tokens  BIGINT      NOT NULL DEFAULT 0,
    completion_tokens   BIGINT      NOT NULL DEFAULT 0,
    reasoning_tokens    BIGINT      NOT NULL DEFAULT 0,
    est_cost_usd        DECIMAL(18,6) NOT NULL DEFAULT 0,
    avg_duration_ms     INT         NOT NULL DEFAULT 0,
    p50_duration_ms     INT         NOT NULL DEFAULT 0,
    p90_duration_ms     INT         NOT NULL DEFAULT 0,
    avg_first_token_ms  INT         NOT NULL DEFAULT 0,
    PRIMARY KEY (stat_date, model, studio_id)
);

CREATE INDEX IF NOT EXISTS idx_model_daily_stats_model ON model_daily_stats(model);
CREATE INDEX IF NOT EXISTS idx_model_daily_stats_date ON model_daily_stats(stat_date);