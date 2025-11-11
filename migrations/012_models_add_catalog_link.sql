-- 012_models_add_catalog_link.sql
-- 给 models 表挂上 catalog 关联与 override 锁定标记。
-- D7：能力位走 live JOIN，不复制到 models；
-- D10：context_length / max_output_tokens 自动同步，但允许 admin 手工覆盖（被设置 *_overridden=true 后再不自动同步）；
-- D10：input_price / output_price / upstream_model_id 永远不自动同步，由 admin 通过 "apply diff" 触发。

ALTER TABLE models ADD COLUMN IF NOT EXISTS catalog_id            UUID NULL REFERENCES model_catalog(id);
ALTER TABLE models ADD COLUMN IF NOT EXISTS ctx_overridden        BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE models ADD COLUMN IF NOT EXISTS max_output_overridden BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_models_catalog_id ON models(catalog_id);
