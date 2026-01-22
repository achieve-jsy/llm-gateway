-- Create "notifications" table
CREATE TABLE "notifications" (
  "id"         uuid          NOT NULL DEFAULT gen_random_uuid(),
  "user_id"    uuid          NOT NULL,
  "type"       varchar(32)   NOT NULL,
  "level"      varchar(16)   NOT NULL DEFAULT 'info',
  "title"      varchar(200)  NOT NULL,
  "content"    text          NOT NULL,
  "ref_type"   varchar(32)   NOT NULL DEFAULT '',
  "ref_id"     varchar(64)   NOT NULL DEFAULT '',
  "is_read"    boolean       NOT NULL DEFAULT false,
  "read_at"    timestamptz   NULL,
  "created_at" timestamptz   NOT NULL,
  PRIMARY KEY ("id")
);
CREATE INDEX "idx_notifications_user_unread" ON "notifications" ("user_id", "is_read");
