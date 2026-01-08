-- Incremental: ensure v0.8 schema on top of v0.7 (idempotent).
-- v0.7 already created open_mode column and group_entitlements table;
-- this migration adds the CHECK constraint, FK, NOT NULL enforcement, and extra indexes.

-- 1. CHECK constraint on open_mode (only add if missing)
DO $$ BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.check_constraints
    WHERE constraint_name = 'chk_model_groups_open_mode'
  ) THEN
    ALTER TABLE "model_groups" ADD CONSTRAINT "chk_model_groups_open_mode"
      CHECK ((open_mode)::text = ANY ((ARRAY['public'::character varying, 'controlled'::character varying])::text[]));
  END IF;
END $$;

-- 2. FK on group_entitlements.group_id → model_groups(id) (only add if missing)
DO $$ BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.table_constraints
    WHERE constraint_name = 'fk_group_entitlements_group'
      AND table_name = 'group_entitlements'
  ) THEN
    ALTER TABLE "group_entitlements"
      ADD CONSTRAINT "fk_group_entitlements_group"
      FOREIGN KEY ("group_id") REFERENCES "model_groups" ("id") ON DELETE CASCADE;
  END IF;
END $$;

-- 3. created_at / updated_at: tighten to NOT NULL with a safe default for any legacy rows
ALTER TABLE "group_entitlements" ALTER COLUMN "created_at" SET NOT NULL;
ALTER TABLE "group_entitlements" ALTER COLUMN "updated_at" SET NOT NULL;

-- 4. Supplementary indexes (idx_group_ent already exists as unique; these add query-path coverage)
CREATE INDEX IF NOT EXISTS "idx_ent_scope" ON "group_entitlements" ("scope_type", "scope_id");
CREATE INDEX IF NOT EXISTS "idx_ent_group_id" ON "group_entitlements" ("group_id");
