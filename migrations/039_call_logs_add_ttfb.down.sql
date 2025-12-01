-- 039 rollback
ALTER TABLE call_logs   DROP COLUMN IF EXISTS stage_ttfb_ms;
ALTER TABLE call_traces DROP COLUMN IF EXISTS stage_ttfb_ms;
