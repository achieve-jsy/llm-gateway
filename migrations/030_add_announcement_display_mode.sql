ALTER TABLE announcements ADD COLUMN IF NOT EXISTS display_mode VARCHAR(20) NOT NULL DEFAULT 'banner'
    CHECK (display_mode IN ('banner', 'popup'));