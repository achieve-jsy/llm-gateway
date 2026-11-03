-- 通知内容模板化：outbox 增加 template_data JSON 列，承载出队后渲染模板所需的业务变量
-- （消费者传结构化变量，渲染发生在 worker 出队之后，变量必须随行持久化）。
ALTER TABLE "notification_outbox" ADD COLUMN IF NOT EXISTS "template_data" jsonb;
