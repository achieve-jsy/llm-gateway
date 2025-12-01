ALTER TABLE announcements ADD COLUMN IF NOT EXISTS category VARCHAR(20) NOT NULL DEFAULT 'system'
    CHECK (category IN ('system', 'feature', 'alert'));