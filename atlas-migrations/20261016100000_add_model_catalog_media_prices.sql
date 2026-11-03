-- A1 计价链路打通：model_catalog 新增媒体价列。
--
-- 与 core.ModelPricing 既有的非 token 字段族一一对应（不新造字段族）：
--   per_image        ← output_cost_per_image
--   input_per_image  ← input_cost_per_image
--   per_second_input ← input_cost_per_video_per_second / input_cost_per_second
--   per_second_output← output_cost_per_video_per_second / output_cost_per_second
--   per_request      ← input_cost_per_request
--
-- 单位为「每张 / 每秒 / 每次」的美元原值（不乘 1e6，不做 token 折算）。
-- 全部可空且无默认值：NULL = 无价，与 token 价列的 0 占位区分开，
-- 供计价层判定「字段存在但无价 → usage_missing」，避免静默记 0 元。
ALTER TABLE "model_catalog"
  ADD COLUMN "per_image" numeric(18,8) NULL,
  ADD COLUMN "input_per_image" numeric(18,8) NULL,
  ADD COLUMN "per_second_input" numeric(18,8) NULL,
  ADD COLUMN "per_second_output" numeric(18,8) NULL,
  ADD COLUMN "per_request" numeric(18,8) NULL;
