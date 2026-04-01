-- Seedance 官方渠道：支持方舟 API Key / AK·SK 显式鉴权方式
UPDATE "provider_definitions"
SET
    "credential_schema" = '{
      "api_key": {"type": "string", "label": "方舟 API Key", "secret": true, "required": false, "placeholder": "方舟控制台 API Key"},
      "access_key": {"type": "string", "label": "Access Key", "secret": true, "required": false, "placeholder": "AK..."},
      "secret_key": {"type": "string", "label": "Secret Key", "secret": true, "required": false, "placeholder": "SK..."},
      "region": {"type": "string", "label": "Region", "secret": false, "required": false, "placeholder": "cn-beijing", "default": "cn-beijing"}
    }'::jsonb,
    "description" = '火山引擎 Seedance 官方 API。认证方式可选：方舟 API Key（Bearer，默认）或 IAM AK/SK（HMAC）',
    "updated_at" = NOW()
WHERE "id" = 'seedance-volcengine';
