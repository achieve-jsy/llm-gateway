-- +migrate Up
CREATE TABLE IF NOT EXISTS video_tasks (
    task_id             VARCHAR(128) PRIMARY KEY,
    api_key_id          BIGINT NOT NULL,
    api_key_name        VARCHAR(128),
    api_key_type        VARCHAR(16),
    user_id             TEXT,
    user_name           VARCHAR(128),
    model               TEXT NOT NULL,
    provider_name       VARCHAR(64) NOT NULL,
    status              VARCHAR(32) NOT NULL DEFAULT 'submitted',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_video_tasks_ts ON video_tasks(created_at);
CREATE INDEX IF NOT EXISTS idx_video_tasks_api_key ON video_tasks(api_key_id);
CREATE INDEX IF NOT EXISTS idx_video_tasks_status ON video_tasks(status);
CREATE INDEX IF NOT EXISTS idx_video_tasks_model ON video_tasks(model);
