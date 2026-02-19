-- call_logs 冗余 key_type（personal | service），写入时快照，供预算重建与用量筛选
ALTER TABLE "call_logs" ADD COLUMN IF NOT EXISTS "key_type" character varying(16) NOT NULL DEFAULT 'personal';

CREATE INDEX IF NOT EXISTS "idx_call_logs_key_type" ON "call_logs" ("key_type");

-- 历史数据从 api_keys 回填
UPDATE "call_logs" cl
SET "key_type" = COALESCE(NULLIF(ak."key_type", ''), 'personal')
FROM "api_keys" ak
WHERE ak."id"::text = cl."key_id"
  AND cl."key_id" <> '';
