ALTER TABLE call_logs
    DROP COLUMN IF EXISTS request_body,
    DROP COLUMN IF EXISTS response_body;
