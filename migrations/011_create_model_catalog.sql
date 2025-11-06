-- 011_create_model_catalog.sql
-- 模型 Catalog（来自上游元数据源，例如 LiteLLM 价格/能力清单）
-- 用于：能力位/价格/上下文长度等 "事实数据" 的真源；
-- 与 models 表通过 models.catalog_id 外键软关联，能力位走 live JOIN 不复制。

CREATE TABLE IF NOT EXISTS model_catalog (
    id                                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source                              VARCHAR(50)  NOT NULL,
    source_model_id                     VARCHAR(200) NOT NULL,
    display_name                        VARCHAR(200),
    provider                            VARCHAR(50),
    mode                                VARCHAR(30),
    context_length                      BIGINT       DEFAULT 0,
    max_output_tokens                   BIGINT       DEFAULT 0,
    input_price_per_million             DECIMAL(14, 6) DEFAULT 0,
    output_price_per_million            DECIMAL(14, 6) DEFAULT 0,
    cache_read_price                    DECIMAL(14, 6),
    cache_write_price                   DECIMAL(14, 6),
    tier_threshold                      BIGINT,
    input_price_above_tier              DECIMAL(14, 6),
    output_price_above_tier             DECIMAL(14, 6),
    supports_function_calling           BOOLEAN      DEFAULT FALSE,
    supports_vision                     BOOLEAN      DEFAULT FALSE,
    supports_prompt_caching             BOOLEAN      DEFAULT FALSE,
    supports_response_schema            BOOLEAN      DEFAULT FALSE,
    supports_tool_choice                BOOLEAN      DEFAULT FALSE,
    supports_parallel_function_calling  BOOLEAN      DEFAULT FALSE,
    deprecation_date                    TIMESTAMPTZ,
    raw_payload                         JSONB,
    synced_at                           TIMESTAMPTZ  NOT NULL,
    created_at                          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at                          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (source, source_model_id)
);

CREATE INDEX IF NOT EXISTS idx_catalog_provider     ON model_catalog (provider);
CREATE INDEX IF NOT EXISTS idx_catalog_display      ON model_catalog (display_name);
CREATE INDEX IF NOT EXISTS idx_catalog_source_model ON model_catalog (source_model_id);
