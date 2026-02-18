-- 049: Key 申请审批与部门策略管控
-- feat/key-approval-with-budget

-- ── 部门策略配置表 ──────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS dept_policies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dept_id         VARCHAR(64)  NOT NULL,                        -- SSO dept_id（与 api_keys.dept_id 同语义）
    policy_key      VARCHAR(64)  NOT NULL,                        -- 策略名称，如 'key_creation_policy'
    policy_value    VARCHAR(255) NOT NULL DEFAULT '',
    description     TEXT,
    updated_by      UUID,                                         -- user.id
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (dept_id, policy_key)
);

CREATE INDEX idx_dept_policies_dept_id ON dept_policies(dept_id);

COMMENT ON TABLE dept_policies IS '部门级策略配置（如 Key 创建策略：free/approval_required）';

-- 默认全局策略写入 system_configs（key = default_key_creation_policy）
INSERT INTO system_configs (key, value)
VALUES ('default_key_creation_policy', 'free')
ON CONFLICT (key) DO NOTHING;

-- ── Key 创建申请表 ─────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS key_requests (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_id     UUID         NOT NULL,                       -- user.id（申请人）
    requester_name   VARCHAR(128),                                -- 申请人姓名冗余（供列表直接展示）
    dept_id          VARCHAR(64),                                 -- 申请人部门 SSO dept_id（冗余）
    dept_name        VARCHAR(128),                                -- 申请人部门名（冗余）
    key_name         VARCHAR(128) NOT NULL,                       -- 申请的 Key 名称
    key_type         VARCHAR(16)  NOT NULL DEFAULT 'personal',   -- personal | service
    purpose          TEXT,                                        -- 申请用途说明
    group_ids        TEXT,                                        -- JSON int 数组，如 "[1,2,3]"
    expires_at       TIMESTAMPTZ,                                -- Key 可选过期时间
    budget_monthly   NUMERIC(12,4) DEFAULT 0,                    -- 同时申请的月预算 USD；0/NULL = 不申请
    status           VARCHAR(16)  NOT NULL DEFAULT 'pending',    -- pending | approved | rejected | cancelled
    approver_id      UUID,                                        -- 审批人 user.id
    approve_comment  TEXT,
    created_key_id   UUID,                                        -- 审批通过后创建的 api_keys.id
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_key_requests_requester_id ON key_requests(requester_id);
CREATE INDEX idx_key_requests_status        ON key_requests(status);
CREATE INDEX idx_key_requests_dept_id       ON key_requests(dept_id);

COMMENT ON TABLE key_requests IS 'Key 创建申请（可附带个人月预算申请，仅需部门负责人审批）';
