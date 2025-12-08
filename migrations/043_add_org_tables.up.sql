-- 043_add_org_tables.up.sql
-- 新增 org-service 所需表：studios、dept_studio_map、user_roles、org_migration_log
-- 同时扩展 departments 表（新增 studio_id、status、owner_id 字段）

-- 工作室表
CREATE TABLE IF NOT EXISTS studios (
    id         VARCHAR(64) PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    owner_id   VARCHAR(64),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_studios_name ON studios(name);

-- 扩展 departments 表（追加字段，不破坏现有数据）
ALTER TABLE departments
    ADD COLUMN IF NOT EXISTS studio_id  VARCHAR(64) REFERENCES studios(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS status     VARCHAR(32) NOT NULL DEFAULT 'pending_studio',
    ADD COLUMN IF NOT EXISTS owner_id   VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_departments_studio_id ON departments(studio_id);
CREATE INDEX IF NOT EXISTS idx_departments_status ON departments(status);

-- 部门↔工作室映射表
CREATE TABLE IF NOT EXISTS dept_studio_map (
    dept_id    VARCHAR(64) PRIMARY KEY,
    studio_id  VARCHAR(64) NOT NULL REFERENCES studios(id),
    source     VARCHAR(16) NOT NULL DEFAULT 'manual',
    updated_by VARCHAR(64) NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_dept_studio_map_studio_id ON dept_studio_map(studio_id);

-- RBAC 角色授权表
CREATE TABLE IF NOT EXISTS user_roles (
    id         BIGSERIAL PRIMARY KEY,
    user_id    VARCHAR(64) NOT NULL,
    role       VARCHAR(32) NOT NULL,
    scope_type VARCHAR(32) NOT NULL,
    scope_id   VARCHAR(64) NOT NULL DEFAULT '',
    granted_by VARCHAR(64) NOT NULL,
    actor_id   VARCHAR(64),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_roles UNIQUE (user_id, role, scope_id)
);
CREATE INDEX IF NOT EXISTS idx_user_roles_user_id ON user_roles(user_id);
CREATE INDEX IF NOT EXISTS idx_user_roles_scope ON user_roles(scope_type, scope_id);

-- 部门迁移日志
CREATE TABLE IF NOT EXISTS org_migration_log (
    id          BIGSERIAL PRIMARY KEY,
    user_id     VARCHAR(64) NOT NULL,
    from_dept   VARCHAR(64) NOT NULL,
    to_dept     VARCHAR(64) NOT NULL,
    migrated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_migration_log_user_id ON org_migration_log(user_id);
