-- 管理端通知历史页:notification_deliveries 冗余类型/标题/内容/接收人,
-- 使 email/webhook 投递记录可在历史页独立展示(webhook-only 系统级通知在 notifications 无对应行)。
ALTER TABLE "notification_deliveries" ADD COLUMN IF NOT EXISTS "notification_type" character varying(32) NOT NULL DEFAULT '';
ALTER TABLE "notification_deliveries" ADD COLUMN IF NOT EXISTS "title" character varying(200) NOT NULL DEFAULT '';
ALTER TABLE "notification_deliveries" ADD COLUMN IF NOT EXISTS "content" text NOT NULL DEFAULT '';
ALTER TABLE "notification_deliveries" ADD COLUMN IF NOT EXISTS "user_id" uuid;
