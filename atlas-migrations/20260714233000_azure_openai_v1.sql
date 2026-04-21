-- Azure OpenAI 切到 /openai/v1：资源根 + 无 dated api-version 硬依赖
-- 不再引导运营填写 /openai/deployments/... 或 /openai/responses?...

UPDATE provider_definitions
SET
    vendor_options = jsonb_build_object(
        'url_pattern', 'azure_v1',
        'auth_header', 'api-key',
        'requires_api_version', false,
        'supports_stream_options', false,
        'empty_choices_skip', true
    ),
    description = 'Microsoft Azure OpenAI（/openai/v1）。Endpoint 仅填资源根（如 https://your-resource.openai.azure.com）；adapter 自动使用 /openai/v1/chat/completions 与 /openai/v1/responses。模型 upstream_model_id 填部署名。不兼容旧 deployments + 日期 api-version 配法。',
    default_models = '["gpt-5.5", "gpt-4o", "gpt-4o-mini"]'::jsonb,
    updated_at = NOW()
WHERE id = 'azure';

-- 废弃旧 azure-openai 定义（adapter 未注册，禁止新建）
UPDATE provider_definitions
SET
    enabled = false,
    description = '已废弃：请改用 definition_id=azure（Azure OpenAI /openai/v1）。',
    updated_at = NOW()
WHERE id = 'azure-openai';
