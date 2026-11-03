-- report_snapshot + anomaly_review + offhours_filing 建表（DB-3）
-- 参考 docs/token-usage-analysis-report-implementation.md §10、docs/token-usage-anomaly-work-unrelated-design.md §5

-- 报告存档：周期性 job 写，页面/MD/HTML 都读它保证一致（§10.3）。
-- 唯一索引 (level, scope_id, period_start) 保证同一 level+scope+周期只有一份存档（幂等 upsert）。
CREATE TABLE "report_snapshot" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "level" character varying(16) NOT NULL,
  "scope_id" text NOT NULL,
  "period_type" character varying(8) NOT NULL,
  "period_start" date NOT NULL,
  "period_end" date NOT NULL,
  "report_json" jsonb NOT NULL DEFAULT '{}',
  "schema_version" character varying(16) NOT NULL DEFAULT '2.0',
  "generated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "chk_report_snapshot_level" CHECK ((level)::text = ANY ((ARRAY['personal'::character varying, 'department'::character varying, 'studio'::character varying])::text[])),
  CONSTRAINT "chk_report_snapshot_period_type" CHECK ((period_type)::text = ANY ((ARRAY['day'::character varying, 'week'::character varying, 'month'::character varying])::text[]))
);
CREATE UNIQUE INDEX "uniq_report_snapshot" ON "report_snapshot" ("level", "scope_id", "period_start");

-- 严重异常（非工作时段无关消耗）待核查记录：状态机 pending_review → confirmed_ok/need_check/false_positive（§5）。
CREATE TABLE "anomaly_review" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" text NOT NULL,
  "dept_id" text NOT NULL DEFAULT '',
  "period_start" date NOT NULL,
  "period_end" date NOT NULL,
  "unrelated_cost_usd" numeric(18,6) NOT NULL DEFAULT 0,
  "unrelated_ratio" numeric(9,6) NOT NULL DEFAULT 0,
  "top_hours" text NOT NULL DEFAULT '',
  "top_model" text NOT NULL DEFAULT '',
  "top_key" text NOT NULL DEFAULT '',
  "status" character varying(16) NOT NULL DEFAULT 'pending_review',
  "reviewer" text NOT NULL DEFAULT '',
  "reviewed_at" timestamptz NULL,
  "appeal_note" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "chk_anomaly_review_status" CHECK ((status)::text = ANY ((ARRAY['pending_review'::character varying, 'confirmed_ok'::character varying, 'need_check'::character varying, 'false_positive'::character varying])::text[]))
);
CREATE INDEX "idx_anomaly_review_dept" ON "anomaly_review" ("dept_id");
CREATE INDEX "idx_anomaly_review_period" ON "anomaly_review" ("period_start");
CREATE INDEX "idx_anomaly_review_status" ON "anomaly_review" ("status");
CREATE INDEX "idx_anomaly_review_user" ON "anomaly_review" ("user_id");

-- 非工作时段消耗备案表（批处理/跨时区/发布值班），命中后从 unrelated_cost 中排除，避免误伤（§2.2）。
CREATE TABLE "offhours_filing" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "subject_type" character varying(8) NOT NULL,
  "subject_id" text NOT NULL,
  "start_at" timestamptz NOT NULL,
  "end_at" timestamptz NOT NULL,
  "filing_type" character varying(16) NOT NULL,
  "note" text NOT NULL DEFAULT '',
  "created_by" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "chk_offhours_filing_subject_type" CHECK ((subject_type)::text = ANY ((ARRAY['user'::character varying, 'dept'::character varying, 'key'::character varying])::text[]))
);
CREATE INDEX "idx_offhours_filing_subject" ON "offhours_filing" ("subject_type", "subject_id");
CREATE INDEX "idx_offhours_filing_window" ON "offhours_filing" ("start_at");
