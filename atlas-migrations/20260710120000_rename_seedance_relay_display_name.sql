-- 将 Seedance 三方中转渠道展示名改为「景汐」
-- 同时修正 default_models 为字符串数组（与 ProviderDefinition.DefaultModels []string 对齐）
UPDATE "provider_definitions"
SET
    "display_name" = '景汐',
    "description" = '景汐（Seedance 三方中转），支持 Bearer Token 认证',
    "default_models" = '["doubao-seedance-2-0-260128", "doubao-seedance-2-0-fast-260128"]'::jsonb,
    "updated_at" = NOW()
WHERE "id" = 'seedance-relay';

UPDATE "provider_definitions"
SET
    "default_models" = '["doubao-seedance-2-0-260128", "doubao-seedance-2-0-fast-260128"]'::jsonb,
    "updated_at" = NOW()
WHERE "id" = 'seedance-volcengine'
  AND jsonb_typeof("default_models" -> 0) = 'object';
