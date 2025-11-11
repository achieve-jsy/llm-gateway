-- 022_add_model_groups: 新增模型组和组内模型关联表
-- UP

CREATE TABLE IF NOT EXISTS model_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    description TEXT DEFAULT '',
    strategy VARCHAR(32) NOT NULL DEFAULT 'round_robin'
        CHECK (strategy IN ('round_robin', 'priority', 'weighted')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE DEFAULT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uniq_model_groups_name
    ON model_groups(name) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_model_groups_enabled
    ON model_groups(enabled) WHERE enabled = TRUE AND deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS group_models (
    id SERIAL PRIMARY KEY,
    group_id INTEGER NOT NULL
        REFERENCES model_groups(id) ON DELETE CASCADE,
    model_id UUID NOT NULL
        REFERENCES models(id) ON DELETE CASCADE,
    priority INTEGER DEFAULT NULL,
    weight INTEGER DEFAULT NULL CHECK (weight >= 1 AND weight <= 100),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uniq_group_model UNIQUE (group_id, model_id)
);

CREATE INDEX IF NOT EXISTS idx_group_models_model_id
    ON group_models(model_id);
