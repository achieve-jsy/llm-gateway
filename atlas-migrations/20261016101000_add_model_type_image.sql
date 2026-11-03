-- 扩展 model_groups.model_type 的 CHECK 约束：新增合法值 image（图像生成/编辑，如 gpt-image）。
--
-- 背景：原白名单为 chat/embedding/embedding_vl/rerank/rerank_vl/video（见
-- atlas-migrations/20260710180000_add_model_groups_model_type.sql，该文件已提交，按 D1 铁律不可修改）。
-- 三渠道接入需要 model_type=image，因此用本迁移重建约束。
--
-- 范围核对：models.model_type 与 model_groups.model_type 列同名，但 models 表
-- **没有** 任何 CHECK 约束（基线 atlas-migrations/20260609082623_baseline.sql:391 仅定义列
-- character varying(50) NOT NULL DEFAULT 'chat'），故本迁移只处理 model_groups。
--
-- 幂等性：DROP CONSTRAINT IF EXISTS + ADD CONSTRAINT，可安全重复执行（含首次 apply 时约束已被
-- 20260710180000 建出的正常路径）。
-- 回滚方式：先确认库内无 model_type='image' 的行，再 DROP CONSTRAINT 并用旧值集合（去掉 image）重建。

ALTER TABLE "model_groups" DROP CONSTRAINT IF EXISTS "chk_model_groups_model_type";

ALTER TABLE "model_groups"
  ADD CONSTRAINT "chk_model_groups_model_type"
  CHECK ((model_type)::text = ANY ((ARRAY[
    'chat'::character varying,
    'image'::character varying,
    'embedding'::character varying,
    'embedding_vl'::character varying,
    'rerank'::character varying,
    'rerank_vl'::character varying,
    'video'::character varying
  ])::text[]));
