-- 003_create_provider_definitions.sql
-- 创建渠道定义表（Provider Registry）

CREATE TABLE IF NOT EXISTS provider_definitions (
    id VARCHAR(50) PRIMARY KEY,
    display_name VARCHAR(100) NOT NULL,
    adapter VARCHAR(50) NOT NULL,
    base_url VARCHAR(500) NOT NULL DEFAULT '',
    credential_schema JSONB NOT NULL DEFAULT '{}',
    description VARCHAR(500) NOT NULL DEFAULT '',
    builtin BOOLEAN NOT NULL DEFAULT false,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- 内置默认渠道定义
INSERT INTO provider_definitions (id, display_name, adapter, base_url, credential_schema, description, builtin) VALUES
('dashscope', '阿里百炼', 'openai-compatible', 'https://dashscope.aliyuncs.com/compatible-mode/v1', '{"api_key": {"type": "string", "required": true, "secret": true, "label": "API Key", "placeholder": "sk-..."}}', '阿里云百炼大模型服务（DashScope），支持 Qwen 系列', true),
('deepseek', 'DeepSeek', 'openai-compatible', 'https://api.deepseek.com/v1', '{"api_key": {"type": "string", "required": true, "secret": true, "label": "API Key", "placeholder": "sk-..."}}', 'DeepSeek 大模型服务，支持 deepseek-chat/deepseek-reasoner', true),
('bedrock', 'AWS Bedrock', 'bedrock', 'https://bedrock-runtime.us-east-1.amazonaws.com', '{"access_key_id": {"type": "string", "required": true, "secret": false, "label": "Access Key ID", "placeholder": "AKIA..."}, "secret_access_key": {"type": "string", "required": true, "secret": true, "label": "Secret Access Key", "placeholder": "wJalrXUt..."}}', 'AWS Bedrock 托管模型服务（Claude/Llama 等）', true),
('azure-openai', 'Azure OpenAI', 'openai-compatible', '', '{"api_key": {"type": "string", "required": true, "secret": true, "label": "API Key", "placeholder": "your-azure-api-key"}}', 'Azure OpenAI 服务', true),
('openai-compatible', '通用 OpenAI 兼容', 'openai-compatible', '', '{"api_key": {"type": "string", "required": true, "secret": true, "label": "API Key", "placeholder": "sk-..."}}', '通用 OpenAI 兼容协议接入（适用于智谱、月之暗面、百川、豆包、零一万物等）', true),
('anthropic', 'Anthropic (Claude)', 'anthropic', 'https://api.anthropic.com/v1', '{"api_key": {"type": "string", "required": true, "secret": true, "label": "API Key", "placeholder": "sk-ant-..."}}', 'Anthropic Claude 原生 API（非 OpenAI 兼容协议，需独立适配）', true)
ON CONFLICT (id) DO NOTHING;

COMMENT ON TABLE provider_definitions IS '渠道定义注册表（Provider Registry），定义支持的厂商类型及其凭证 schema';
COMMENT ON COLUMN provider_definitions.id IS '唯一标识符，用作 providers.definition_id 外键';
COMMENT ON COLUMN provider_definitions.adapter IS '调用适配器类型：openai-compatible / bedrock / azure 等';
COMMENT ON COLUMN provider_definitions.base_url IS '默认接入地址，创建渠道时可覆盖';
COMMENT ON COLUMN provider_definitions.credential_schema IS 'JSON Schema，描述凭证字段结构，前端据此动态渲染表单';
COMMENT ON COLUMN provider_definitions.builtin IS '是否为系统内置（内置定义不可删除）';
