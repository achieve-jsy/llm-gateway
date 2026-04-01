-- 注册 Embedding/Rerank 相关渠道定义（幂等 upsert）
-- 前端「新建渠道 → 渠道类型」来自 provider_definitions；gateway factory 已支持这些 adapter。
--
-- - gemini: Google AI Studio，文本 embedding 走 OpenAI 兼容 /embeddings
-- - vertex: Vertex AI，文本 embedding 走 native predict
-- - cohere: Rerank 走 /v2/rerank；embedding 走兼容基类
-- - dashscope: 阿里百炼（adapter=bailian），文本/多模态 embedding + 原生 rerank

INSERT INTO provider_definitions (
    id, display_name, adapter, base_url,
    credential_schema, vendor_options, description, builtin, enabled,
    default_models, doc_url, created_at, updated_at
) VALUES (
    'gemini',
    'Google Gemini',
    'gemini',
    'https://generativelanguage.googleapis.com/v1beta/openai',
    '{"api_key": {"type": "string", "label": "API Key", "secret": true, "required": true, "placeholder": "AIza..."}}'::jsonb,
    '{"url_pattern": "standard", "auth_header": "bearer", "supports_stream_options": true}'::jsonb,
    'Google AI Studio Gemini。支持 Chat 与文本 Embedding（OpenAI 兼容 /embeddings）；不支持网关 /v1/rerank 与 embedding_vl 双端点。',
    true, true,
    '["gemini-2.0-flash", "text-embedding-004"]'::jsonb,
    'https://ai.google.dev/gemini-api/docs',
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

INSERT INTO provider_definitions (
    id, display_name, adapter, base_url,
    credential_schema, vendor_options, description, builtin, enabled,
    default_models, doc_url, created_at, updated_at
) VALUES (
    'vertex',
    'Google Vertex AI',
    'vertex',
    '',
    '{"api_key": {"type": "string", "label": "API Key / Access Token", "secret": true, "required": true, "placeholder": "ya29... 或服务账号相关凭证"}, "vertex_project": {"type": "string", "label": "GCP Project", "secret": false, "required": false, "placeholder": "my-gcp-project"}, "vertex_location": {"type": "string", "label": "Location", "secret": false, "required": false, "placeholder": "us-central1", "default": "us-central1"}}'::jsonb,
    '{"url_pattern": "standard", "skip_forward_check": true}'::jsonb,
    'Google Vertex AI。文本 Embedding 走 Vertex native predict；Endpoint 可填完整 Vertex OpenAI 兼容基址，或依赖 project/location。不支持网关 /v1/rerank。',
    true, true,
    '["text-embedding-004", "gemini-2.0-flash"]'::jsonb,
    'https://cloud.google.com/vertex-ai/generative-ai/docs',
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

INSERT INTO provider_definitions (
    id, display_name, adapter, base_url,
    credential_schema, vendor_options, description, builtin, enabled,
    default_models, doc_url, created_at, updated_at
) VALUES (
    'cohere',
    'Cohere',
    'cohere',
    'https://api.cohere.com',
    '{"api_key": {"type": "string", "label": "API Key", "secret": true, "required": true, "placeholder": "..."}}'::jsonb,
    '{"url_pattern": "standard", "auth_header": "bearer"}'::jsonb,
    'Cohere。Rerank 走原生 /v2/rerank；Embedding 走兼容路径。适合作为网关 /v1/rerank 上游。',
    true, true,
    '["rerank-v3.5", "rerank-multilingual-v3.0", "embed-english-v3.0"]'::jsonb,
    'https://docs.cohere.com/',
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

-- 空库可能从未有过 dashscope 行（旧 UPDATE 迁移无法插入）。此处幂等补齐，adapter 固定 bailian。
INSERT INTO provider_definitions (
    id, display_name, adapter, base_url,
    credential_schema, vendor_options, description, builtin, enabled,
    default_models, doc_url, created_at, updated_at
) VALUES (
    'dashscope',
    '阿里百炼',
    'bailian',
    'https://dashscope.aliyuncs.com/compatible-mode/v1',
    '{"api_key": {"type": "string", "label": "API Key", "secret": true, "required": true, "placeholder": "sk-..."}}'::jsonb,
    '{"url_pattern": "standard", "auth_header": "bearer", "stream_usage_in_last_chunk": true, "empty_choices_skip": true}'::jsonb,
    '阿里云百炼（DashScope）。文本 Embedding / Embedding-VL（多模态）/ Rerank 走 bailian 原生适配；模型类型请分别标 embedding、embedding_vl、rerank。',
    true, true,
    '["qwen-max", "qwen-plus", "qwen-turbo", "text-embedding-v4", "qwen3-vl-embedding", "gte-rerank"]'::jsonb,
    'https://help.aliyun.com/zh/model-studio/',
    NOW(), NOW()
) ON CONFLICT (id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    adapter = 'bailian',
    base_url = EXCLUDED.base_url,
    credential_schema = EXCLUDED.credential_schema,
    vendor_options = EXCLUDED.vendor_options,
    description = EXCLUDED.description,
    default_models = EXCLUDED.default_models,
    doc_url = EXCLUDED.doc_url,
    builtin = EXCLUDED.builtin,
    enabled = EXCLUDED.enabled,
    updated_at = NOW();
