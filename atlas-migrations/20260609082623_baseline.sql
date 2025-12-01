-- Create "announcement_reads" table
CREATE TABLE "announcement_reads" (
  "user_id" uuid NOT NULL,
  "announcement_id" uuid NOT NULL,
  "is_read" boolean NOT NULL DEFAULT false,
  "is_dismissed" boolean NOT NULL DEFAULT false,
  "read_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("user_id", "announcement_id")
);
-- Create "announcements" table
CREATE TABLE "announcements" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "title" character varying(200) NOT NULL,
  "content" text NOT NULL,
  "category" character varying(20) NOT NULL DEFAULT 'system',
  "level" character varying(20) NOT NULL DEFAULT 'info',
  "display_mode" character varying(20) NOT NULL DEFAULT 'banner',
  "status" character varying(20) NOT NULL DEFAULT 'draft',
  "sort_order" bigint NOT NULL DEFAULT 0,
  "created_by" character varying(128) NOT NULL DEFAULT '',
  "published_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "chk_announcements_category" CHECK ((category)::text = ANY ((ARRAY['system'::character varying, 'feature'::character varying, 'alert'::character varying])::text[])),
  CONSTRAINT "chk_announcements_display_mode" CHECK ((display_mode)::text = ANY ((ARRAY['banner'::character varying, 'popup'::character varying])::text[])),
  CONSTRAINT "chk_announcements_level" CHECK ((level)::text = ANY ((ARRAY['info'::character varying, 'warning'::character varying, 'critical'::character varying])::text[])),
  CONSTRAINT "chk_announcements_status" CHECK ((status)::text = ANY ((ARRAY['draft'::character varying, 'published'::character varying, 'archived'::character varying])::text[]))
);
-- Create "api_keys" table
CREATE TABLE "api_keys" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "name" character varying(128) NOT NULL,
  "key_hash" character varying(128) NOT NULL,
  "encrypted_key" text NOT NULL,
  "key_prefix" character varying(8) NOT NULL DEFAULT 'sk-',
  "key_hint" character varying(8) NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "log_enabled" boolean NOT NULL DEFAULT true,
  "access_all_groups" boolean NOT NULL DEFAULT false,
  "user_name" character varying(128) NULL,
  "dept_id" text NULL DEFAULT '',
  "dept_name" character varying(128) NULL,
  "studio_id" character varying(128) NULL,
  "studio_name" character varying(128) NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "last_used_at" timestamptz NULL,
  "expires_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_api_keys_enabled" to table: "api_keys"
CREATE INDEX "idx_api_keys_enabled" ON "api_keys" ("enabled");
-- Create index "idx_api_keys_user_id" to table: "api_keys"
CREATE INDEX "idx_api_keys_user_id" ON "api_keys" ("user_id");
-- Create index "uniq_key_hash" to table: "api_keys"
CREATE UNIQUE INDEX "uniq_key_hash" ON "api_keys" ("key_hash");
-- Create "call_logs" table
CREATE TABLE "call_logs" (
  "request_id" text NOT NULL,
  "ts" timestamptz NOT NULL,
  "key_id" text NULL,
  "key_name" character varying(128) NULL,
  "user_id" text NULL,
  "user_name" character varying(128) NULL,
  "dept_id" text NULL,
  "dept_name" character varying(128) NULL,
  "studio_id" text NULL,
  "studio_name" character varying(128) NULL,
  "model" text NOT NULL,
  "routed_model" text NULL,
  "group_name" text NULL,
  "provider" text NULL,
  "prompt_tokens" bigint NOT NULL DEFAULT 0,
  "cached_tokens" bigint NOT NULL DEFAULT 0,
  "cache_write_tokens" bigint NOT NULL DEFAULT 0,
  "completion_tokens" bigint NOT NULL DEFAULT 0,
  "reasoning_tokens" bigint NOT NULL DEFAULT 0,
  "total_tokens" bigint NOT NULL DEFAULT 0,
  "status" bigint NULL,
  "upstream_status" bigint NULL,
  "duration_ms" bigint NULL,
  "first_token_ms" bigint NULL,
  "stage_ttfb_ms" bigint NULL,
  "stage_auth_ms" smallint NULL,
  "stage_route_ms" smallint NULL,
  "stage_upstream_ms" smallint NULL,
  "stream" boolean NOT NULL DEFAULT false,
  "failover_used" boolean NOT NULL DEFAULT false,
  "routed_via_group" boolean NOT NULL DEFAULT false,
  "usage_missing" boolean NOT NULL DEFAULT false,
  "reasoning_effort" character varying(16) NULL,
  "user_agent" text NULL,
  "ip_address" character varying(64) NULL,
  "input_price_snapshot" numeric(14,6) NULL,
  "output_price_snapshot" numeric(14,6) NULL,
  "input_cost_usd" numeric(18,8) NOT NULL DEFAULT 0,
  "output_cost_usd" numeric(18,8) NOT NULL DEFAULT 0,
  "cache_read_cost_usd" numeric(18,8) NOT NULL DEFAULT 0,
  "est_cost_usd" numeric(18,6) NOT NULL DEFAULT 0,
  PRIMARY KEY ("request_id")
);
-- Create index "idx_call_logs_dept_ts" to table: "call_logs"
CREATE INDEX "idx_call_logs_dept_ts" ON "call_logs" ("dept_id");
-- Create index "idx_call_logs_key_ts" to table: "call_logs"
CREATE INDEX "idx_call_logs_key_ts" ON "call_logs" ("key_id");
-- Create index "idx_call_logs_model_ts" to table: "call_logs"
CREATE INDEX "idx_call_logs_model_ts" ON "call_logs" ("model");
-- Create index "idx_call_logs_ts" to table: "call_logs"
CREATE INDEX "idx_call_logs_ts" ON "call_logs" ("ts");
-- Create "call_traces" table
CREATE TABLE "call_traces" (
  "request_id" text NOT NULL,
  "ts" timestamptz NOT NULL,
  "stage_auth_ms" smallint NULL,
  "stage_route_ms" smallint NULL,
  "stage_upstream_ms" smallint NULL,
  "stage_ttfb_ms" bigint NULL,
  "stage_ttft_ms" bigint NULL,
  "stage_total_ms" bigint NULL,
  "request_headers" jsonb NULL,
  "request_body" jsonb NULL,
  "response_body" jsonb NULL,
  "error_code" text NULL,
  "error_message" text NULL,
  PRIMARY KEY ("request_id")
);
-- Create index "idx_call_traces_ts" to table: "call_traces"
CREATE INDEX "idx_call_traces_ts" ON "call_traces" ("ts");
-- Create "departments" table
CREATE TABLE "departments" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "dept_id" character varying(64) NOT NULL,
  "dept_name" character varying(128) NOT NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "uniq_departments_dept_id" to table: "departments"
CREATE UNIQUE INDEX "uniq_departments_dept_id" ON "departments" ("dept_id");
-- Create "dev_users" table
CREATE TABLE "dev_users" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "username" character varying(64) NOT NULL,
  "password_hash" character varying(256) NOT NULL,
  "role" character varying(16) NOT NULL DEFAULT 'user',
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "uniq_dev_users_username" to table: "dev_users"
CREATE UNIQUE INDEX "uniq_dev_users_username" ON "dev_users" ("username");
-- Create "health_check_configs" table
CREATE TABLE "health_check_configs" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "enabled" boolean NOT NULL DEFAULT false,
  "interval_seconds" bigint NOT NULL DEFAULT 300,
  "failure_threshold" bigint NOT NULL DEFAULT 3,
  "failure_action" character varying(20) NOT NULL DEFAULT 'auto_disable',
  "check_providers" boolean NOT NULL DEFAULT true,
  "check_models" boolean NOT NULL DEFAULT true,
  "check_mode" character varying(20) NOT NULL DEFAULT 'connectivity',
  "timeout_seconds" bigint NOT NULL DEFAULT 15,
  "model_check_enabled" boolean NOT NULL DEFAULT true,
  "model_check_interval_seconds" bigint NOT NULL DEFAULT 300,
  "model_check_failure_threshold" bigint NOT NULL DEFAULT 3,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create "health_check_logs" table
CREATE TABLE "health_check_logs" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "target_type" character varying(20) NOT NULL,
  "target_id" uuid NOT NULL,
  "target_name" character varying(200) NOT NULL DEFAULT '',
  "provider_id" uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  "provider_name" character varying(200) NOT NULL DEFAULT '',
  "success" boolean NOT NULL,
  "response_time_ms" bigint NOT NULL DEFAULT 0,
  "error_message" text NULL DEFAULT '',
  "checked_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create "key_group_bindings" table
CREATE TABLE "key_group_bindings" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "key_id" uuid NOT NULL,
  "group_id" bigint NOT NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_kbg_group_id" to table: "key_group_bindings"
CREATE INDEX "idx_kbg_group_id" ON "key_group_bindings" ("group_id");
-- Create index "uniq_key_group" to table: "key_group_bindings"
CREATE UNIQUE INDEX "uniq_key_group" ON "key_group_bindings" ("key_id", "group_id");
-- Create "model_groups" table
CREATE TABLE "model_groups" (
  "id" bigserial NOT NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "name" character varying(64) NOT NULL,
  "description" text NULL,
  "strategy" character varying(32) NOT NULL DEFAULT 'round_robin',
  "enabled" boolean NOT NULL DEFAULT true,
  "max_switches" bigint NOT NULL DEFAULT 2,
  "same_model_retries" bigint NOT NULL DEFAULT 1,
  "failover_cooldown_seconds" bigint NOT NULL DEFAULT 60,
  PRIMARY KEY ("id"),
  CONSTRAINT "chk_model_groups_strategy" CHECK ((strategy)::text = ANY ((ARRAY['round_robin'::character varying, 'priority'::character varying, 'weighted'::character varying])::text[]))
);
-- Create index "idx_model_groups_deleted_at" to table: "model_groups"
CREATE INDEX "idx_model_groups_deleted_at" ON "model_groups" ("deleted_at");
-- Create index "idx_model_groups_enabled" to table: "model_groups"
CREATE INDEX "idx_model_groups_enabled" ON "model_groups" ("enabled") WHERE ((enabled = true) AND (deleted_at IS NULL));
-- Create index "uniq_model_groups_name" to table: "model_groups"
CREATE UNIQUE INDEX "uniq_model_groups_name" ON "model_groups" ("name") WHERE (deleted_at IS NULL);
-- Create "model_tag_relations" table
CREATE TABLE "model_tag_relations" (
  "model_id" uuid NOT NULL,
  "tag_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("model_id", "tag_id")
);
-- Create "model_tags" table
CREATE TABLE "model_tags" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" character varying(50) NOT NULL,
  "color" character varying(7) NOT NULL DEFAULT '#2BA8A2',
  "description" character varying(200) NULL DEFAULT '',
  "sort_order" bigint NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_model_tags_name_unique" to table: "model_tags"
CREATE UNIQUE INDEX "idx_model_tags_name_unique" ON "model_tags" ("name");
-- Create "system_configs" table
CREATE TABLE "system_configs" (
  "key" character varying(100) NOT NULL,
  "value" text NULL,
  "encrypted" boolean NULL,
  "description" character varying(500) NULL,
  "created_at" text NULL,
  "updated_at" text NULL,
  PRIMARY KEY ("key")
);
-- Create "usage_daily" table
CREATE TABLE "usage_daily" (
  "stat_date" date NOT NULL,
  "key_id" text NOT NULL,
  "user_id" text NOT NULL DEFAULT '',
  "dept_id" text NOT NULL DEFAULT '',
  "studio_id" text NOT NULL DEFAULT '',
  "model" text NOT NULL,
  "req_count" bigint NOT NULL DEFAULT 0,
  "success_count" bigint NOT NULL DEFAULT 0,
  "prompt_tokens" bigint NOT NULL DEFAULT 0,
  "cached_tokens" bigint NOT NULL DEFAULT 0,
  "cache_write_tokens" bigint NOT NULL DEFAULT 0,
  "completion_tokens" bigint NOT NULL DEFAULT 0,
  "reasoning_tokens" bigint NOT NULL DEFAULT 0,
  "est_cost_usd" numeric(18,6) NOT NULL DEFAULT 0,
  PRIMARY KEY ("stat_date", "key_id", "dept_id", "studio_id", "model")
);
-- Create "users" table
CREATE TABLE "users" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "sub" character varying(128) NOT NULL,
  "dept_id" uuid NULL,
  "name" character varying(128) NOT NULL,
  "email" character varying(256) NOT NULL,
  "source" character varying(32) NOT NULL DEFAULT 'oidc',
  "role" character varying(16) NOT NULL DEFAULT 'user',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_users_dept_id" to table: "users"
CREATE INDEX "idx_users_dept_id" ON "users" ("dept_id");
-- Create index "idx_users_email" to table: "users"
CREATE INDEX "idx_users_email" ON "users" ("email");
-- Create index "uniq_users_sub" to table: "users"
CREATE UNIQUE INDEX "uniq_users_sub" ON "users" ("sub");
-- Create "group_models" table
CREATE TABLE "group_models" (
  "id" bigserial NOT NULL,
  "group_id" bigint NOT NULL,
  "model_id" uuid NOT NULL,
  "model_name" text NULL,
  "priority" bigint NULL,
  "weight" bigint NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_model_groups_models" FOREIGN KEY ("group_id") REFERENCES "model_groups" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "chk_group_models_weight" CHECK ((weight >= 1) AND (weight <= 100))
);
-- Create index "idx_group_models_group_id" to table: "group_models"
CREATE INDEX "idx_group_models_group_id" ON "group_models" ("group_id");
-- Create index "idx_group_models_model_id" to table: "group_models"
CREATE INDEX "idx_group_models_model_id" ON "group_models" ("model_id");
-- Create index "uniq_group_model" to table: "group_models"
CREATE UNIQUE INDEX "uniq_group_model" ON "group_models" ("group_id", "model_id");
-- Create "model_catalog" table
CREATE TABLE "model_catalog" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "source" character varying(50) NOT NULL,
  "source_model_id" character varying(200) NOT NULL,
  "display_name" character varying(200) NULL,
  "provider" character varying(50) NULL,
  "mode" character varying(30) NULL,
  "context_length" bigint NULL DEFAULT 0,
  "max_output_tokens" bigint NULL DEFAULT 0,
  "input_price_per_million" numeric(14,6) NULL DEFAULT 0,
  "output_price_per_million" numeric(14,6) NULL DEFAULT 0,
  "cache_read_price" numeric(14,6) NULL,
  "cache_write_price" numeric(14,6) NULL,
  "tier_threshold" bigint NULL,
  "input_price_above_tier" numeric(14,6) NULL,
  "output_price_above_tier" numeric(14,6) NULL,
  "supports_function_calling" boolean NULL DEFAULT false,
  "supports_vision" boolean NULL DEFAULT false,
  "supports_prompt_caching" boolean NULL DEFAULT false,
  "supports_response_schema" boolean NULL DEFAULT false,
  "supports_tool_choice" boolean NULL DEFAULT false,
  "supports_parallel_function_calling" boolean NULL DEFAULT false,
  "deprecation_date" timestamptz NULL,
  "raw_payload" text NULL,
  "synced_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_catalog_provider" to table: "model_catalog"
CREATE INDEX "idx_catalog_provider" ON "model_catalog" ("provider");
-- Create index "idx_catalog_source_id" to table: "model_catalog"
CREATE UNIQUE INDEX "idx_catalog_source_id" ON "model_catalog" ("source", "source_model_id");
-- Create "provider_definitions" table
CREATE TABLE "provider_definitions" (
  "id" character varying(50) NOT NULL,
  "display_name" character varying(100) NOT NULL,
  "adapter" character varying(50) NOT NULL,
  "base_url" character varying(500) NOT NULL DEFAULT '',
  "credential_schema" jsonb NOT NULL DEFAULT '{}',
  "vendor_options" jsonb NOT NULL DEFAULT '{}',
  "description" character varying(500) NOT NULL DEFAULT '',
  "icon_url" character varying(500) NULL DEFAULT '',
  "doc_url" character varying(500) NULL DEFAULT '',
  "default_models" jsonb NOT NULL DEFAULT '[]',
  "builtin" boolean NOT NULL DEFAULT false,
  "enabled" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
-- Create "providers" table
CREATE TABLE "providers" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" character varying(100) NOT NULL,
  "definition_id" character varying(50) NULL,
  "type" character varying(50) NOT NULL,
  "endpoint" character varying(500) NOT NULL,
  "encrypted_credentials" text NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "weight" bigint NOT NULL DEFAULT 1,
  "priority" bigint NOT NULL DEFAULT 0,
  "status" bigint NOT NULL DEFAULT 1,
  "test_model" character varying(200) NULL DEFAULT '',
  "response_time" bigint NULL DEFAULT 0,
  "balance" numeric(12,4) NULL DEFAULT 0,
  "remark" character varying(500) NULL DEFAULT '',
  "max_retries" bigint NOT NULL DEFAULT 3,
  "timeout" bigint NOT NULL DEFAULT 30,
  "tag" character varying(100) NULL DEFAULT '',
  "consecutive_failures" bigint NOT NULL DEFAULT 0,
  "last_check_error" character varying(500) NULL DEFAULT '',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_providers_definition" FOREIGN KEY ("definition_id") REFERENCES "provider_definitions" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_providers_name_unique" to table: "providers"
CREATE UNIQUE INDEX "idx_providers_name_unique" ON "providers" ("name");
-- Create "models" table
CREATE TABLE "models" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" character varying(200) NOT NULL,
  "provider_id" uuid NOT NULL,
  "model_type" character varying(50) NOT NULL DEFAULT 'chat',
  "enabled" boolean NOT NULL DEFAULT true,
  "upstream_model_id" character varying(200) NULL DEFAULT '',
  "description" character varying(500) NULL DEFAULT '',
  "context_length" bigint NULL DEFAULT 0,
  "max_output_tokens" bigint NULL DEFAULT 0,
  "input_price" numeric(14,6) NULL DEFAULT 0,
  "output_price" numeric(14,6) NULL DEFAULT 0,
  "price_mode" character varying(10) NOT NULL DEFAULT 'catalog',
  "weight" bigint NOT NULL DEFAULT 1,
  "catalog_id" uuid NULL,
  "ctx_overridden" boolean NOT NULL DEFAULT false,
  "max_output_overridden" boolean NOT NULL DEFAULT false,
  "consecutive_failures" bigint NOT NULL DEFAULT 0,
  "last_check_error" character varying(500) NULL DEFAULT '',
  "response_time" bigint NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_models_catalog" FOREIGN KEY ("catalog_id") REFERENCES "model_catalog" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "fk_providers_models" FOREIGN KEY ("provider_id") REFERENCES "providers" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "idx_models_catalog_id" to table: "models"
CREATE INDEX "idx_models_catalog_id" ON "models" ("catalog_id");
-- Create index "idx_models_enabled" to table: "models"
CREATE INDEX "idx_models_enabled" ON "models" ("enabled");
-- Create index "idx_models_name_provider_unique" to table: "models"
CREATE UNIQUE INDEX "idx_models_name_provider_unique" ON "models" ("name", "provider_id");
-- Create index "idx_models_provider_id" to table: "models"
CREATE INDEX "idx_models_provider_id" ON "models" ("provider_id");
