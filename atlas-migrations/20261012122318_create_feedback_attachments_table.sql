-- Create "feedback_attachments" table
CREATE TABLE "feedback_attachments" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "file_name" character varying(255) NOT NULL DEFAULT '',
  "mime" character varying(64) NOT NULL,
  "data" bytea NOT NULL,
  "size_bytes" bigint NOT NULL,
  "width" bigint NOT NULL DEFAULT 0,
  "height" bigint NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "feedback_id" uuid NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_feedback_attachments_feedback" FOREIGN KEY ("feedback_id") REFERENCES "feedbacks" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "idx_feedback_attachments_created_at" to table: "feedback_attachments"
CREATE INDEX "idx_feedback_attachments_created_at" ON "feedback_attachments" ("created_at");
-- Create index "idx_feedback_attachments_feedback_id" to table: "feedback_attachments"
CREATE INDEX "idx_feedback_attachments_feedback_id" ON "feedback_attachments" ("feedback_id");
-- Create index "idx_feedback_attachments_user_id" to table: "feedback_attachments"
CREATE INDEX "idx_feedback_attachments_user_id" ON "feedback_attachments" ("user_id");
