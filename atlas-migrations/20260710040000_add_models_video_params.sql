-- 模型表增加视频参数字段（Seedance / Video 模型注册）
ALTER TABLE "models" ADD COLUMN IF NOT EXISTS "video_supported_resolutions" jsonb NOT NULL DEFAULT '[]';
ALTER TABLE "models" ADD COLUMN IF NOT EXISTS "video_supported_aspect_ratios" jsonb NOT NULL DEFAULT '[]';
ALTER TABLE "models" ADD COLUMN IF NOT EXISTS "video_min_duration_seconds" bigint NOT NULL DEFAULT 0;
ALTER TABLE "models" ADD COLUMN IF NOT EXISTS "video_max_duration_seconds" bigint NOT NULL DEFAULT 0;
ALTER TABLE "models" ADD COLUMN IF NOT EXISTS "video_supported_actions" jsonb NOT NULL DEFAULT '[]';
