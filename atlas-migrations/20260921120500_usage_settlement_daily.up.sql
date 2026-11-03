-- 063: 金额结算沉淀表 usage_settlement_daily（C·明细账户粒度）
-- 参考 docs/trace-archive-final-design.md §2.2/§15

CREATE TABLE IF NOT EXISTS usage_settlement_daily (
    stat_date            DATE        NOT NULL,
    key_id               TEXT        NOT NULL,
    dept_id              TEXT        NOT NULL DEFAULT '',
    studio_id            TEXT        NOT NULL DEFAULT '',
    model                TEXT        NOT NULL,
    provider             TEXT        NOT NULL DEFAULT '',
    budget_tier_id       TEXT        NOT NULL DEFAULT '',   -- '' = 未绑定预算
    budget_tier_name     TEXT        NOT NULL DEFAULT '',
    accounting_eligible  BOOLEAN     NOT NULL DEFAULT false,
    req_count            BIGINT      NOT NULL DEFAULT 0,
    success_count        BIGINT      NOT NULL DEFAULT 0,
    prompt_tokens        BIGINT      NOT NULL DEFAULT 0,
    cached_tokens        BIGINT      NOT NULL DEFAULT 0,
    cache_write_tokens   BIGINT      NOT NULL DEFAULT 0,
    completion_tokens    BIGINT      NOT NULL DEFAULT 0,
    reasoning_tokens     BIGINT      NOT NULL DEFAULT 0,
    input_cost_usd       DECIMAL(18,8) NOT NULL DEFAULT 0,
    output_cost_usd      DECIMAL(18,8) NOT NULL DEFAULT 0,
    cache_read_cost_usd  DECIMAL(18,8) NOT NULL DEFAULT 0,
    est_cost_usd         DECIMAL(18,6) NOT NULL DEFAULT 0,
    diff_usd             DECIMAL(18,8) NOT NULL DEFAULT 0,
    avg_input_price      DECIMAL(14,6),
    avg_output_price     DECIMAL(14,6),
    price_varied         BOOLEAN     NOT NULL DEFAULT false,
    usage_missing_count  BIGINT      NOT NULL DEFAULT 0,
    settled_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source               VARCHAR(16) NOT NULL DEFAULT 'live',
    batch_id             TEXT        NOT NULL DEFAULT '',
    PRIMARY KEY (stat_date, key_id, dept_id, studio_id, model, budget_tier_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_settlement_statdate ON usage_settlement_daily (stat_date);
CREATE INDEX IF NOT EXISTS idx_settlement_tier    ON usage_settlement_daily (budget_tier_id, stat_date);
CREATE INDEX IF NOT EXISTS idx_settlement_model   ON usage_settlement_daily (model, stat_date);
