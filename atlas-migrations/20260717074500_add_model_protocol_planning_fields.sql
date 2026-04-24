-- Add upstream planner v2 protocol planning fields to models.
-- These columns are additive and default to old behavior: auto protocol/profile,
-- preserve unknown/unsupported params, and no per-model overrides.
ALTER TABLE "models"
  ADD COLUMN IF NOT EXISTS "base_model" varchar(255) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS "protocol_mode" varchar(40) NOT NULL DEFAULT 'auto',
  ADD COLUMN IF NOT EXISTS "param_profile" varchar(64) NOT NULL DEFAULT 'auto',
  ADD COLUMN IF NOT EXISTS "unsupported_param_policy" varchar(40) NOT NULL DEFAULT 'preserve',
  ADD COLUMN IF NOT EXISTS "supported_params_override" jsonb NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS "transform_overrides" jsonb NOT NULL DEFAULT '{}';
