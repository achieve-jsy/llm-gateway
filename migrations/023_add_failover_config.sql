-- 023_add_failover_config: 模型组故障切换配置
-- UP

ALTER TABLE model_groups
  ADD COLUMN IF NOT EXISTS max_switches INTEGER NOT NULL DEFAULT 2,
  ADD COLUMN IF NOT EXISTS same_model_retries INTEGER NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS failover_cooldown_seconds INTEGER NOT NULL DEFAULT 60;