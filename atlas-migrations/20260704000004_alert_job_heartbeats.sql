CREATE TABLE IF NOT EXISTS alert_job_heartbeats (
  id SERIAL PRIMARY KEY,
  name VARCHAR(255) NOT NULL UNIQUE,
  status VARCHAR(30) NOT NULL DEFAULT 'running',
  last_duration_ms INTEGER NOT NULL DEFAULT 0,
  last_success_at TIMESTAMPTZ,
  last_error_at TIMESTAMPTZ,
  last_result TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);