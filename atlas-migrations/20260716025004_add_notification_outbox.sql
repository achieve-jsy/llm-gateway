-- Persistent, cross-instance notification delivery deduplication and retry state.
CREATE TABLE IF NOT EXISTS "notification_outbox" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "dedupe_key" character varying(255) NOT NULL,
  "user_id" uuid NOT NULL,
  "type" character varying(32) NOT NULL,
  "level" character varying(16) NOT NULL,
  "title" character varying(200) NOT NULL,
  "content" text NOT NULL,
  "ref_type" character varying(32) NOT NULL DEFAULT '',
  "ref_id" character varying(64) NOT NULL DEFAULT '',
  "status" character varying(16) NOT NULL DEFAULT 'pending',
  "attempts" bigint NOT NULL DEFAULT 0,
  "next_attempt" timestamptz NOT NULL DEFAULT now(),
  "last_error" text NOT NULL DEFAULT '',
  "locked_at" timestamptz NULL,
  "lease_owner" character varying(64) NOT NULL DEFAULT '',
  "lease_expires_at" bigint NOT NULL DEFAULT 0,
  "delivered_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "uniq_notification_outbox_dedupe"
  ON "notification_outbox" ("dedupe_key");
CREATE INDEX IF NOT EXISTS "idx_notification_outbox_status"
  ON "notification_outbox" ("status");
CREATE INDEX IF NOT EXISTS "idx_notification_outbox_next_attempt"
  ON "notification_outbox" ("next_attempt");
CREATE INDEX IF NOT EXISTS "idx_notification_outbox_locked_at"
  ON "notification_outbox" ("locked_at");
CREATE INDEX IF NOT EXISTS "idx_notification_outbox_lease_expires_at"
  ON "notification_outbox" ("lease_expires_at");

ALTER TABLE "notifications" ADD COLUMN IF NOT EXISTS "source" character varying(32) NOT NULL DEFAULT 'direct';
ALTER TABLE "notifications" ADD COLUMN IF NOT EXISTS "dedupe_key" character varying(255);
CREATE UNIQUE INDEX IF NOT EXISTS "uniq_notifications_source_dedupe"
  ON "notifications" ("source", "dedupe_key") WHERE "dedupe_key" IS NOT NULL;

CREATE TABLE IF NOT EXISTS "notification_deliveries" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "dedupe_key" character varying(255) NOT NULL,
  "channel" character varying(32) NOT NULL,
  "status" character varying(16) NOT NULL DEFAULT 'pending',
  "attempts" bigint NOT NULL DEFAULT 0,
  "last_error" text NOT NULL DEFAULT '',
  "delivered_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "uniq_notification_delivery_channel" UNIQUE ("dedupe_key", "channel")
);
