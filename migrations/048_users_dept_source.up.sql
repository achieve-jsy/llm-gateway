-- 成员部门归属修正工作流：记录部门来源，防止手动修正被 SSO 自动覆盖
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS dept_source     VARCHAR(16)  NOT NULL DEFAULT 'sso',
    ADD COLUMN IF NOT EXISTS dept_updated_by VARCHAR(128),
    ADD COLUMN IF NOT EXISTS dept_updated_at TIMESTAMPTZ;

-- 冲突提醒：SSO claims 部门与手动归属不一致时写入
CREATE TABLE IF NOT EXISTS user_dept_conflicts (
    id          BIGSERIAL    PRIMARY KEY,
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    current_dept_id  UUID,
    sso_dept_id      UUID,
    sso_dept_name    VARCHAR(256),
    detected_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_user_dept_conflicts_user_id ON user_dept_conflicts(user_id);
CREATE INDEX IF NOT EXISTS idx_user_dept_conflicts_detected_at ON user_dept_conflicts(detected_at);
