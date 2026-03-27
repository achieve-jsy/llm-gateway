-- model_groups.model_type：组类型（chat/embedding/embedding_vl/rerank/video）
-- 与 models.model_type 对齐；历史数据默认 chat

ALTER TABLE "model_groups"
  ADD COLUMN IF NOT EXISTS "model_type" character varying(50) NOT NULL DEFAULT 'chat';

-- 约束：仅允许已知类型（IF NOT EXISTS 兼容重复 apply）
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'chk_model_groups_model_type'
  ) THEN
    ALTER TABLE "model_groups"
      ADD CONSTRAINT "chk_model_groups_model_type"
      CHECK ((model_type)::text = ANY ((ARRAY[
        'chat'::character varying,
        'embedding'::character varying,
        'embedding_vl'::character varying,
        'rerank'::character varying,
        'video'::character varying
      ])::text[]));
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS "idx_model_groups_model_type" ON "model_groups" ("model_type");
