-- 025_models_name_provider_unique.down.sql
-- 回滚：恢复 name 全局唯一索引

DROP INDEX IF EXISTS idx_models_name_provider_unique;

CREATE UNIQUE INDEX idx_models_name_unique ON models (name);
