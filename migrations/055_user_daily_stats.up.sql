CREATE TABLE IF NOT EXISTS user_daily_stats (
    stat_date           DATE        NOT NULL,
    user_id             TEXT        NOT NULL,
    studio_id           TEXT        NOT NULL DEFAULT '',
    dept_id             TEXT        NOT NULL DEFAULT '',
    req_count           INT         NOT NULL DEFAULT 0,
    success_count       INT         NOT NULL DEFAULT 0,
    error_count         INT         NOT NULL DEFAULT 0,
    model_count         INT         NOT NULL DEFAULT 0,
    prompt_tokens       BIGINT      NOT NULL DEFAULT 0,
    cached_tokens       BIGINT      NOT NULL DEFAULT 0,
    completion_tokens   BIGINT      NOT NULL DEFAULT 0,
    reasoning_tokens    BIGINT      NOT NULL DEFAULT 0,
    est_cost_usd        DECIMAL(18,6) NOT NULL DEFAULT 0,
    hourly_distribution JSONB       NOT NULL DEFAULT '{}',
    PRIMARY KEY (stat_date, user_id)
);

CREATE INDEX idx_user_daily_stats_user ON user_daily_stats(user_id);
CREATE INDEX idx_user_daily_stats_date ON user_daily_stats(stat_date);
CREATE INDEX idx_user_daily_stats_dept ON user_daily_stats(dept_id);
CREATE INDEX idx_user_daily_stats_studio ON user_daily_stats(studio_id);
