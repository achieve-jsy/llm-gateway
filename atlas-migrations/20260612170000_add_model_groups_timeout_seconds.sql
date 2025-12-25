-- Add timeout_seconds to model_groups
-- 0 = 使用系统默认值（非流式 600s，流式 600s），正整数 = 每轮上游超时秒数
ALTER TABLE "model_groups" ADD COLUMN "timeout_seconds" integer NOT NULL DEFAULT 0;
