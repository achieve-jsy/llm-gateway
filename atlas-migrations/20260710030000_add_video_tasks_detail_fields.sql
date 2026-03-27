-- Add detail fields for video task drawer prototype.
ALTER TABLE "video_tasks"
  ADD COLUMN IF NOT EXISTS "video_url" text NULL,
  ADD COLUMN IF NOT EXISTS "request_body" text NULL,
  ADD COLUMN IF NOT EXISTS "response_body" text NULL,
  ADD COLUMN IF NOT EXISTS "error_message" text NULL;

-- Store lightweight task lifecycle events for the detail timeline.
CREATE TABLE IF NOT EXISTS "video_task_events" (
  "id" serial NOT NULL,
  "task_id" character varying(128) NOT NULL,
  "event_type" character varying(64) NOT NULL,
  "event_data" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_video_task_events_task" FOREIGN KEY ("task_id") REFERENCES "video_tasks" ("task_id") ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS "idx_video_task_events_task_id" ON "video_task_events" ("task_id");
CREATE INDEX IF NOT EXISTS "idx_video_task_events_created_at" ON "video_task_events" ("created_at");
