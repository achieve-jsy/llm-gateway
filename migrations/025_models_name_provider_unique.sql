-- 025_models_name_provider_unique.sql
-- 将 models.name 全局唯一索引改为 (name, provider_id) 联合唯一
-- 允许不同渠道注册同名模型（如多个渠道都提供 deepseek-chat）

DROP INDEX IF EXISTS idx_models_name_unique;

CREATE UNIQUE INDEX idx_models_name_provider_unique ON models (name, provider_id);
