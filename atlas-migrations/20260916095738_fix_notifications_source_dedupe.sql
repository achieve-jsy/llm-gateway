-- Drop index "uniq_notifications_source_dedupe" from table: "notifications"
DROP INDEX "uniq_notifications_source_dedupe";
-- Create index "uniq_notifications_source_dedupe" to table: "notifications"
CREATE UNIQUE INDEX "uniq_notifications_source_dedupe" ON "notifications" ("user_id", "source", "dedupe_key");
