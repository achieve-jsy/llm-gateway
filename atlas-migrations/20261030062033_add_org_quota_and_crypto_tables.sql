-- 补齐「代码需要、但迁移目录里没有」的表和列。
--
-- 背景：这些表/列历史上只在运行时或手工 SQL 里建过（orgservice 的 studios/departments
-- 扩展列、user_roles、dept_studio_map、org_migration_log，channel_monitor 系列，
-- model_status_logs，以及 crypto 的 encryption_keys），Atlas 目录里从来没有。
-- 结果是从零建库后服务一启动就报 column does not exist（models.quota_rpm 等）。
--
-- 本文件由 `atlas migrate diff` 生成后**人工裁剪**：只保留 CREATE TABLE / ADD COLUMN /
-- 新增索引，删掉了同一份 diff 里与本次无关的既有漂移（alert_*/audit_logs/budget_* 等表的
-- 类型与约束变更）。那些漂移是模型与迁移早就对不上的历史问题，改它们会带来数据重写风险，
-- 需要单独一次专项治理，不能搭在补列的车上一起来。

ALTER TABLE "api_keys" ADD COLUMN "quota_rpm" bigint NULL, ADD COLUMN "quota_max_concurrency" bigint NULL;
ALTER TABLE "departments" ADD COLUMN "studio_id" uuid NULL, ADD COLUMN "status" character varying(32) NOT NULL DEFAULT 'pending_studio', ADD COLUMN "owner_id" character varying(64) NULL;
CREATE INDEX "idx_departments_status" ON "departments" ("status");
CREATE INDEX "idx_departments_studio_id" ON "departments" ("studio_id");
ALTER TABLE "group_models" ADD COLUMN "provider_name" text NULL;
ALTER TABLE "models" ADD COLUMN "quota_rpm" bigint NULL, ADD COLUMN "quota_max_concurrency" bigint NULL;
ALTER TABLE "users" ADD COLUMN "quota_rpm" bigint NULL, ADD COLUMN "quota_max_concurrency" bigint NULL;
ALTER TABLE "model_groups" ADD COLUMN "quota_rpm" bigint NULL, ADD COLUMN "quota_max_concurrency" bigint NULL;

CREATE TABLE "channel_monitor_aggregation_watermark" (
  "id" bigint NOT NULL DEFAULT 1,
  "last_aggregated_date" date NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create "channel_monitor_daily_rollups" table
CREATE TABLE "channel_monitor_daily_rollups" (
  "id" bigserial NOT NULL,
  "monitor_id" bigint NOT NULL,
  "model" character varying(200) NOT NULL,
  "bucket_date" date NOT NULL,
  "total_checks" bigint NOT NULL DEFAULT 0,
  "ok_count" bigint NOT NULL DEFAULT 0,
  "operational_count" bigint NOT NULL DEFAULT 0,
  "degraded_count" bigint NOT NULL DEFAULT 0,
  "failed_count" bigint NOT NULL DEFAULT 0,
  "error_count" bigint NOT NULL DEFAULT 0,
  "sum_latency_ms" bigint NOT NULL DEFAULT 0,
  "count_latency" bigint NOT NULL DEFAULT 0,
  "sum_ping_latency_ms" bigint NOT NULL DEFAULT 0,
  "count_ping_latency" bigint NOT NULL DEFAULT 0,
  "computed_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_channel_monitor_daily_rollups_deleted_at" to table: "channel_monitor_daily_rollups"
CREATE INDEX "idx_channel_monitor_daily_rollups_deleted_at" ON "channel_monitor_daily_rollups" ("deleted_at");
-- Create index "idx_channel_monitor_daily_rollups_uniq" to table: "channel_monitor_daily_rollups"
CREATE UNIQUE INDEX "idx_channel_monitor_daily_rollups_uniq" ON "channel_monitor_daily_rollups" ("monitor_id", "model", "bucket_date");
-- Create "channel_monitor_histories" table
CREATE TABLE "channel_monitor_histories" (
  "id" bigserial NOT NULL,
  "monitor_id" bigint NOT NULL,
  "model" character varying(200) NOT NULL,
  "status" character varying(20) NOT NULL,
  "latency_ms" bigint NULL,
  "ping_latency_ms" bigint NULL,
  "message" character varying(500) NOT NULL DEFAULT '',
  "checked_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_channel_monitor_histories_checked_at" to table: "channel_monitor_histories"
CREATE INDEX "idx_channel_monitor_histories_checked_at" ON "channel_monitor_histories" ("checked_at");
-- Create index "idx_channel_monitor_histories_lookup" to table: "channel_monitor_histories"
CREATE INDEX "idx_channel_monitor_histories_lookup" ON "channel_monitor_histories" ("checked_at" DESC, "monitor_id", "model");
-- Create "channel_monitor_request_templates" table
CREATE TABLE "channel_monitor_request_templates" (
  "id" bigserial NOT NULL,
  "name" character varying(200) NOT NULL,
  "provider" character varying(50) NOT NULL,
  "api_mode" character varying(50) NOT NULL DEFAULT 'chat_completions',
  "description" text NOT NULL DEFAULT '',
  "extra_headers" jsonb NOT NULL DEFAULT '{}',
  "body_override_mode" character varying(20) NOT NULL DEFAULT 'off',
  "body_override" jsonb NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_channel_monitor_request_templates_deleted_at" to table: "channel_monitor_request_templates"
CREATE INDEX "idx_channel_monitor_request_templates_deleted_at" ON "channel_monitor_request_templates" ("deleted_at");
-- Create "channel_monitors" table
CREATE TABLE "channel_monitors" (
  "id" bigserial NOT NULL,
  "name" character varying(200) NOT NULL,
  "provider" character varying(50) NOT NULL,
  "api_mode" character varying(50) NOT NULL DEFAULT 'chat_completions',
  "endpoint" text NOT NULL,
  "api_key_encrypted" text NOT NULL,
  "primary_model" character varying(200) NOT NULL,
  "extra_models" text[] NOT NULL DEFAULT '{}',
  "group_name" character varying(200) NOT NULL DEFAULT '',
  "enabled" boolean NOT NULL DEFAULT true,
  "interval_seconds" bigint NOT NULL DEFAULT 60,
  "last_checked_at" timestamptz NULL,
  "created_by" character varying(100) NOT NULL DEFAULT '',
  "extra_headers" jsonb NOT NULL DEFAULT '{}',
  "body_override_mode" character varying(20) NOT NULL DEFAULT 'off',
  "body_override" jsonb NULL,
  "template_id" bigint NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_channel_monitors_deleted_at" to table: "channel_monitors"
CREATE INDEX "idx_channel_monitors_deleted_at" ON "channel_monitors" ("deleted_at");
-- Create "dept_studio_map" table
CREATE TABLE "dept_studio_map" (
  "dept_id" character varying(64) NOT NULL,
  "studio_id" uuid NOT NULL,
  "source" character varying(16) NOT NULL DEFAULT 'manual',
  "updated_by" character varying(64) NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("dept_id")
);
-- Create index "idx_dept_studio_map_studio_id" to table: "dept_studio_map"
CREATE INDEX "idx_dept_studio_map_studio_id" ON "dept_studio_map" ("studio_id");
-- Create "encryption_keys" table
CREATE TABLE "encryption_keys" (
  "version" bigserial NOT NULL,
  "encrypted_key" text NULL,
  "algorithm" character varying(50) NULL,
  "status" character varying(20) NULL,
  "created_at" text NULL,
  "retired_at" text NULL,
  PRIMARY KEY ("version")
);
-- Create "model_status_logs" table
CREATE TABLE "model_status_logs" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "model_id" uuid NOT NULL,
  "model_name" character varying(200) NOT NULL,
  "provider_id" uuid NOT NULL,
  "provider_name" character varying(200) NOT NULL,
  "change_type" character varying(20) NOT NULL,
  "reason" character varying(500) NOT NULL DEFAULT '',
  "error_message" text NULL DEFAULT '',
  "operator_id" uuid NULL,
  "operator_name" character varying(128) NULL DEFAULT '',
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_model_status_logs_change_type" to table: "model_status_logs"
CREATE INDEX "idx_model_status_logs_change_type" ON "model_status_logs" ("change_type");
-- Create index "idx_model_status_logs_created_at" to table: "model_status_logs"
CREATE INDEX "idx_model_status_logs_created_at" ON "model_status_logs" ("created_at");
-- Create index "idx_model_status_logs_model_id" to table: "model_status_logs"
CREATE INDEX "idx_model_status_logs_model_id" ON "model_status_logs" ("model_id");
-- Create "org_migration_log" table
CREATE TABLE "org_migration_log" (
  "id" bigserial NOT NULL,
  "user_id" character varying(64) NOT NULL,
  "from_dept" character varying(64) NOT NULL,
  "to_dept" character varying(64) NOT NULL,
  "migrated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_migration_log_user_id" to table: "org_migration_log"
CREATE INDEX "idx_migration_log_user_id" ON "org_migration_log" ("user_id");
-- Create "studios" table
CREATE TABLE "studios" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" character varying(255) NOT NULL,
  "owner_id" character varying(64) NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_studios_name" to table: "studios"
CREATE UNIQUE INDEX "idx_studios_name" ON "studios" ("name");
-- Create "user_roles" table
CREATE TABLE "user_roles" (
  "id" bigserial NOT NULL,
  "user_id" character varying(64) NOT NULL,
  "role" character varying(32) NOT NULL,
  "scope_type" character varying(32) NOT NULL,
  "scope_id" character varying(64) NOT NULL DEFAULT '',
  "granted_by" character varying(64) NOT NULL,
  "actor_id" character varying(64) NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_user_roles_scope" to table: "user_roles"
CREATE INDEX "idx_user_roles_scope" ON "user_roles" ("scope_type", "scope_id");
-- Create index "idx_user_roles_user_id" to table: "user_roles"
CREATE INDEX "idx_user_roles_user_id" ON "user_roles" ("user_id");
-- Modify "video_task_events" table
