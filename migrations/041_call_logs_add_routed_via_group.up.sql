ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS routed_via_group boolean NOT NULL DEFAULT false;
