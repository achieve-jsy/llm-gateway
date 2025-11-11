-- 006_enhance_all_tables.sql
-- 全量补齐 providers / models / provider_definitions 字段

-- ==================== providers 表补充 ====================

ALTER TABLE providers ADD COLUMN IF NOT EXISTS weight INT NOT NULL DEFAULT 1;
ALTER TABLE providers ADD COLUMN IF NOT EXISTS priority INT NOT NULL DEFAULT 0;
ALTER TABLE providers ADD COLUMN IF NOT EXISTS status INT NOT NULL DEFAULT 1;
ALTER TABLE providers ADD COLUMN IF NOT EXISTS test_model VARCHAR(200) DEFAULT '';
ALTER TABLE providers ADD COLUMN IF NOT EXISTS response_time INT DEFAULT 0;
ALTER TABLE providers ADD COLUMN IF NOT EXISTS balance DECIMAL(12,4) DEFAULT 0;
ALTER TABLE providers ADD COLUMN IF NOT EXISTS remark VARCHAR(500) DEFAULT '';
ALTER TABLE providers ADD COLUMN IF NOT EXISTS max_retries INT NOT NULL DEFAULT 3;
ALTER TABLE providers ADD COLUMN IF NOT EXISTS timeout INT NOT NULL DEFAULT 30;
ALTER TABLE providers ADD COLUMN IF NOT EXISTS tag VARCHAR(100) DEFAULT '';

-- status: 1=启用 2=手动禁用 3=自动禁用
-- 将原 enabled 字段的值迁移到 status：enabled=true → status=1, enabled=false → status=2
UPDATE providers SET status = CASE WHEN enabled = true THEN 1 ELSE 2 END WHERE status = 1 AND enabled = false;

CREATE INDEX IF NOT EXISTS idx_providers_tag ON providers (tag);
CREATE INDEX IF NOT EXISTS idx_providers_status ON providers (status);
CREATE INDEX IF NOT EXISTS idx_providers_priority ON providers (priority DESC);

COMMENT ON COLUMN providers.weight IS '负载均衡权重（同模型多渠道时按权重分流）';
COMMENT ON COLUMN providers.priority IS '路由优先级（数值越大越优先）';
COMMENT ON COLUMN providers.status IS '状态：1=启用 2=手动禁用 3=自动禁用（连续失败）';
COMMENT ON COLUMN providers.test_model IS '测试用模型名（一键测试连通性时使用）';
COMMENT ON COLUMN providers.response_time IS '最近一次测试的响应时间（毫秒）';
COMMENT ON COLUMN providers.balance IS '渠道余额（USD），部分厂商可查询';
COMMENT ON COLUMN providers.remark IS '管理员备注说明';
COMMENT ON COLUMN providers.max_retries IS '最大重试次数';
COMMENT ON COLUMN providers.timeout IS '请求超时时间（秒）';
COMMENT ON COLUMN providers.tag IS '标签（用于分组筛选）';

-- ==================== models 表补充 ====================

ALTER TABLE models ADD COLUMN IF NOT EXISTS upstream_model_id VARCHAR(200) DEFAULT '';
ALTER TABLE models ADD COLUMN IF NOT EXISTS description VARCHAR(500) DEFAULT '';
ALTER TABLE models ADD COLUMN IF NOT EXISTS context_length INT DEFAULT 0;
ALTER TABLE models ADD COLUMN IF NOT EXISTS max_output_tokens INT DEFAULT 0;
ALTER TABLE models ADD COLUMN IF NOT EXISTS input_price DECIMAL(10,6) DEFAULT 0;
ALTER TABLE models ADD COLUMN IF NOT EXISTS output_price DECIMAL(10,6) DEFAULT 0;
ALTER TABLE models ADD COLUMN IF NOT EXISTS capabilities JSONB NOT NULL DEFAULT '[]';
ALTER TABLE models ADD COLUMN IF NOT EXISTS weight INT NOT NULL DEFAULT 1;

COMMENT ON COLUMN models.upstream_model_id IS '上游厂商实际接受的模型 ID（为空时使用 name 原样转发）';
COMMENT ON COLUMN models.description IS '模型描述（前端展示给用户）';
COMMENT ON COLUMN models.context_length IS '上下文窗口大小（token 数）';
COMMENT ON COLUMN models.max_output_tokens IS '最大输出 token 数';
COMMENT ON COLUMN models.input_price IS '输入价格（每百万 token，USD）';
COMMENT ON COLUMN models.output_price IS '输出价格（每百万 token，USD）';
COMMENT ON COLUMN models.capabilities IS '能力标签 JSON 数组：["chat","vision","function_call","reasoning","embedding"]';
COMMENT ON COLUMN models.weight IS '负载均衡权重（同名模型多渠道时）';

-- ==================== provider_definitions 表补充 ====================

ALTER TABLE provider_definitions ADD COLUMN IF NOT EXISTS icon_url VARCHAR(500) DEFAULT '';
ALTER TABLE provider_definitions ADD COLUMN IF NOT EXISTS doc_url VARCHAR(500) DEFAULT '';
ALTER TABLE provider_definitions ADD COLUMN IF NOT EXISTS default_models JSONB NOT NULL DEFAULT '[]';

-- 填充内置定义的 default_models
UPDATE provider_definitions SET default_models = '["qwen-max","qwen-plus","qwen-turbo","qwen-long","qwen-vl-max","text-embedding-v3"]' WHERE id = 'dashscope';
UPDATE provider_definitions SET default_models = '["deepseek-chat","deepseek-reasoner"]' WHERE id = 'deepseek';
UPDATE provider_definitions SET default_models = '["gpt-4o","gpt-4o-mini","gpt-4-turbo","o1","o3-mini","text-embedding-3-small","text-embedding-3-large"]' WHERE id = 'openai-compatible';
UPDATE provider_definitions SET default_models = '["gpt-4o","gpt-4","gpt-35-turbo","text-embedding-ada-002"]' WHERE id = 'azure-openai';
UPDATE provider_definitions SET default_models = '["claude-sonnet-4-20250514","claude-3-5-haiku-20241022","claude-3-opus-20240229"]' WHERE id = 'anthropic';
UPDATE provider_definitions SET default_models = '["anthropic.claude-sonnet-4-20250514-v1:0","anthropic.claude-3-5-haiku-20241022-v1:0","meta.llama3-70b-instruct-v1:0"]' WHERE id = 'bedrock';

-- 填充文档链接
UPDATE provider_definitions SET doc_url = 'https://help.aliyun.com/zh/model-studio/getting-started/' WHERE id = 'dashscope';
UPDATE provider_definitions SET doc_url = 'https://platform.deepseek.com/api-docs/' WHERE id = 'deepseek';
UPDATE provider_definitions SET doc_url = 'https://learn.microsoft.com/en-us/azure/ai-services/openai/' WHERE id = 'azure-openai';
UPDATE provider_definitions SET doc_url = 'https://docs.anthropic.com/en/api/getting-started' WHERE id = 'anthropic';
UPDATE provider_definitions SET doc_url = 'https://docs.aws.amazon.com/bedrock/latest/userguide/' WHERE id = 'bedrock';

COMMENT ON COLUMN provider_definitions.icon_url IS '厂商图标 URL（前端展示）';
COMMENT ON COLUMN provider_definitions.doc_url IS '厂商文档链接（帮助管理员对接）';
COMMENT ON COLUMN provider_definitions.default_models IS '默认模型列表 JSON 数组（创建渠道时预填）';
