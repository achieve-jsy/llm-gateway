-- 补齐配额紧急豁免表（原 migrations/058，此前未纳入 Atlas）
-- 幂等：测试环境可能已手工建表，生产/其他环境按迁移创建。
CREATE TABLE IF NOT EXISTS "quota_exemptions" (
  "id"           uuid           NOT NULL DEFAULT gen_random_uuid(),
  "subject_type" character varying(32) NOT NULL,
  "subject_id"   character varying(255) NOT NULL,
  "reason"       text           NULL,
  "expires_at"   timestamptz    NOT NULL,
  "created_by"   character varying(255) NOT NULL,
  "created_at"   timestamptz    NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_quota_exemptions_active"
  ON "quota_exemptions" ("subject_type", "subject_id", "expires_at");
