CREATE TABLE IF NOT EXISTS alert_events (
  id SERIAL PRIMARY KEY,
  rule_id INTEGER NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
  severity VARCHAR(10) NOT NULL,
  status VARCHAR(30) NOT NULL DEFAULT 'firing',
  title VARCHAR(500) NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  metric_value DOUBLE PRECISION NOT NULL DEFAULT 0,
  threshold_value DOUBLE PRECISION NOT NULL DEFAULT 0,
  dimensions JSONB DEFAULT '{}',
  fired_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at TIMESTAMPTZ,
  email_sent BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_alert_events_fired_at ON alert_events(fired_at DESC);
CREATE INDEX idx_alert_events_rule_id ON alert_events(rule_id);
CREATE INDEX idx_alert_events_status ON alert_events(status);
