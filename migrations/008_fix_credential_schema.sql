-- 008_fix_credential_schema.sql
-- 将 AWS Bedrock 的 region 和 Azure OpenAI 的 api_version 从 credential_schema 移出
-- region 改为通过 Endpoint 字段体现，api_version 已存在于 vendor_options 中

-- AWS Bedrock: 移除 region，只保留 access_key_id + secret_access_key
UPDATE provider_definitions
SET credential_schema = '{
  "access_key_id": {"type": "string", "required": true, "secret": false, "label": "Access Key ID", "placeholder": "AKIA..."},
  "secret_access_key": {"type": "string", "required": true, "secret": true, "label": "Secret Access Key", "placeholder": "wJalrXUt..."}
}',
base_url = 'https://bedrock-runtime.us-east-1.amazonaws.com'
WHERE id = 'bedrock';

-- Azure OpenAI: 移除 api_version，只保留 api_key
-- api_version 已存在于 vendor_options.requires_api_version，实际值由高级设置承载
UPDATE provider_definitions
SET credential_schema = '{
  "api_key": {"type": "string", "required": true, "secret": true, "label": "API Key", "placeholder": "your-azure-api-key"}
}'
WHERE id = 'azure-openai';

-- 给 Azure OpenAI 的 vendor_options 补充 default_api_version 字段
UPDATE provider_definitions
SET vendor_options = jsonb_set(
  vendor_options,
  '{default_api_version}',
  '"2024-02-01"'
)
WHERE id = 'azure-openai';

-- 给 Bedrock 的 vendor_options 补充 default_region 字段
UPDATE provider_definitions
SET vendor_options = jsonb_set(
  COALESCE(vendor_options, '{}'),
  '{default_region}',
  '"us-east-1"'
)
WHERE id = 'bedrock';
