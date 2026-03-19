-- 057_ehr_sync_tables.down.sql
-- 回滚 EHR 组织结构同步相关表

DROP TABLE IF EXISTS ehr_sync_stat_snapshots;
DROP TABLE IF EXISTS ehr_sync_skip_logs;
DROP TABLE IF EXISTS ehr_sync_diff_reports;
DROP TABLE IF EXISTS ehr_sync_error_logs;
DROP TABLE IF EXISTS ehr_sync_change_logs;
DROP TABLE IF EXISTS ehr_sync_execution_logs;
DROP TABLE IF EXISTS ehr_positions;
DROP TABLE IF EXISTS ehr_employees;
DROP TABLE IF EXISTS ehr_departments;
DROP TABLE IF EXISTS ehr_companies;
DROP TABLE IF EXISTS ehr_organizations;
