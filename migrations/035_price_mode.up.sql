-- 035_price_mode.up.sql
-- 定价模式：catalog=跟随目录价，manual=使用本表 input/output_price

ALTER TABLE models
    ADD COLUMN IF NOT EXISTS price_mode VARCHAR(10) NOT NULL DEFAULT 'catalog';

COMMENT ON COLUMN models.price_mode IS 'catalog: 读取时按 catalog_id 解析 LiteLLM 价; manual: 使用 input_price/output_price 作为实付价';