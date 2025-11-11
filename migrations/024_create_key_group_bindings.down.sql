-- 024_create_key_group_bindings: 回滚
-- DOWN
DROP INDEX IF EXISTS idx_kbg_group_id;
DROP INDEX IF EXISTS idx_kbg_key_id;
DROP TABLE IF EXISTS key_group_bindings;