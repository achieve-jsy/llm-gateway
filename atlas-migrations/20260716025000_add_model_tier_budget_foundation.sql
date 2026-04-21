-- P0 model-tier budget foundation.
-- Generated from GORM; unrelated pre-existing drift was removed during review.
-- Expand keeps idx_budget_alert; P3 pre-enable Contract migration owns its removal.
ALTER TABLE "call_logs"
  ADD COLUMN IF NOT EXISTS "key_type" character varying(16) NOT NULL DEFAULT 'personal',
  ADD COLUMN IF NOT EXISTS "budget_tier_id" uuid NULL,
  ADD COLUMN IF NOT EXISTS "budget_tier_name_snapshot" character varying(64) NULL,
  ADD COLUMN IF NOT EXISTS "budget_tier_accounting_eligible" boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS "budget_tier_constraints_snapshot" jsonb NOT NULL DEFAULT '[]',
  ADD COLUMN IF NOT EXISTS "budget_block_code" character varying(64) NULL,
  ADD COLUMN IF NOT EXISTS "budget_block_dimension" character varying(8) NULL,
  ADD COLUMN IF NOT EXISTS "budget_block_scope_type" character varying(32) NULL,
  ADD COLUMN IF NOT EXISTS "budget_block_scope_id" character varying(128) NULL,
  ADD COLUMN IF NOT EXISTS "budget_block_period_type" character varying(8) NULL;
CREATE INDEX IF NOT EXISTS "idx_call_logs_key_type" ON "call_logs" ("key_type");

CREATE TABLE "model_tiers" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" character varying(64) NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "color" character varying(7) NOT NULL DEFAULT '#64748B',
  "sort_order" bigint NOT NULL DEFAULT 0,
  "enabled" boolean NOT NULL DEFAULT true,
  "archived_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
CREATE INDEX "idx_model_tiers_archived_at" ON "model_tiers" ("archived_at");
CREATE INDEX "idx_model_tiers_sort" ON "model_tiers" ("sort_order");
CREATE UNIQUE INDEX "uniq_model_tiers_active_name"
  ON "model_tiers" ((lower((name)::text))) WHERE (archived_at IS NULL);

ALTER TABLE "budget_alert_records"
  ADD CONSTRAINT "chk_budget_alert_dimension"
    CHECK ((dimension_type = 'total' AND model_tier_id IS NULL) OR
           (dimension_type = 'tier' AND model_tier_id IS NOT NULL)) NOT VALID,
  ALTER COLUMN "scope_type" TYPE character varying(32),
  ALTER COLUMN "scope_id" TYPE character varying(255),
  ALTER COLUMN "period_type" TYPE character varying(16),
  ALTER COLUMN "period" TYPE character varying(16),
  ADD COLUMN "dimension_key" character varying(48) NOT NULL DEFAULT 'total',
  ADD COLUMN "dimension_type" character varying(8) NOT NULL DEFAULT 'total',
  ADD COLUMN "model_tier_id" uuid NULL,
  ADD CONSTRAINT "fk_budget_alert_records_model_tier"
    FOREIGN KEY ("model_tier_id") REFERENCES "model_tiers" ("id") ON DELETE RESTRICT NOT VALID;

CREATE TABLE "budget_limit_schedules" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "budget_allocation_id" uuid NOT NULL,
  "dimension_type" character varying(8) NOT NULL,
  "dimension_key" character varying(48) NOT NULL,
  "model_tier_id" uuid NULL,
  "period_type" character varying(8) NOT NULL,
  "target_limit_usd" numeric(18,6) NOT NULL,
  "effective_at" timestamptz NOT NULL,
  "reason" text NOT NULL,
  "created_by" uuid NOT NULL,
  "status" character varying(16) NOT NULL DEFAULT 'pending',
  "failure_reason" text NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_budget_limit_schedules_budget_allocation"
    FOREIGN KEY ("budget_allocation_id") REFERENCES "budget_allocations" ("id") ON DELETE RESTRICT,
  CONSTRAINT "fk_budget_limit_schedules_model_tier"
    FOREIGN KEY ("model_tier_id") REFERENCES "model_tiers" ("id") ON DELETE RESTRICT,
  CONSTRAINT "chk_budget_limit_schedules_dimension_type" CHECK (dimension_type IN ('total','tier')),
  CONSTRAINT "chk_budget_limit_schedules_period_type" CHECK (period_type IN ('daily','weekly','monthly')),
  CONSTRAINT "chk_budget_limit_schedules_status" CHECK (status IN ('pending','applied','replaced','cancelled','failed')),
  CONSTRAINT "chk_budget_limit_schedules_target_limit_usd" CHECK (target_limit_usd >= 0),
  CONSTRAINT "chk_budget_schedule_dimension"
    CHECK ((dimension_type = 'total' AND model_tier_id IS NULL) OR
           (dimension_type = 'tier' AND model_tier_id IS NOT NULL)),
  CONSTRAINT "chk_budget_schedule_dimension_key"
    CHECK ((dimension_type = 'total' AND dimension_key = 'total') OR
           (dimension_type = 'tier' AND dimension_key = ('tier:' || model_tier_id::text)))
);
CREATE INDEX "idx_budget_limit_schedules_effective_at" ON "budget_limit_schedules" ("effective_at");
CREATE INDEX "idx_budget_limit_schedules_model_tier_id" ON "budget_limit_schedules" ("model_tier_id");
CREATE INDEX "idx_budget_limit_schedules_status" ON "budget_limit_schedules" ("status");
CREATE UNIQUE INDEX "uniq_budget_schedule_pending"
  ON "budget_limit_schedules" ("budget_allocation_id", "dimension_key", "period_type")
  WHERE "status" = 'pending';

CREATE TABLE "budget_temporary_overrides" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "budget_allocation_id" uuid NOT NULL,
  "dimension_type" character varying(8) NOT NULL,
  "dimension_key" character varying(48) NOT NULL,
  "model_tier_id" uuid NULL,
  "period_type" character varying(8) NOT NULL,
  "target_limit_usd" numeric(18,6) NOT NULL,
  "starts_at" timestamptz NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "expansion_request_id" uuid NULL,
  "status" character varying(16) NOT NULL DEFAULT 'active',
  "failure_reason" text NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_budget_temporary_overrides_budget_allocation"
    FOREIGN KEY ("budget_allocation_id") REFERENCES "budget_allocations" ("id") ON DELETE RESTRICT,
  CONSTRAINT "fk_budget_temporary_overrides_model_tier"
    FOREIGN KEY ("model_tier_id") REFERENCES "model_tiers" ("id") ON DELETE RESTRICT,
  CONSTRAINT "chk_budget_temporary_overrides_dimension_type" CHECK (dimension_type IN ('total','tier')),
  CONSTRAINT "chk_budget_temporary_overrides_period_type" CHECK (period_type IN ('daily','weekly','monthly')),
  CONSTRAINT "chk_budget_temporary_overrides_status" CHECK (status IN ('active','expired','cancelled','failed')),
  CONSTRAINT "chk_budget_temporary_overrides_target_limit_usd" CHECK (target_limit_usd >= 0),
  CONSTRAINT "chk_budget_override_window" CHECK (expires_at > starts_at),
  CONSTRAINT "chk_budget_override_dimension"
    CHECK ((dimension_type = 'total' AND model_tier_id IS NULL) OR
           (dimension_type = 'tier' AND model_tier_id IS NOT NULL)),
  CONSTRAINT "chk_budget_override_dimension_key"
    CHECK ((dimension_type = 'total' AND dimension_key = 'total') OR
           (dimension_type = 'tier' AND dimension_key = ('tier:' || model_tier_id::text)))
);
CREATE UNIQUE INDEX "idx_budget_temporary_overrides_expansion_request_id"
  ON "budget_temporary_overrides" ("expansion_request_id");
CREATE INDEX "idx_budget_temporary_overrides_expires_at" ON "budget_temporary_overrides" ("expires_at");
CREATE INDEX "idx_budget_temporary_overrides_model_tier_id" ON "budget_temporary_overrides" ("model_tier_id");
CREATE INDEX "idx_budget_temporary_overrides_status" ON "budget_temporary_overrides" ("status");
CREATE UNIQUE INDEX "uniq_budget_override_active"
  ON "budget_temporary_overrides" ("budget_allocation_id", "dimension_key", "period_type")
  WHERE "status" = 'active';

CREATE TABLE "budget_tier_limits" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "budget_allocation_id" uuid NOT NULL,
  "model_tier_id" uuid NOT NULL,
  "period_type" character varying(8) NOT NULL,
  "limit_usd" numeric(18,6) NOT NULL,
  "effective_at" timestamptz NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_budget_tier_limits_budget_allocation"
    FOREIGN KEY ("budget_allocation_id") REFERENCES "budget_allocations" ("id") ON DELETE RESTRICT,
  CONSTRAINT "fk_budget_tier_limits_model_tier"
    FOREIGN KEY ("model_tier_id") REFERENCES "model_tiers" ("id") ON DELETE RESTRICT,
  CONSTRAINT "chk_budget_tier_limits_limit_usd" CHECK (limit_usd >= 0),
  CONSTRAINT "chk_budget_tier_limits_period_type" CHECK (period_type IN ('daily','weekly','monthly'))
);
CREATE UNIQUE INDEX "uniq_budget_tier_limit"
  ON "budget_tier_limits" ("budget_allocation_id", "model_tier_id", "period_type");

ALTER TABLE "expansion_requests"
  ADD CONSTRAINT "chk_expansion_dimension"
    CHECK ((dimension_type = 'total' AND model_tier_id IS NULL) OR
           (dimension_type = 'tier' AND model_tier_id IS NOT NULL)) NOT VALID,
  ADD CONSTRAINT "chk_expansion_dimension_key"
    CHECK ((dimension_type = 'total' AND dimension_key = 'total') OR
           (dimension_type = 'tier' AND dimension_key = ('tier:' || model_tier_id::text))) NOT VALID,
  ADD CONSTRAINT "chk_expansion_requests_duration_type" CHECK (duration_type IN ('permanent','current_period')) NOT VALID,
  ADD CONSTRAINT "chk_expansion_requests_period_type" CHECK (period_type IN ('daily','weekly','monthly')) NOT VALID,
  ADD CONSTRAINT "chk_expansion_requests_priority" CHECK (priority IN ('normal','urgent')) NOT VALID,
  ADD CONSTRAINT "chk_expansion_requests_sync_status" CHECK (sync_status IN ('pending','synced','failed')) NOT VALID,
  ALTER COLUMN "status" TYPE character varying(24),
  ADD COLUMN "dimension_type" character varying(8) NOT NULL DEFAULT 'total',
  ADD COLUMN "dimension_key" character varying(48) NOT NULL DEFAULT 'total',
  ADD COLUMN "model_tier_id" uuid NULL,
  ADD COLUMN "duration_type" character varying(16) NOT NULL DEFAULT 'permanent',
  ADD COLUMN "priority" character varying(16) NOT NULL DEFAULT 'normal',
  ADD COLUMN "current_amount_snapshot" numeric(18,6) NOT NULL DEFAULT 0,
  ADD COLUMN "used_amount_snapshot" numeric(18,6) NOT NULL DEFAULT 0,
  ADD COLUMN "precheck_version" character varying(64) NULL,
  ADD COLUMN "prechecked_at" timestamptz NULL,
  ADD COLUMN "expires_at" timestamptz NULL,
  ADD COLUMN "applied_at" timestamptz NULL,
  ADD COLUMN "applied_target_type" character varying(32) NULL,
  ADD COLUMN "applied_target_id" uuid NULL,
  ADD COLUMN "sync_status" character varying(16) NOT NULL DEFAULT 'pending',
  ADD COLUMN "urgent_reason" text NOT NULL DEFAULT '',
  ADD CONSTRAINT "fk_expansion_requests_model_tier"
    FOREIGN KEY ("model_tier_id") REFERENCES "model_tiers" ("id") ON DELETE RESTRICT NOT VALID;

ALTER TABLE "model_groups"
  ADD COLUMN "budget_tier_id" uuid NULL,
  ADD CONSTRAINT "fk_model_groups_budget_tier"
    FOREIGN KEY ("budget_tier_id") REFERENCES "model_tiers" ("id") ON DELETE RESTRICT NOT VALID;
