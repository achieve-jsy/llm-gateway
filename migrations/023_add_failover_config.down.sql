-- 023_add_failover_config: 回滚
-- DOWN

ALTER TABLE model_groups
  DROP COLUMN IF EXISTS max_switches,
  DROP COLUMN IF EXISTS same_model_retries,
  DROP COLUMN IF EXISTS failover_cooldown_seconds;