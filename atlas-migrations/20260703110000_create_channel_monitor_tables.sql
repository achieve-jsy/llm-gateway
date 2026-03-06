-- Channel Monitor 子系统数据表 (WP12)
-- 5 tables: channel_monitors, channel_monitor_histories, channel_monitor_daily_rollups,
--   channel_monitor_request_templates, channel_monitor_aggregation_watermark

CREATE TABLE IF NOT EXISTS "channel_monitors" (
    "id" bigserial PRIMARY KEY,
    "name" varchar(200) NOT NULL,
    "provider" varchar(50) NOT NULL,
    "api_mode" varchar(50) NOT NULL DEFAULT 'chat_completions',
    "endpoint" text NOT NULL,
    "api_key_encrypted" text NOT NULL,
    "primary_model" varchar(200) NOT NULL,
    "extra_models" text[] NOT NULL DEFAULT '{}',
    "group_name" varchar(200) NOT NULL DEFAULT '',
    "enabled" boolean NOT NULL DEFAULT true,
    "interval_seconds" integer NOT NULL DEFAULT 60,
    "last_checked_at" timestamptz,
    "created_by" varchar(100) NOT NULL DEFAULT '',
    "extra_headers" jsonb NOT NULL DEFAULT '{}',
    "body_override_mode" varchar(20) NOT NULL DEFAULT 'off',
    "body_override" jsonb,
    "template_id" bigint,
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "updated_at" timestamptz NOT NULL DEFAULT now(),
    "deleted_at" timestamptz
);

CREATE INDEX IF NOT EXISTS "idx_channel_monitors_deleted_at" ON "channel_monitors" ("deleted_at");

CREATE TABLE IF NOT EXISTS "channel_monitor_histories" (
    "id" bigserial PRIMARY KEY,
    "monitor_id" bigint NOT NULL,
    "model" varchar(200) NOT NULL,
    "status" varchar(20) NOT NULL,
    "latency_ms" integer,
    "ping_latency_ms" integer,
    "message" varchar(500) NOT NULL DEFAULT '',
    "checked_at" timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS "idx_channel_monitor_histories_lookup" ON "channel_monitor_histories" ("monitor_id", "model", "checked_at" DESC);
CREATE INDEX IF NOT EXISTS "idx_channel_monitor_histories_checked_at" ON "channel_monitor_histories" ("checked_at");

CREATE TABLE IF NOT EXISTS "channel_monitor_daily_rollups" (
    "id" bigserial PRIMARY KEY,
    "monitor_id" bigint NOT NULL,
    "model" varchar(200) NOT NULL,
    "bucket_date" date NOT NULL,
    "total_checks" integer NOT NULL DEFAULT 0,
    "ok_count" integer NOT NULL DEFAULT 0,
    "operational_count" integer NOT NULL DEFAULT 0,
    "degraded_count" integer NOT NULL DEFAULT 0,
    "failed_count" integer NOT NULL DEFAULT 0,
    "error_count" integer NOT NULL DEFAULT 0,
    "sum_latency_ms" bigint NOT NULL DEFAULT 0,
    "count_latency" integer NOT NULL DEFAULT 0,
    "sum_ping_latency_ms" bigint NOT NULL DEFAULT 0,
    "count_ping_latency" integer NOT NULL DEFAULT 0,
    "computed_at" timestamptz NOT NULL DEFAULT now(),
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "deleted_at" timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS "idx_channel_monitor_daily_rollups_uniq" ON "channel_monitor_daily_rollups" ("monitor_id", "model", "bucket_date");
CREATE INDEX IF NOT EXISTS "idx_channel_monitor_daily_rollups_deleted_at" ON "channel_monitor_daily_rollups" ("deleted_at");

CREATE TABLE IF NOT EXISTS "channel_monitor_request_templates" (
    "id" bigserial PRIMARY KEY,
    "name" varchar(200) NOT NULL,
    "provider" varchar(50) NOT NULL,
    "api_mode" varchar(50) NOT NULL DEFAULT 'chat_completions',
    "description" text NOT NULL DEFAULT '',
    "extra_headers" jsonb NOT NULL DEFAULT '{}',
    "body_override_mode" varchar(20) NOT NULL DEFAULT 'off',
    "body_override" jsonb,
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "updated_at" timestamptz NOT NULL DEFAULT now(),
    "deleted_at" timestamptz
);

CREATE INDEX IF NOT EXISTS "idx_channel_monitor_request_templates_deleted_at" ON "channel_monitor_request_templates" ("deleted_at");

CREATE TABLE IF NOT EXISTS "channel_monitor_aggregation_watermark" (
    "id" integer PRIMARY KEY DEFAULT 1,
    "last_aggregated_date" date NOT NULL,
    "updated_at" timestamptz NOT NULL DEFAULT now()
);

