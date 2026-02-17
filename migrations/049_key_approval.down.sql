-- 回滚：移除 049 Key 申请审批相关表与配置
DROP TABLE IF EXISTS key_requests;
DROP TABLE IF EXISTS dept_policies;
DELETE FROM system_configs WHERE key = 'default_key_creation_policy';
