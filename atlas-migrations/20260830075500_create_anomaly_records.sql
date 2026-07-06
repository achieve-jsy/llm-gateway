-- 方案X 异常反馈闭环：新建 anomaly_records 表。
-- 异常被报告识别后落库为可长期存续记录，人工可反馈 5 态状态（待确认/已确认正常/修复中/已恢复/误报），
-- 下次报告读回真实状态。唯一 upsert 键 uniq_anomaly_record(scope_level, scope_id, rule, window_start, member_id)
-- 保证同一异常窗口同一成员只一条。window_start 为 Asia/Shanghai 命中窗口起点，分钟精度。
CREATE TABLE "anomaly_records" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "scope_level" character varying(16) NOT NULL,
  "scope_id" character varying(255) NOT NULL,
  "rule" character varying(8) NOT NULL,
  "rule_version" character varying(8) NOT NULL DEFAULT 'v1',
  "window_start" timestamptz NOT NULL,
  "member_id" character varying(255) NOT NULL DEFAULT '',
  "status" character varying(32) NOT NULL DEFAULT '待确认',
  "feedback_at" timestamptz NULL,
  "feedback_by" character varying(255) NULL,
  "reason" character varying(512) NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_anomaly_scope_window" to table: "anomaly_records"
CREATE INDEX "idx_anomaly_scope_window" ON "anomaly_records" ("scope_id", "rule", "window_start");
-- Create index "idx_anomaly_status" to table: "anomaly_records"
CREATE INDEX "idx_anomaly_status" ON "anomaly_records" ("status");
-- Create index "uniq_anomaly_record" to table: "anomaly_records"
CREATE UNIQUE INDEX "uniq_anomaly_record" ON "anomaly_records" ("scope_level", "scope_id", "rule", "window_start", "member_id");

-- Down migration: 回滚删除表
-- DROP TABLE IF EXISTS "anomaly_records";
