-- Add 'auto' routing strategy to model_groups and complexity_tier to group_models
-- for the auto-routing (difficulty-aware) feature.

-- 1. Update model_groups.strategy CHECK to include 'auto'
ALTER TABLE "model_groups" DROP CONSTRAINT IF EXISTS "chk_model_groups_strategy";
ALTER TABLE "model_groups" ADD CONSTRAINT "chk_model_groups_strategy"
  CHECK ((strategy)::text = ANY ((ARRAY['round_robin'::character varying, 'priority'::character varying, 'weighted'::character varying, 'auto'::character varying])::text[]));

-- 2. Add complexity_tier column to group_models (default MEDIUM, not-null)
ALTER TABLE "group_models" ADD COLUMN IF NOT EXISTS "complexity_tier" character varying(16) NOT NULL DEFAULT 'MEDIUM';

-- 3. Add CHECK constraint on complexity_tier
DO $$ BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.check_constraints
    WHERE constraint_name = 'chk_group_models_complexity_tier'
  ) THEN
    ALTER TABLE "group_models" ADD CONSTRAINT "chk_group_models_complexity_tier"
      CHECK ((complexity_tier)::text = ANY ((ARRAY['SIMPLE'::character varying, 'MEDIUM'::character varying, 'COMPLEX'::character varying, 'REASONING'::character varying])::text[]));
  END IF;
END $$;
