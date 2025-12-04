-- 043_add_org_tables.down.sql
DROP TABLE IF EXISTS org_migration_log;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS dept_studio_map;
ALTER TABLE departments
    DROP COLUMN IF EXISTS owner_id,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS studio_id;
DROP TABLE IF EXISTS studios;
