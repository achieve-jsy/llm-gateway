-- Seedance Provider Definition 注册
-- 用于测试环境注册 Seedance 视频生成 Provider

-- 注册 seedance-relay (第三方中转)
INSERT INTO provider_definitions (
    id, display_name, adapter, base_url,
    credential_schema, description, builtin, enabled,
    vendor_options, default_models,
    created_at, updated_at
) VALUES (
    'seedance-relay',
    'Seedance 中转',
    'seedance-relay',
    'http://115.190.78.58:8000',
    '{"api_key": {"type": "string", "required": true, "description": "API Key"}}'::jsonb,
    'Seedance 视频生成第三方中转，支持 Bearer Token 认证',
    true, true,
    '{"auth_type": "bearer", "supported_features": ["video_generation", "asset_management", "liveness"]}'::jsonb,
    '[{"id": "doubao-seedance-2-0-260128", "name": "Seedance 2.0"}, {"id": "doubao-seedance-2-0-fast-260128", "name": "Seedance 2.0 Fast"}]'::jsonb,
    NOW(), NOW()
) ON CONFLICT (id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    adapter = EXCLUDED.adapter,
    base_url = EXCLUDED.base_url,
    credential_schema = EXCLUDED.credential_schema,
    description = EXCLUDED.description,
    vendor_options = EXCLUDED.vendor_options,
    default_models = EXCLUDED.default_models,
    updated_at = NOW();

-- 注册 seedance-volcengine (火山引擎官方)
INSERT INTO provider_definitions (
    id, display_name, adapter, base_url,
    credential_schema, description, builtin, enabled,
    vendor_options, default_models,
    created_at, updated_at
) VALUES (
    'seedance-volcengine',
    'Seedance 官方',
    'seedance-volcengine',
    'https://ark.cn-beijing.volces.com',
    '{"access_key": {"type": "string", "required": true, "description": "Access Key"}, "secret_key": {"type": "string", "required": true, "description": "Secret Key"}}'::jsonb,
    '火山引擎 Seedance 视频生成官方 API，支持 AK/SK HMAC 签名认证',
    true, true,
    '{"auth_type": "aksk", "supported_features": ["video_generation", "asset_management", "liveness"]}'::jsonb,
    '[{"id": "doubao-seedance-2-0-260128", "name": "Seedance 2.0"}, {"id": "doubao-seedance-2-0-fast-260128", "name": "Seedance 2.0 Fast"}]'::jsonb,
    NOW(), NOW()
) ON CONFLICT (id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    adapter = EXCLUDED.adapter,
    base_url = EXCLUDED.base_url,
    credential_schema = EXCLUDED.credential_schema,
    description = EXCLUDED.description,
    vendor_options = EXCLUDED.vendor_options,
    default_models = EXCLUDED.default_models,
    updated_at = NOW();
