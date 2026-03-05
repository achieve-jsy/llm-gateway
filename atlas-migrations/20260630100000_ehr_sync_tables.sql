-- Create "ehr_organizations" table
CREATE TABLE "public"."ehr_organizations" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "org_id" character varying(64) NOT NULL,
  "org_name" character varying(255) NOT NULL,
  "org_code" character varying(64) NOT NULL DEFAULT '',
  "ehr_create_date" character varying(64) NOT NULL DEFAULT '',
  "ehr_modify_date" character varying(64) NOT NULL DEFAULT '',
  "synced_at" timestamptz NOT NULL DEFAULT NOW(),
  "created_at" timestamptz NOT NULL DEFAULT NOW(),
  "updated_at" timestamptz NOT NULL DEFAULT NOW(),
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "idx_ehr_org_org_id" ON "public"."ehr_organizations" ("org_id");
CREATE INDEX "idx_ehr_org_modify_date" ON "public"."ehr_organizations" ("ehr_modify_date");

-- Create "ehr_companies" table
CREATE TABLE "public"."ehr_companies" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "org_id" character varying(64) NOT NULL,
  "company_id" character varying(64) NOT NULL,
  "company_code" character varying(64) NOT NULL DEFAULT '',
  "company_name" character varying(255) NOT NULL,
  "parent_id" character varying(64) NOT NULL DEFAULT '',
  "ehr_modify_date" character varying(64) NOT NULL DEFAULT '',
  "synced_at" timestamptz NOT NULL DEFAULT NOW(),
  "created_at" timestamptz NOT NULL DEFAULT NOW(),
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "idx_ehr_company_org_company" ON "public"."ehr_companies" ("org_id", "company_id");
CREATE INDEX "idx_ehr_company_org_id" ON "public"."ehr_companies" ("org_id");
CREATE INDEX "idx_ehr_company_parent" ON "public"."ehr_companies" ("parent_id");
CREATE INDEX "idx_ehr_company_modify_date" ON "public"."ehr_companies" ("ehr_modify_date");

-- Create "ehr_departments" table
CREATE TABLE "public"."ehr_departments" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "org_id" character varying(64) NOT NULL,
  "department_id" character varying(64) NOT NULL,
  "department_code" character varying(64) NOT NULL DEFAULT '',
  "department_name" character varying(255) NOT NULL,
  "department_type" integer NOT NULL DEFAULT 0,
  "department_type_name" character varying(64) NOT NULL DEFAULT '',
  "parent_id" character varying(64) NOT NULL DEFAULT '',
  "leader_name" character varying(128) NOT NULL DEFAULT '',
  "leader_passport" character varying(128) NOT NULL DEFAULT '',
  "leader_person_id" character varying(64) NOT NULL DEFAULT '',
  "leader_employee_id" character varying(64) NOT NULL DEFAULT '',
  "manager_name" character varying(128) NOT NULL DEFAULT '',
  "manager_passport" character varying(128) NOT NULL DEFAULT '',
  "manager_person_id" character varying(64) NOT NULL DEFAULT '',
  "manager_employee_id" character varying(64) NOT NULL DEFAULT '',
  "hr_bp_name" character varying(128) NOT NULL DEFAULT '',
  "hr_bp_employee_id" character varying(64) NOT NULL DEFAULT '',
  "ehr_modify_date" character varying(64) NOT NULL DEFAULT '',
  "manual_override_at" timestamptz NULL,
  "manual_override_by" character varying(128) NOT NULL DEFAULT '',
  "manual_override_reason" character varying(500) NOT NULL DEFAULT '',
  "synced_at" timestamptz NOT NULL DEFAULT NOW(),
  "created_at" timestamptz NOT NULL DEFAULT NOW(),
  "updated_at" timestamptz NOT NULL DEFAULT NOW(),
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "idx_ehr_dept_org_dept" ON "public"."ehr_departments" ("org_id", "department_id");
CREATE INDEX "idx_ehr_dept_org_id" ON "public"."ehr_departments" ("org_id");
CREATE INDEX "idx_ehr_dept_parent" ON "public"."ehr_departments" ("parent_id");
CREATE INDEX "idx_ehr_dept_leader_emp" ON "public"."ehr_departments" ("leader_employee_id");
CREATE INDEX "idx_ehr_dept_manager_emp" ON "public"."ehr_departments" ("manager_employee_id");
CREATE INDEX "idx_ehr_dept_modify_date" ON "public"."ehr_departments" ("ehr_modify_date");

-- Create "ehr_employees" table
CREATE TABLE "public"."ehr_employees" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "org_id" character varying(64) NOT NULL,
  "employee_id" character varying(64) NOT NULL,
  "person_id" character varying(64) NOT NULL DEFAULT '',
  "passport" character varying(128) NOT NULL DEFAULT '',
  "appoint_name" character varying(128) NOT NULL DEFAULT '',
  "appoint_code" character varying(64) NOT NULL DEFAULT '',
  "office" character varying(128) NOT NULL DEFAULT '',
  "email" character varying(255) NOT NULL DEFAULT '',
  "person_status" integer NOT NULL DEFAULT -1,
  "person_type" integer NOT NULL DEFAULT 0,
  "is_default" boolean NOT NULL DEFAULT false,
  "company_id" character varying(64) NOT NULL DEFAULT '',
  "company_name" character varying(255) NOT NULL DEFAULT '',
  "department_id" character varying(64) NOT NULL DEFAULT '',
  "department_name" character varying(255) NOT NULL DEFAULT '',
  "position_id" character varying(64) NOT NULL DEFAULT '',
  "position_name" character varying(128) NOT NULL DEFAULT '',
  "position_category_id" character varying(64) NOT NULL DEFAULT '',
  "manager_employee_id" character varying(64) NOT NULL DEFAULT '',
  "manager_person_id" character varying(64) NOT NULL DEFAULT '',
  "manager_passport" character varying(128) NOT NULL DEFAULT '',
  "manager_name" character varying(128) NOT NULL DEFAULT '',
  "person_name" character varying(128) NOT NULL DEFAULT '',
  "group_e_number" character varying(64) NOT NULL DEFAULT '',
  "gender" integer NOT NULL DEFAULT -1,
  "join_date" character varying(32) NOT NULL DEFAULT '',
  "on_board_date" character varying(32) NOT NULL DEFAULT '',
  "location" character varying(128) NOT NULL DEFAULT '',
  "dismiss_date" character varying(32) NOT NULL DEFAULT '',
  "ehr_modify_date" character varying(64) NOT NULL DEFAULT '',
  "manual_override_at" timestamptz NULL,
  "manual_override_by" character varying(128) NOT NULL DEFAULT '',
  "manual_override_reason" character varying(500) NOT NULL DEFAULT '',
  "synced_at" timestamptz NOT NULL DEFAULT NOW(),
  "created_at" timestamptz NOT NULL DEFAULT NOW(),
  "updated_at" timestamptz NOT NULL DEFAULT NOW(),
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "idx_ehr_emp_org_emp" ON "public"."ehr_employees" ("org_id", "employee_id");
CREATE INDEX "idx_ehr_emp_org_id" ON "public"."ehr_employees" ("org_id");
CREATE INDEX "idx_ehr_emp_person_id" ON "public"."ehr_employees" ("person_id");
CREATE UNIQUE INDEX "idx_ehr_emp_passport" ON "public"."ehr_employees" ("passport");
CREATE INDEX "idx_ehr_emp_status" ON "public"."ehr_employees" ("person_status");
CREATE INDEX "idx_ehr_emp_type" ON "public"."ehr_employees" ("person_type");
CREATE INDEX "idx_ehr_emp_company" ON "public"."ehr_employees" ("company_id");
CREATE INDEX "idx_ehr_emp_dept" ON "public"."ehr_employees" ("department_id");
CREATE INDEX "idx_ehr_emp_position" ON "public"."ehr_employees" ("position_id");
CREATE INDEX "idx_ehr_emp_manager" ON "public"."ehr_employees" ("manager_employee_id");
CREATE INDEX "idx_ehr_emp_enumber" ON "public"."ehr_employees" ("group_e_number");
CREATE INDEX "idx_ehr_emp_modify_date" ON "public"."ehr_employees" ("ehr_modify_date");

-- Create "ehr_positions" table
CREATE TABLE "public"."ehr_positions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "org_id" character varying(64) NOT NULL,
  "position_id" character varying(64) NOT NULL,
  "name" character varying(255) NOT NULL,
  "type" integer NOT NULL DEFAULT 0,
  "name_path" text NOT NULL DEFAULT '',
  "parent_id" character varying(64) NOT NULL DEFAULT '',
  "synced_at" timestamptz NOT NULL DEFAULT NOW(),
  "created_at" timestamptz NOT NULL DEFAULT NOW(),
  "updated_at" timestamptz NOT NULL DEFAULT NOW(),
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "idx_ehr_pos_org_pos" ON "public"."ehr_positions" ("org_id", "position_id");
CREATE INDEX "idx_ehr_pos_org_id" ON "public"."ehr_positions" ("org_id");
CREATE INDEX "idx_ehr_pos_parent" ON "public"."ehr_positions" ("parent_id");

-- Create "ehr_sync_execution_logs" table
CREATE TABLE "public"."ehr_sync_execution_logs" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "batch_id" character varying(64) NOT NULL,
  "entity" character varying(32) NOT NULL,
  "org_id" character varying(64) NOT NULL,
  "sync_type" character varying(16) NOT NULL,
  "status" character varying(16) NOT NULL,
  "trigger_type" character varying(16) NOT NULL DEFAULT 'cron',
  "trigger_user" character varying(128) NOT NULL DEFAULT '',
  "started_at" timestamptz NOT NULL,
  "finished_at" timestamptz NULL,
  "duration_ms" bigint NOT NULL DEFAULT 0,
  "records_fetched" bigint NOT NULL DEFAULT 0,
  "records_created" bigint NOT NULL DEFAULT 0,
  "records_updated" bigint NOT NULL DEFAULT 0,
  "records_skipped" bigint NOT NULL DEFAULT 0,
  "records_deleted" bigint NOT NULL DEFAULT 0,
  "error_msg" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT NOW(),
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "idx_ehr_exec_batch" ON "public"."ehr_sync_execution_logs" ("batch_id");
CREATE INDEX "idx_ehr_exec_entity" ON "public"."ehr_sync_execution_logs" ("entity");
CREATE INDEX "idx_ehr_exec_org" ON "public"."ehr_sync_execution_logs" ("org_id");
CREATE INDEX "idx_ehr_exec_type" ON "public"."ehr_sync_execution_logs" ("sync_type");
CREATE INDEX "idx_ehr_exec_status" ON "public"."ehr_sync_execution_logs" ("status");
CREATE INDEX "idx_ehr_exec_created" ON "public"."ehr_sync_execution_logs" ("created_at");

-- Create "ehr_sync_change_logs" table
CREATE TABLE "public"."ehr_sync_change_logs" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "batch_id" character varying(64) NOT NULL,
  "entity" character varying(32) NOT NULL,
  "entity_id" character varying(64) NOT NULL,
  "org_id" character varying(64) NOT NULL,
  "change_type" character varying(16) NOT NULL,
  "old_value" jsonb NOT NULL DEFAULT '{}',
  "new_value" jsonb NOT NULL DEFAULT '{}',
  "source" character varying(16) NOT NULL,
  "occurred_at" timestamptz NOT NULL DEFAULT NOW(),
  PRIMARY KEY ("id")
);
CREATE INDEX "idx_ehr_change_batch" ON "public"."ehr_sync_change_logs" ("batch_id");
CREATE INDEX "idx_ehr_change_entity" ON "public"."ehr_sync_change_logs" ("entity");
CREATE INDEX "idx_ehr_change_entity_id" ON "public"."ehr_sync_change_logs" ("entity_id");
CREATE INDEX "idx_ehr_change_org" ON "public"."ehr_sync_change_logs" ("org_id");
CREATE INDEX "idx_ehr_change_type" ON "public"."ehr_sync_change_logs" ("change_type");
CREATE INDEX "idx_ehr_change_occurred" ON "public"."ehr_sync_change_logs" ("occurred_at");

-- Create "ehr_sync_error_logs" table
CREATE TABLE "public"."ehr_sync_error_logs" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "batch_id" character varying(64) NOT NULL,
  "entity" character varying(32) NOT NULL,
  "org_id" character varying(64) NOT NULL,
  "method" character varying(255) NOT NULL,
  "query_params" text NOT NULL DEFAULT '',
  "status_code" bigint NOT NULL DEFAULT 0,
  "response" text NOT NULL DEFAULT '',
  "error_msg" text NOT NULL DEFAULT '',
  "attempt_num" bigint NOT NULL DEFAULT 1,
  "max_retries" bigint NOT NULL DEFAULT 3,
  "backoff_ms" bigint NOT NULL DEFAULT 0,
  "stack_trace" text NOT NULL DEFAULT '',
  "occurred_at" timestamptz NOT NULL DEFAULT NOW(),
  PRIMARY KEY ("id")
);
CREATE INDEX "idx_ehr_err_batch" ON "public"."ehr_sync_error_logs" ("batch_id");
CREATE INDEX "idx_ehr_err_entity" ON "public"."ehr_sync_error_logs" ("entity");
CREATE INDEX "idx_ehr_err_org" ON "public"."ehr_sync_error_logs" ("org_id");
CREATE INDEX "idx_ehr_err_occurred" ON "public"."ehr_sync_error_logs" ("occurred_at");

-- Create "ehr_sync_diff_reports" table
CREATE TABLE "public"."ehr_sync_diff_reports" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "batch_id" character varying(64) NOT NULL,
  "org_id" character varying(64) NOT NULL,
  "entity" character varying(32) NOT NULL,
  "total_in_ehr" bigint NOT NULL DEFAULT 0,
  "total_in_local" bigint NOT NULL DEFAULT 0,
  "created_count" bigint NOT NULL DEFAULT 0,
  "modified_count" bigint NOT NULL DEFAULT 0,
  "missing_count" bigint NOT NULL DEFAULT 0,
  "consistent_count" bigint NOT NULL DEFAULT 0,
  "override_count" bigint NOT NULL DEFAULT 0,
  "status" character varying(16) NOT NULL,
  "message" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT NOW(),
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "idx_ehr_diff_batch" ON "public"."ehr_sync_diff_reports" ("batch_id");
CREATE INDEX "idx_ehr_diff_org" ON "public"."ehr_sync_diff_reports" ("org_id");
CREATE INDEX "idx_ehr_diff_entity" ON "public"."ehr_sync_diff_reports" ("entity");
CREATE INDEX "idx_ehr_diff_created" ON "public"."ehr_sync_diff_reports" ("created_at");

-- Create "ehr_sync_skip_logs" table
CREATE TABLE "public"."ehr_sync_skip_logs" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "batch_id" character varying(64) NOT NULL,
  "entity" character varying(32) NOT NULL,
  "entity_id" character varying(64) NOT NULL,
  "org_id" character varying(64) NOT NULL,
  "override_at" timestamptz NOT NULL,
  "override_by" character varying(128) NOT NULL DEFAULT '',
  "local_value" jsonb NOT NULL DEFAULT '{}',
  "ehr_value" jsonb NOT NULL DEFAULT '{}',
  "differing_fields" jsonb NOT NULL DEFAULT '[]',
  "created_at" timestamptz NOT NULL DEFAULT NOW(),
  PRIMARY KEY ("id")
);
CREATE INDEX "idx_ehr_skip_batch" ON "public"."ehr_sync_skip_logs" ("batch_id");
CREATE INDEX "idx_ehr_skip_entity" ON "public"."ehr_sync_skip_logs" ("entity");
CREATE INDEX "idx_ehr_skip_entity_id" ON "public"."ehr_sync_skip_logs" ("entity_id");
CREATE INDEX "idx_ehr_skip_org" ON "public"."ehr_sync_skip_logs" ("org_id");
CREATE INDEX "idx_ehr_skip_created" ON "public"."ehr_sync_skip_logs" ("created_at");

-- Create "ehr_sync_stat_snapshots" table
CREATE TABLE "public"."ehr_sync_stat_snapshots" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "org_id" character varying(64) NOT NULL,
  "synced_at" timestamptz NOT NULL,
  "total_organizations" bigint NOT NULL DEFAULT 0,
  "total_companies" bigint NOT NULL DEFAULT 0,
  "total_departments" bigint NOT NULL DEFAULT 0,
  "total_employees" bigint NOT NULL DEFAULT 0,
  "active_employees" bigint NOT NULL DEFAULT 0,
  "override_count" bigint NOT NULL DEFAULT 0,
  "pending_review_count" bigint NOT NULL DEFAULT 0,
  "last_incremental_ms" bigint NOT NULL DEFAULT 0,
  "last_full_reconcile_ms" bigint NOT NULL DEFAULT 0,
  "ehr_call_count" bigint NOT NULL DEFAULT 0,
  "ehr_error_count" bigint NOT NULL DEFAULT 0,
  PRIMARY KEY ("id")
);
CREATE INDEX "idx_ehr_stat_org" ON "public"."ehr_sync_stat_snapshots" ("org_id");
CREATE INDEX "idx_ehr_stat_synced" ON "public"."ehr_sync_stat_snapshots" ("synced_at");
