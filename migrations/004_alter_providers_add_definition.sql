-- 004_alter_providers_add_definition.sql
-- 改造 providers 表：增加 definition_id 关联，credentials 改为 JSON 结构加密

-- 新增 definition_id 列
ALTER TABLE providers ADD COLUMN IF NOT EXISTS definition_id VARCHAR(50) REFERENCES provider_definitions(id);

-- 存量数据迁移：按旧 type 字段映射到 definition_id
UPDATE providers SET definition_id = type WHERE definition_id IS NULL;

-- credentials 字段已经是 TEXT 加密存储，内容从单字符串改为 JSON 格式
-- 存量迁移：把旧的 "sk-xxx" 包装为 {"api_key": "sk-xxx"} 后重新加密
-- 注意：此迁移需要应用层配合执行（加密操作无法在纯 SQL 中完成）
-- 标记：应用启动时检测旧格式并自动迁移

-- 设置 definition_id 为非空（存量迁移后）
-- ALTER TABLE providers ALTER COLUMN definition_id SET NOT NULL;
-- 暂不执行，等存量迁移完成后手动执行

-- 添加索引
CREATE INDEX IF NOT EXISTS idx_providers_definition_id ON providers (definition_id);

COMMENT ON COLUMN providers.definition_id IS '关联的渠道定义 ID，决定使用哪个 adapter 和凭证 schema';
