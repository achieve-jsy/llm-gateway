-- atlas-migrations/20260811090000_add_group_mode_max_override.sql
-- Feature: group-level manual override for mode / context length / max output tokens
-- 新增 model_groups 的 override_mode、override_context_length、override_max_output_tokens 字段，
-- 用于在 is_capability_override 开启时手动填充 /v1/model/info 的 mode/max（成员全禁用或私有化部署无 catalog 时聚合值为空）。

ALTER TABLE model_groups ADD COLUMN IF NOT EXISTS override_mode varchar(50) DEFAULT NULL;
ALTER TABLE model_groups ADD COLUMN IF NOT EXISTS override_context_length integer DEFAULT NULL;
ALTER TABLE model_groups ADD COLUMN IF NOT EXISTS override_max_output_tokens integer DEFAULT NULL;

COMMENT ON COLUMN model_groups.override_mode IS 'Manual override for group mode (empty = inherit aggregated value)';
COMMENT ON COLUMN model_groups.override_context_length IS 'Manual override for max input/context tokens (NULL or <=0 = inherit aggregated value)';
COMMENT ON COLUMN model_groups.override_max_output_tokens IS 'Manual override for max output tokens (NULL or <=0 = inherit aggregated value)';
