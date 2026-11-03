-- 通知渠道配置表：存储每种通知类型的 webhook/email 投递配置
CREATE TABLE notification_channel_configs (
    notification_type VARCHAR(100) NOT NULL,
    channel           VARCHAR(50)  NOT NULL CHECK (channel IN ('webhook', 'email')),
    enabled           BOOLEAN      NOT NULL DEFAULT true,
    webhook_url       TEXT         NULL,
    webhook_secret    TEXT         NULL,   -- 加密存储
    email_recipients  TEXT         NULL,   -- 逗号分隔
    subject_template  TEXT         NULL,
    body_template     TEXT         NULL,
    created_at        DATETIME     NOT NULL DEFAULT NOW(),
    updated_at        DATETIME     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (notification_type, channel)
);
