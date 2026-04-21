-- Create "video_tasks" table（Seedance 视频任务用量记录）
-- 幂等：表/索引已存在时跳过
CREATE TABLE IF NOT EXISTS "video_tasks" (
  "task_id" character varying(128) NOT NULL,
  "api_key_id" character varying(64) NOT NULL,
  "api_key_name" character varying(128) NULL,
  "api_key_type" character varying(16) NULL,
  "user_id" text NULL,
  "user_name" character varying(128) NULL,
  "model" text NOT NULL,
  "provider_name" character varying(64) NOT NULL,
  "provider_type" character varying(64) NOT NULL DEFAULT '',
  "status" character varying(32) NOT NULL DEFAULT 'submitted',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("task_id")
);
CREATE INDEX IF NOT EXISTS "idx_video_tasks_api_key" ON "video_tasks" ("api_key_id");
CREATE INDEX IF NOT EXISTS "idx_video_tasks_model" ON "video_tasks" ("model");
CREATE INDEX IF NOT EXISTS "idx_video_tasks_status" ON "video_tasks" ("status");
CREATE INDEX IF NOT EXISTS "idx_video_tasks_ts" ON "video_tasks" ("created_at");
