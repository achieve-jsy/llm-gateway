-- 024_create_key_group_bindings: 新增 Key↔模型组多对多绑定关联表
-- UP

CREATE TABLE IF NOT EXISTS key_group_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id UUID NOT NULL,
    group_id INTEGER NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_kbg_key_group UNIQUE (key_id, group_id),
    CONSTRAINT fk_kbg_key FOREIGN KEY (key_id) REFERENCES api_keys(id) ON DELETE CASCADE,
    CONSTRAINT fk_kbg_group FOREIGN KEY (group_id) REFERENCES model_groups(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_kbg_key_id ON key_group_bindings(key_id);
CREATE INDEX IF NOT EXISTS idx_kbg_group_id ON key_group_bindings(group_id);