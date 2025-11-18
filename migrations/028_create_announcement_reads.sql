CREATE TABLE IF NOT EXISTS announcement_reads (
    user_id UUID NOT NULL,
    announcement_id UUID NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
    is_read BOOLEAN NOT NULL DEFAULT FALSE,
    is_dismissed BOOLEAN NOT NULL DEFAULT FALSE,
    read_at TIMESTAMP WITH TIME ZONE DEFAULT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, announcement_id)
);

CREATE INDEX IF NOT EXISTS idx_announcement_reads_user ON announcement_reads (user_id, is_read);