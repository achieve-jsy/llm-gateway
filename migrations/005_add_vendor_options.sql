-- 005_add_vendor_options.sql
-- 给渠道定义表增加 vendor_options 字段，承载各厂商的协议差异化配置

ALTER TABLE provider_definitions ADD COLUMN IF NOT EXISTS vendor_options JSONB NOT NULL DEFAULT '{}';

-- 更新内置定义的 vendor_options
UPDATE provider_definitions SET vendor_options = '{
  "url_pattern": "standard",
  "auth_header": "bearer",
  "stream_usage_in_last_chunk": true,
  "empty_choices_skip": true
}' WHERE id = 'dashscope';

UPDATE provider_definitions SET vendor_options = '{
  "url_pattern": "standard",
  "auth_header": "bearer",
  "supports_reasoning_content": true
}' WHERE id = 'deepseek';

UPDATE provider_definitions SET vendor_options = '{
  "url_pattern": "azure",
  "auth_header": "api-key",
  "requires_api_version": true,
  "empty_choices_skip": true,
  "supports_stream_options": true
}' WHERE id = 'azure-openai';

UPDATE provider_definitions SET vendor_options = '{
  "url_pattern": "standard",
  "auth_header": "bearer"
}' WHERE id = 'openai-compatible';

UPDATE provider_definitions SET vendor_options = '{}' WHERE id = 'anthropic';
UPDATE provider_definitions SET vendor_options = '{}' WHERE id = 'bedrock';

COMMENT ON COLUMN provider_definitions.vendor_options IS '厂商协议差异化配置（URL构造/Header/响应解析等开关），通用适配器据此驱动行为分支';
