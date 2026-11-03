-- 033_add_pricing_fields.sql
-- 变更：CHG-20260603-001 — 模型定价字段
-- 描述：model_catalog 新增 reasoning/cached 价格列；models 新增价格模式与手动定价列

-- 1. model_catalog 新增定价列（LiteLLM 同步的推理/缓存价格）
ALTER TABLE model_catalog
    ADD COLUMN IF NOT EXISTS reasoning_price NUMERIC DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS cached_price    NUMERIC DEFAULT NULL;

-- 2. models 新增价格模式与手动定价列（支持 'catalog'/'manual' 模式切换）
ALTER TABLE models
    ADD COLUMN IF NOT EXISTS price_mode      VARCHAR(20) NOT NULL DEFAULT 'catalog',
    ADD COLUMN IF NOT EXISTS reasoning_price NUMERIC DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS cached_price    NUMERIC DEFAULT NULL;
