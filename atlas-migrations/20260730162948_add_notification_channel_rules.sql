-- 通知渠道规则表：管理每种通知类型的站内信/邮件开关
CREATE TABLE notification_channel_rules (
    notification_type VARCHAR(32) NOT NULL,
    channel           VARCHAR(16) NOT NULL CHECK (channel IN ('inapp', 'email')),
    enabled           BOOLEAN NOT NULL DEFAULT true,
    PRIMARY KEY (notification_type, channel)
);

-- 默认：全部开启，key_request_approved.email 保持现状（当前只发站内信）
INSERT INTO notification_channel_rules (notification_type, channel, enabled) VALUES
    ('budget_alert', 'inapp',   true),
    ('budget_alert', 'email',   true),
    ('expansion_approved', 'inapp',   true),
    ('expansion_approved', 'email',   true),
    ('expansion_rejected', 'inapp',   true),
    ('expansion_rejected', 'email',   true),
    ('expansion_submitted', 'inapp',   true),
    ('expansion_submitted', 'email',   true),
    ('expansion_decision', 'inapp',   true),
    ('expansion_decision', 'email',   true),
    ('expansion_apply_failed', 'inapp',   true),
    ('expansion_apply_failed', 'email',   true),
    ('expansion_expiring', 'inapp',   true),
    ('expansion_expiring', 'email',   true),
    ('expansion_restored', 'inapp',   true),
    ('expansion_restored', 'email',   true),
    ('budget_owner_attention', 'inapp',   true),
    ('budget_owner_attention', 'email',   true),
    ('key_activity', 'inapp',   true),
    ('key_activity', 'email',   true),
    ('key_request_approved', 'inapp',   true),
    ('key_request_approved', 'email',   false);
