-- Add open_mode column to model_groups and create group_entitlements table for 003-group-authorization

-- model_groups: open_mode field
ALTER TABLE "model_groups" ADD COLUMN "open_mode" character varying(16) NOT NULL DEFAULT 'public';
ALTER TABLE "model_groups" ADD CONSTRAINT "chk_model_groups_open_mode" CHECK ((open_mode)::text = ANY ((ARRAY['public'::character varying, 'controlled'::character varying])::text[]));

-- group_entitlements table
CREATE TABLE "group_entitlements" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "group_id" bigint NOT NULL,
  "scope_type" character varying(16) NOT NULL,
  "scope_id" character varying(64) NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "granted_by" uuid NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_group_entitlements_group" FOREIGN KEY ("group_id") REFERENCES "model_groups" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE INDEX "idx_ent_scope" ON "group_entitlements" ("scope_type", "scope_id");
CREATE UNIQUE INDEX "idx_ent_unique" ON "group_entitlements" ("group_id", "scope_type", "scope_id");
CREATE INDEX "idx_ent_group_id" ON "group_entitlements" ("group_id");
