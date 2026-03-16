CREATE TABLE IF NOT EXISTS alert_silences (
  id SERIAL PRIMARY KEY,
  rule_id INTEGER NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
  platform VARCHAR(100) DEFAULT '',
  group_id INTEGER,
  region VARCHAR(100) DEFAULT '',
  until TIMESTAMPTZ NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_by VARCHAR(255) DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_alert_silences_rule_id ON alert_silences(rule_id);
CREATE INDEX IF NOT EXISTS idx_alert_silences_until ON alert_silences(until);