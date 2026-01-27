-- Add key activity tracking tables (Key Activity Alerts feature)

-- Table: key_source_seen
-- Tracks unique (key_id, source_fp) pairs for E1 (new source) and E2 (first use) detection.
CREATE TABLE key_source_seen (
    id         BIGSERIAL   PRIMARY KEY,
    key_id     TEXT        NOT NULL,
    source_fp  VARCHAR(80) NOT NULL,
    ua_fp      VARCHAR(40) NOT NULL,
    ua_raw     TEXT        NOT NULL DEFAULT '',
    ip         VARCHAR(64) NOT NULL DEFAULT '',
    first_seen TIMESTAMPTZ NOT NULL,
    last_seen  TIMESTAMPTZ NOT NULL,
    hit_count  BIGINT      NOT NULL DEFAULT 1
);

-- Create unique index "uniq_key_source" to table: "key_source_seen"
CREATE UNIQUE INDEX uniq_key_source ON key_source_seen (key_id, source_fp);

-- Create index "idx_kss_key" to table: "key_source_seen"
CREATE INDEX idx_kss_key ON key_source_seen (key_id);


-- Table: key_activity_state
-- Per-key EWMA baseline state for E4 (usage spike) and E3 (wakeup) detection.
CREATE TABLE key_activity_state (
    key_id          TEXT PRIMARY KEY,
    ewma_mean       DOUBLE PRECISION NOT NULL DEFAULT 0,
    ewma_dev        DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_seen_at    TIMESTAMPTZ,
    last_spike_hour TIMESTAMPTZ,
    samples         BIGINT  NOT NULL DEFAULT 0,
    baseline_ready  BOOLEAN NOT NULL DEFAULT false,
    updated_at      TIMESTAMPTZ NOT NULL
);


-- Table: key_activity_alert
-- Alert records with deduplication via unique constraint on (key_id, event_type, dedup_key).
CREATE TABLE key_activity_alert (
    id          BIGSERIAL   PRIMARY KEY,
    key_id      TEXT        NOT NULL,
    user_id     TEXT        NOT NULL,
    event_type  VARCHAR(16) NOT NULL,
    dedup_key   VARCHAR(96) NOT NULL,
    ua_raw      TEXT        NOT NULL DEFAULT '',
    ip          VARCHAR(64) NOT NULL DEFAULT '',
    detail      JSONB,
    notified_at TIMESTAMPTZ NOT NULL
);

-- Create unique index "uniq_activity_alert" to table: "key_activity_alert"
CREATE UNIQUE INDEX uniq_activity_alert ON key_activity_alert (key_id, event_type, dedup_key);

-- Create index "idx_kaa_user" to table: "key_activity_alert"
CREATE INDEX idx_kaa_user ON key_activity_alert (user_id, notified_at DESC);
