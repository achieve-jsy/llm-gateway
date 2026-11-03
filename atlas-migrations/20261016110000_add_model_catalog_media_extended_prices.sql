-- A1 计价链路打通（补充维度）：model_catalog 新增「图像 token / 像素 / 视频 token / 字符」价列。
--
-- 为什么需要新增（既有 ModelPricing 字段族无法表达，单位不同）：
--   input_cost_per_image_token / output_cost_per_image_token  —— gpt-image 系列按图像 token 计费
--   input_cost_per_pixel / output_cost_per_pixel              —— dall-e-2 等按像素计费
--   input_cost_per_video_token / output_cost_per_video_token  —— 视频按 token 计费的上游
--   input_cost_per_character                                  —— 语音合成按字符计费（复用既有 PerCharacterInput）
--
-- 全部可空且无默认值：NULL = 无价；单价为美元原值（不做 token 缩放）。
--
-- 消费状态（A1 交付口径）：
--   input/output_per_image_token → 已参与成本计算（gateway obs EstimateMediaCost）
--   per_character_input / per_pixel / per_video_token → 只解析入库，暂不参与成本计算（留待 B/C/D 队接入）
ALTER TABLE "model_catalog"
  ADD COLUMN "input_per_image_token" numeric(18,8) NULL,
  ADD COLUMN "output_per_image_token" numeric(18,8) NULL,
  ADD COLUMN "input_per_pixel" numeric(18,8) NULL,
  ADD COLUMN "output_per_pixel" numeric(18,8) NULL,
  ADD COLUMN "input_per_video_token" numeric(18,8) NULL,
  ADD COLUMN "output_per_video_token" numeric(18,8) NULL,
  ADD COLUMN "per_character_input" numeric(18,8) NULL;
