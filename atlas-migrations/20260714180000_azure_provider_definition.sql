-- 注册 Azure OpenAI 渠道定义（幂等 upsert）
--
-- Azure 特殊性：
--   1. 认证用 api-key 头（不是 Authorization: Bearer）
--   2. URL 模式为 /openai/deployments/{model}/... （adapter 内部拼接）
--   3. 每个请求必须带 ?api-version= 查询参数，由 VendorOptions.api_version 提供默认值
--   4. streaming 不支持 stream_options（Azure 会拒绝），需 supports_stream_options=false
--   5. 某些 chunk 可能 choices 为空（Azure 会发送 usage-only chunk），需 empty_choices_skip=true

INSERT INTO provider_definitions (
    id, display_name, adapter, base_url,
    credential_schema, vendor_options, description, builtin, enabled,
    default_models, doc_url, created_at, updated_at
) VALUES (
    'azure',
    'Azure OpenAI',
    'azure',
    '',
    '{"api_key": {"type": "string", "label": "API Key", "secret": true, "required": true, "placeholder": "从 Azure Portal → Keys and Endpoint 获取"}}'::jsonb,
    '{"url_pattern": "azure", "auth_header": "api-key", "requires_api_version": true, "api_version": "2025-04-01-preview", "supports_stream_options": false, "empty_choices_skip": true}'::jsonb,
    'Microsoft Azure OpenAI Service。Endpoint 填写 Azure 资源基址（如 https://your-resource.openai.azure.com），adapter 自动拼接 /openai/deployments/{model}/... 路径和 ?api-version= 参数。',
    true, true,
    '["gpt-5.5", "gpt-4o", "gpt-4o-mini", "o3-mini", "o4-mini"]'::jsonb,
    'https://learn.microsoft.com/en-us/azure/ai-services/openai/',
    NOW(), NOW()
) ON CONFLICT (id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    adapter = EXCLUDED.adapter,
    base_url = EXCLUDED.base_url,
    credential_schema = EXCLUDED.credential_schema,
    vendor_options = EXCLUDED.vendor_options,
    description = EXCLUDED.description,
    default_models = EXCLUDED.default_models,
    doc_url = EXCLUDED.doc_url,
    builtin = EXCLUDED.builtin,
    enabled = EXCLUDED.enabled,
    updated_at = NOW();
