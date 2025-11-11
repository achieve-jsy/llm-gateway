-- 001_create_providers.sql
-- 创建渠道（厂商通道）配置表

CREATE TABLE IF NOT EXISTS providers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    type VARCHAR(50) NOT NULL,
    endpoint VARCHAR(500) NOT NULL,
    encrypted_credentials TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- 渠道名称全局唯一
CREATE UNIQUE INDEX idx_providers_name_unique ON providers (name);

-- 按类型查询
CREATE INDEX idx_providers_type ON providers (type);

-- 按启用状态过滤
CREATE INDEX idx_providers_enabled ON providers (enabled);

COMMENT ON TABLE providers IS '厂商渠道配置表，存储各模型厂商的连接信息';
COMMENT ON COLUMN providers.name IS '渠道名称，全局唯一';
COMMENT ON COLUMN providers.type IS '渠道类型：dashscope/deepseek/bedrock/litellm';
COMMENT ON COLUMN providers.endpoint IS '厂商服务接入地址';
COMMENT ON COLUMN providers.encrypted_credentials IS 'AES-256-GCM 加密后的凭证信息（Base64 编码）';
COMMENT ON COLUMN providers.enabled IS '是否启用，禁用时其下所有模型不可用';
