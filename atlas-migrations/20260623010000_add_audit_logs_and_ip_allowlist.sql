CREATE TABLE "audit_logs" (
  "id"            uuid          NOT NULL DEFAULT gen_random_uuid(),
  "user_id"       uuid          NOT NULL,
  "user_name"     varchar(128)  NOT NULL DEFAULT '',
  "action"        varchar(64)   NOT NULL,
  "resource_type" varchar(32)   NOT NULL,
  "resource_id"   text          NOT NULL DEFAULT '',
  "resource_name" varchar(255)  NOT NULL DEFAULT '',
  "dept_id"       text          NOT NULL DEFAULT '',
  "dept_name"     varchar(128)  NOT NULL DEFAULT '',
  "status_code"   integer       NOT NULL DEFAULT 200,
  "client_ip"     varchar(64)   NOT NULL DEFAULT '',
  "user_agent"    text          NOT NULL DEFAULT '',
  "details"       text          NOT NULL DEFAULT '',
  "created_at"    timestamptz   NOT NULL,
  PRIMARY KEY ("id")
);
CREATE INDEX "idx_audit_logs_user_id"       ON "audit_logs" ("user_id");
CREATE INDEX "idx_audit_logs_action"        ON "audit_logs" ("action");
CREATE INDEX "idx_audit_logs_resource_type" ON "audit_logs" ("resource_type");
CREATE INDEX "idx_audit_logs_dept_id"       ON "audit_logs" ("dept_id");
CREATE INDEX "idx_audit_logs_created_at"    ON "audit_logs" ("created_at");

ALTER TABLE "api_keys"
  ADD COLUMN IF NOT EXISTS "ip_allowlist" text NOT NULL DEFAULT '';
