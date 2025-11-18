ALTER TABLE call_logs
    DROP COLUMN IF EXISTS first_token_ms,
    DROP COLUMN IF EXISTS user_agent,
    DROP COLUMN IF EXISTS ip_address,
    DROP COLUMN IF EXISTS reasoning_effort,
    DROP COLUMN IF EXISTS input_cost_usd,
    DROP COLUMN IF EXISTS output_cost_usd,
    DROP COLUMN IF EXISTS cache_read_cost_usd;