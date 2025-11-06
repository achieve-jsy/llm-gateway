-- 002_create_models.sql
-- 创建模型注册表

CREATE TABLE IF NOT EXISTS models (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(200) NOT NULL,
    provider_id UUID NOT NULL REFERENCES providers(id) ON DELETE RESTRICT ON UPDATE CASCADE,
    model_type VARCHAR(50) NOT NULL DEFAULT 'chat',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- 模型名称全局唯一
CREATE UNIQUE INDEX idx_models_name_unique ON models (name);

-- 按渠道查询模型
CREATE INDEX idx_models_provider_id ON models (provider_id);

-- 按启用状态过滤（用于 gateway-api 查询可用模型）
CREATE INDEX idx_models_enabled ON models (enabled);

-- 复合索引：按渠道+启用状态查询
CREATE INDEX idx_models_provider_enabled ON models (provider_id, enabled);

COMMENT ON TABLE models IS '模型注册表，记录各模型与渠道的归属关系';
COMMENT ON COLUMN models.name IS '模型名称，全局唯一，用于 gateway-api 路由匹配';
COMMENT ON COLUMN models.provider_id IS '归属渠道 ID，外键关联 providers 表';
COMMENT ON COLUMN models.model_type IS '模型类型：chat（对话补全）/ embedding（向量嵌入）';
COMMENT ON COLUMN models.enabled IS '是否启用，渠道禁用时自动不可用';
