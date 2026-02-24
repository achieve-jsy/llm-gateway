CREATE TABLE IF NOT EXISTS quota_exemptions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_type VARCHAR(32)  NOT NULL,
    subject_id   VARCHAR(255) NOT NULL,
    reason       TEXT,
    expires_at   TIMESTAMPTZ  NOT NULL,
    created_by   VARCHAR(255) NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_quota_exemptions_active ON quota_exemptions (subject_type, subject_id) WHERE expires_at > now();
