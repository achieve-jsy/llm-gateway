-- 057_ehr_sync_tables.up.sql
-- EHR 组织结构同步相关表

-- 1. 组织架构表
CREATE TABLE IF NOT EXISTS ehr_organizations (
    id UUID PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    org_name VARCHAR(255) NOT NULL,
    org_code VARCHAR(64) NOT NULL DEFAULT '',
    ehr_create_date VARCHAR(64) NOT NULL DEFAULT '',
    ehr_modify_date VARCHAR(64) NOT NULL DEFAULT '',
    synced_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(org_id)
);

CREATE INDEX IF NOT EXISTS idx_ehr_org_modify_date ON ehr_organizations(ehr_modify_date);

-- 2. 公司表
CREATE TABLE IF NOT EXISTS ehr_companies (
    id UUID PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    company_id VARCHAR(64) NOT NULL,
    company_name VARCHAR(255) NOT NULL,
    parent_company_id VARCHAR(64),
    ehr_modify_date VARCHAR(64) NOT NULL DEFAULT '',
    synced_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, company_id)
);

CREATE INDEX IF NOT EXISTS idx_ehr_comp_modify_date ON ehr_companies(ehr_modify_date);

-- 3. 部门表（含 override 字段）
CREATE TABLE IF NOT EXISTS ehr_departments (
    id UUID PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    department_id VARCHAR(64) NOT NULL,
    department_name VARCHAR(255) NOT NULL,
    parent_department_id VARCHAR(64),
    leader_name VARCHAR(128) NOT NULL DEFAULT '',
    leader_employee_id VARCHAR(64) NOT NULL DEFAULT '',
    leader_email VARCHAR(128) NOT NULL DEFAULT '',
    manual_override_at TIMESTAMP WITH TIME ZONE,
    manual_override_at_ehr TIMESTAMP WITH TIME ZONE,
    ehr_modify_date VARCHAR(64) NOT NULL DEFAULT '',
    synced_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, department_id)
);

CREATE INDEX IF NOT EXISTS idx_ehr_dept_modify_date ON ehr_departments(ehr_modify_date);
CREATE INDEX IF NOT EXISTS idx_ehr_dept_override_at ON ehr_departments(manual_override_at);

-- 4. 员工表（含 override 字段）
CREATE TABLE IF NOT EXISTS ehr_employees (
    id UUID PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    employee_id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL DEFAULT '',
    email VARCHAR(128) NOT NULL DEFAULT '',
    department_id VARCHAR(64) NOT NULL DEFAULT '',
    manual_override_at TIMESTAMP WITH TIME ZONE,
    manual_override_at_ehr TIMESTAMP WITH TIME ZONE,
    ehr_modify_date VARCHAR(64) NOT NULL DEFAULT '',
    synced_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, employee_id)
);

CREATE INDEX IF NOT EXISTS idx_ehr_emp_modify_date ON ehr_employees(ehr_modify_date);
CREATE INDEX IF NOT EXISTS idx_ehr_emp_override_at ON ehr_employees(manual_override_at);

-- 5. 岗位表
CREATE TABLE IF NOT EXISTS ehr_positions (
    id UUID PRIMARY KEY,
    org_id VARCHAR(64) NOT NULL,
    position_id VARCHAR(64) NOT NULL,
    position_name VARCHAR(255) NOT NULL,
    synced_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, position_id)
);

-- 6. 同步执行日志表
CREATE TABLE IF NOT EXISTS ehr_sync_execution_logs (
    id UUID PRIMARY KEY,
    batch_id VARCHAR(64) NOT NULL,
    entity_type VARCHAR(32) NOT NULL,
    sync_type VARCHAR(16) NOT NULL,
    status VARCHAR(16) NOT NULL,
    records_fetched INT NOT NULL DEFAULT 0,
    records_created INT NOT NULL DEFAULT 0,
    records_updated INT NOT NULL DEFAULT 0,
    records_deleted INT NOT NULL DEFAULT 0,
    records_skipped INT NOT NULL DEFAULT 0,
    error_message TEXT,
    started_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMP WITH TIME ZONE,
    duration_ms INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ehr_exec_batch_id ON ehr_sync_execution_logs(batch_id);
CREATE INDEX IF NOT EXISTS idx_ehr_exec_entity_type ON ehr_sync_execution_logs(entity_type);
CREATE INDEX IF NOT EXISTS idx_ehr_exec_status ON ehr_sync_execution_logs(status);

-- 7. 同步变更日志表
CREATE TABLE IF NOT EXISTS ehr_sync_change_logs (
    id UUID PRIMARY KEY,
    batch_id VARCHAR(64) NOT NULL,
    entity_type VARCHAR(32) NOT NULL,
    entity_id VARCHAR(64) NOT NULL,
    change_type VARCHAR(16) NOT NULL,
    before_data JSONB,
    after_data JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ehr_change_batch_id ON ehr_sync_change_logs(batch_id);
CREATE INDEX IF NOT EXISTS idx_ehr_change_entity ON ehr_sync_change_logs(entity_type, entity_id);

-- 8. 同步错误日志表
CREATE TABLE IF NOT EXISTS ehr_sync_error_logs (
    id UUID PRIMARY KEY,
    batch_id VARCHAR(64) NOT NULL,
    entity_type VARCHAR(32) NOT NULL,
    entity_id VARCHAR(64),
    error_message TEXT NOT NULL,
    error_details JSONB,
    retry_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ehr_error_batch_id ON ehr_sync_error_logs(batch_id);
CREATE INDEX IF NOT EXISTS idx_ehr_error_entity ON ehr_sync_error_logs(entity_type, entity_id);

-- 9. 同步差异报告表
CREATE TABLE IF NOT EXISTS ehr_sync_diff_reports (
    id UUID PRIMARY KEY,
    batch_id VARCHAR(64) NOT NULL,
    sync_type VARCHAR(16) NOT NULL,
    total_records INT NOT NULL DEFAULT 0,
    records_created INT NOT NULL DEFAULT 0,
    records_updated INT NOT NULL DEFAULT 0,
    records_deleted INT NOT NULL DEFAULT 0,
    records_skipped INT NOT NULL DEFAULT 0,
    records_override INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ehr_diff_batch_id ON ehr_sync_diff_reports(batch_id);

-- 10. 同步跳过日志表（override 导致的跳过）
CREATE TABLE IF NOT EXISTS ehr_sync_skip_logs (
    id UUID PRIMARY KEY,
    batch_id VARCHAR(64) NOT NULL,
    entity_type VARCHAR(32) NOT NULL,
    entity_id VARCHAR(64) NOT NULL,
    reason VARCHAR(255) NOT NULL,
    override_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ehr_skip_batch_id ON ehr_sync_skip_logs(batch_id);
CREATE INDEX IF NOT EXISTS idx_ehr_skip_entity ON ehr_sync_skip_logs(entity_type, entity_id);

-- 11. 同步统计快照表
CREATE TABLE IF NOT EXISTS ehr_sync_stat_snapshots (
    id UUID PRIMARY KEY,
    snapshot_type VARCHAR(16) NOT NULL DEFAULT 'daily',
    org_id VARCHAR(64),
    organizations_count INT NOT NULL DEFAULT 0,
    companies_count INT NOT NULL DEFAULT 0,
    departments_count INT NOT NULL DEFAULT 0,
    employees_count INT NOT NULL DEFAULT 0,
    positions_count INT NOT NULL DEFAULT 0,
    override_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ehr_stat_snapshot_type ON ehr_sync_stat_snapshots(snapshot_type);
CREATE INDEX IF NOT EXISTS idx_ehr_stat_org_id ON ehr_sync_stat_snapshots(org_id);
