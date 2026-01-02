-- Modify "api_keys" table
ALTER TABLE "api_keys" ADD COLUMN "key_type" character varying(16) NOT NULL DEFAULT 'personal', ADD COLUMN "purpose" character varying(128) NOT NULL DEFAULT '', ADD COLUMN "created_by" uuid NULL;
-- Create index "idx_api_keys_type" to table: "api_keys"
CREATE INDEX "idx_api_keys_type" ON "api_keys" ("key_type");
-- Modify "health_check_configs" table
ALTER TABLE "health_check_configs" ALTER COLUMN "model_recovery_success_threshold" TYPE bigint;
-- Modify "model_groups" table
ALTER TABLE "model_groups" ALTER COLUMN "timeout_seconds" TYPE bigint, ADD COLUMN "open_mode" character varying(16) NOT NULL DEFAULT 'public';
-- Modify "models" table
ALTER TABLE "models" ALTER COLUMN "status" TYPE bigint, ALTER COLUMN "consecutive_recovery_successes" TYPE bigint;
-- Modify "users" table
ALTER TABLE "users" DROP CONSTRAINT "chk_users_source", ADD COLUMN "employee_id" character varying(64) NULL, ADD COLUMN "job_family" character varying(128) NULL, ADD COLUMN "job_category" character varying(128) NULL, ADD COLUMN "job_title" character varying(128) NULL, ADD COLUMN "dept_source" character varying(16) NOT NULL DEFAULT 'sso', ADD COLUMN "dept_updated_by" character varying(128) NULL, ADD COLUMN "dept_updated_at" timestamptz NULL;
-- Create index "idx_users_employee_id" to table: "users"
CREATE INDEX "idx_users_employee_id" ON "users" ("employee_id");
-- Create "budget_alert_records" table
CREATE TABLE "budget_alert_records" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "scope_type" character varying(16) NOT NULL,
  "scope_id" character varying(64) NOT NULL,
  "period_type" character varying(8) NOT NULL,
  "period" character varying(10) NOT NULL,
  "threshold" bigint NOT NULL,
  "notified_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_budget_alert" to table: "budget_alert_records"
CREATE UNIQUE INDEX "idx_budget_alert" ON "budget_alert_records" ("scope_type", "scope_id", "period_type", "period", "threshold");
-- Create "budget_allocations" table
CREATE TABLE "budget_allocations" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "scope_type" character varying(16) NOT NULL,
  "scope_id" character varying(64) NOT NULL,
  "amount_usd" numeric(12,4) NOT NULL DEFAULT 0,
  "period" character varying(8) NOT NULL DEFAULT 'monthly',
  "enabled" boolean NOT NULL DEFAULT true,
  "created_by" uuid NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_budget_alloc_scope" to table: "budget_allocations"
CREATE UNIQUE INDEX "idx_budget_alloc_scope" ON "budget_allocations" ("scope_type", "scope_id");
-- Create "budget_consumption" table
CREATE TABLE "budget_consumption" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "scope_type" character varying(16) NOT NULL,
  "scope_id" character varying(64) NOT NULL,
  "period" character varying(7) NOT NULL,
  "used_usd" numeric(14,6) NOT NULL DEFAULT 0,
  "synced_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_budget_cons" to table: "budget_consumption"
CREATE UNIQUE INDEX "idx_budget_cons" ON "budget_consumption" ("scope_type", "scope_id", "period");
-- Create "expansion_approval_steps" table
CREATE TABLE "expansion_approval_steps" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "request_id" uuid NOT NULL,
  "level" character varying(16) NOT NULL,
  "approver_id" uuid NULL,
  "action" character varying(16) NOT NULL,
  "comment" text NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_expansion_approval_steps_request_id" to table: "expansion_approval_steps"
CREATE INDEX "idx_expansion_approval_steps_request_id" ON "expansion_approval_steps" ("request_id");
-- Create "expansion_requests" table
CREATE TABLE "expansion_requests" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "scope_type" character varying(16) NOT NULL,
  "scope_id" character varying(64) NOT NULL,
  "requester_id" uuid NOT NULL,
  "current_amount" numeric(12,4) NULL,
  "requested_amount" numeric(12,4) NOT NULL,
  "reason" text NULL,
  "status" character varying(16) NOT NULL DEFAULT 'pending',
  "current_level" character varying(16) NOT NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_expansion_requests_requester_id" to table: "expansion_requests"
CREATE INDEX "idx_expansion_requests_requester_id" ON "expansion_requests" ("requester_id");
-- Create index "idx_expansion_requests_scope_id" to table: "expansion_requests"
CREATE INDEX "idx_expansion_requests_scope_id" ON "expansion_requests" ("scope_id");
-- Create index "idx_expansion_requests_status" to table: "expansion_requests"
CREATE INDEX "idx_expansion_requests_status" ON "expansion_requests" ("status");
-- Create "group_entitlements" table
CREATE TABLE "group_entitlements" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "group_id" bigint NOT NULL,
  "scope_type" character varying(16) NOT NULL,
  "scope_id" character varying(64) NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "granted_by" uuid NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_group_ent" to table: "group_entitlements"
CREATE UNIQUE INDEX "idx_group_ent" ON "group_entitlements" ("group_id", "scope_type", "scope_id");
-- Drop "model_status_logs" table
DROP TABLE "model_status_logs";
