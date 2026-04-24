-- Add param_style to models table for explicit parameter adaptation strategy.
-- Values: '' = auto-detect by model name (backward compat), 'reasoning' = reasoning model
-- adaptation (max_tokens→max_completion_tokens, drop temperature), 'standard' = pass-through.
ALTER TABLE "models" ADD COLUMN IF NOT EXISTS "param_style" varchar(20) NOT NULL DEFAULT '';
