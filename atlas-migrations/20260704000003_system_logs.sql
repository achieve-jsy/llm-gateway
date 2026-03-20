CREATE TABLE IF NOT EXISTS system_logs (
  id BIGSERIAL PRIMARY KEY,
  ts TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  level VARCHAR(20) NOT NULL,
  component VARCHAR(100) NOT NULL,
  message TEXT NOT NULL,
  request_id VARCHAR(64),
  client_request_id VARCHAR(64),
  user_id VARCHAR(64),
  provider VARCHAR(100),
  model VARCHAR(100),
  "group" VARCHAR(100),
  duration_ms INTEGER,
  status INTEGER,
  error_message TEXT,
  extra JSONB DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_system_logs_ts ON system_logs(ts DESC);
CREATE INDEX IF NOT EXISTS idx_system_logs_level ON system_logs(level);
CREATE INDEX IF NOT EXISTS idx_system_logs_component ON system_logs(component);
CREATE INDEX IF NOT EXISTS idx_system_logs_request_id ON system_logs(request_id);