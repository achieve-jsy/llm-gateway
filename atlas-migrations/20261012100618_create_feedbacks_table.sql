-- Create "feedbacks" table
CREATE TABLE "feedbacks" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "user_name" character varying(128) NOT NULL DEFAULT '',
  "user_email" character varying(255) NOT NULL DEFAULT '',
  "type" character varying(32) NOT NULL DEFAULT 'suggestion',
  "content" text NOT NULL,
  "contact" character varying(128) NOT NULL DEFAULT '',
  "status" character varying(32) NOT NULL DEFAULT 'pending',
  "handled_at" timestamptz NULL,
  "handled_by" uuid NULL,
  "handle_note" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "idx_feedbacks_created_at" to table: "feedbacks"
CREATE INDEX "idx_feedbacks_created_at" ON "feedbacks" ("created_at");
-- Create index "idx_feedbacks_status" to table: "feedbacks"
CREATE INDEX "idx_feedbacks_status" ON "feedbacks" ("status");
-- Create index "idx_feedbacks_user_id" to table: "feedbacks"
CREATE INDEX "idx_feedbacks_user_id" ON "feedbacks" ("user_id");
