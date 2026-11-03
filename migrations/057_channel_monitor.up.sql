-- Migration 057: Channel Monitor subsystem
-- Wave 4: Full channel monitoring with arithmetic challenge verification,
-- ping/request latency separation, 7/15/30d availability, per-channel ticker,
-- request templates, SSRF protection, AES key encryption.

CREATE TABLE IF NOT EXISTS channel_monitors (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    provider VARCHAR(50) NOT NULL CHECK (provider IN ('openai', 'anthropic', 'gemini')),
    api_mode VARCHAR(50) NOT NULL DEFAULT 'chat_completions' CHECK (api_mode IN ('chat_completions', 'responses')),
    endpoint TEXT NOT NULL,
    api_key_encrypted TEXT NOT NULL,
    primary_model VARCHAR(200) NOT NULL,
    extra_models TEXT[] NOT NULL DEFAULT '{}',
    group_name VARCHAR(200) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    interval_seconds INTEGER NOT NULL DEFAULT 60 CHECK (interval_seconds >= 15 AND interval_seconds <= 3600),
    last_checked_at TIMESTAMPTZ,
    created_by VARCHAR(100) NOT NULL DEFAULT '',
    extra_headers JSONB NOT NULL DEFAULT '{}',
    body_override_mode VARCHAR(20) NOT NULL DEFAULT 'off' CHECK (body_override_mode IN ('off', 'merge', 'replace')),
    body_override JSONB,
    template_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_channel_monitors_enabled ON channel_monitors (enabled) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_channel_monitors_provider ON channel_monitors (provider) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS channel_monitor_histories (
    id BIGSERIAL PRIMARY KEY,
    monitor_id BIGINT NOT NULL REFERENCES channel_monitors(id) ON DELETE CASCADE,
    model VARCHAR(200) NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('operational', 'degraded', 'failed', 'error')),
    latency_ms INTEGER,
    ping_latency_ms INTEGER,
    message VARCHAR(500) NOT NULL DEFAULT '',
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_histories_lookup ON channel_monitor_histories (monitor_id, model, checked_at DESC);
CREATE INDEX IF NOT EXISTS idx_channel_monitor_histories_checked_at ON channel_monitor_histories (checked_at);

CREATE TABLE IF NOT EXISTS channel_monitor_daily_rollups (
    id BIGSERIAL PRIMARY KEY,
    monitor_id BIGINT NOT NULL REFERENCES channel_monitors(id) ON DELETE CASCADE,
    model VARCHAR(200) NOT NULL,
    bucket_date DATE NOT NULL,
    total_checks INTEGER NOT NULL DEFAULT 0,
    ok_count INTEGER NOT NULL DEFAULT 0,
    operational_count INTEGER NOT NULL DEFAULT 0,
    degraded_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    error_count INTEGER NOT NULL DEFAULT 0,
    sum_latency_ms BIGINT NOT NULL DEFAULT 0,
    count_latency INTEGER NOT NULL DEFAULT 0,
    sum_ping_latency_ms BIGINT NOT NULL DEFAULT 0,
    count_ping_latency INTEGER NOT NULL DEFAULT 0,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_monitor_daily_rollups_uniq ON channel_monitor_daily_rollups (monitor_id, model, bucket_date) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_channel_monitor_daily_rollups_date ON channel_monitor_daily_rollups (bucket_date) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS channel_monitor_request_templates (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    provider VARCHAR(50) NOT NULL CHECK (provider IN ('openai', 'anthropic', 'gemini')),
    api_mode VARCHAR(50) NOT NULL DEFAULT 'chat_completions' CHECK (api_mode IN ('chat_completions', 'responses')),
    description TEXT NOT NULL DEFAULT '',
    extra_headers JSONB NOT NULL DEFAULT '{}',
    body_override_mode VARCHAR(20) NOT NULL DEFAULT 'off' CHECK (body_override_mode IN ('off', 'merge', 'replace')),
    body_override JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

-- Watermark table for daily aggregation (single-row, id=1)
CREATE TABLE IF NOT EXISTS channel_monitor_aggregation_watermark (
    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_aggregated_date DATE NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert initial watermark row (today, so aggregation starts from tomorrow)
INSERT INTO channel_monitor_aggregation_watermark (id, last_aggregated_date)
VALUES (1, CURRENT_DATE)
ON CONFLICT (id) DO NOTHING;