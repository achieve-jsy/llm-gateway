-- 多 webhook 支持:端点子表,把 (type, channel) 配置行的 URL/secret 抽成独立维度,
-- 使一个通知类型可同时发往多个飞书群(fan-out)。模板仍挂在配置行,渲染一次、发往所有端点。
CREATE TABLE notification_webhook_targets (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_type varchar(100) NOT NULL,
    name              varchar(100) NOT NULL DEFAULT '',
    url               text NOT NULL,
    secret            text NULL,
    enabled           boolean NOT NULL DEFAULT true,
    sort_order        integer NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL DEFAULT NOW(),
    updated_at        timestamptz NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_notification_webhook_targets_type ON notification_webhook_targets (notification_type);

-- 回填:存量 webhook config 行 → 默认端点,升级后无需任何操作继续生效。
-- 新表为空时幂等;若升级前管理员已改过 config 行 URL,回填的是最新值。
INSERT INTO notification_webhook_targets (id, notification_type, name, url, secret, enabled)
SELECT gen_random_uuid(), notification_type, '', webhook_url, webhook_secret, enabled
FROM notification_channel_configs
WHERE channel = 'webhook' AND webhook_url IS NOT NULL AND webhook_url != '';
