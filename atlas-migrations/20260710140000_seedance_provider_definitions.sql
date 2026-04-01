-- Seedance Provider Definition 注册
-- 注册 seedance 视频生成 Provider 定义

INSERT INTO provider_definitions (
    id, display_name, adapter, base_url,
    credential_schema, vendor_options, description, builtin, enabled,
    default_models, created_at, updated_at
) VALUES (
    'seedance-relay',
    'Seedance 中转',
    'seedance-relay',
    'http://115.190.78.58:8000',
    '{"api_key": {"type": "string", "label": "API Key", "secret": true, "required": true, "placeholder": "sk-..."}}'::jsonb,
    '{"skip_forward_check": true}'::jsonb,
    'Seedance 视频生成第三方中转，支持 Bearer Token 认证',
    true, true,
    '["doubao-seedance-2-0-260128", "doubao-seedance-2-0-fast-260128"]'::jsonb,
    NOW(), NOW()
) ON CONFLICT (id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    adapter = EXCLUDED.adapter,
    base_url = EXCLUDED.base_url,
    credential_schema = EXCLUDED.credential_schema,
    vendor_options = EXCLUDED.vendor_options,
    description = EXCLUDED.description,
    default_models = EXCLUDED.default_models,
    updated_at = NOW();

INSERT INTO provider_definitions (
    id, display_name, adapter, base_url,
    credential_schema, vendor_options, description, builtin, enabled,
    default_models, created_at, updated_at
) VALUES (
    'seedance-volcengine',
    'Seedance 官方',
    'seedance-volcengine',
    'https://ark.cn-beijing.volces.com',
    '{"access_key": {"type": "string", "label": "Access Key", "secret": true, "required": true, "placeholder": "AK..."}, "secret_key": {"type": "string", "label": "Secret Key", "secret": true, "required": true, "placeholder": "SK..."}}'::jsonb,
    '{"skip_forward_check": true}'::jsonb,
    '火山引擎 Seedance 视频生成官方 API，支持 AK/SK HMAC 签名认证',
    true, true,
    '["doubao-seedance-2-0-260128", "doubao-seedance-2-0-fast-260128"]'::jsonb,
    NOW(), NOW()
) ON CONFLICT (id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    adapter = EXCLUDED.adapter,
    base_url = EXCLUDED.base_url,
    credential_schema = EXCLUDED.credential_schema,
    vendor_options = EXCLUDED.vendor_options,
    description = EXCLUDED.description,
    default_models = EXCLUDED.default_models,
    updated_at = NOW();