-- 通知渠道配置 seed：为所有通知类型配置默认飞书 Webhook 渠道
-- 幂等：已存在的配置不会被覆盖（保留管理员后续的个性化设置）
INSERT INTO notification_channel_configs (notification_type, channel, enabled, webhook_url, webhook_secret)
VALUES
    ('platform_risk_counter_integrity', 'webhook', true, 'https://open.feishu.cn/open-apis/bot/v2/hook/8efdd4a3-e866-455d-8768-a033a062687e', 'rAon5zWZjaVTDGlc68mRT'),
    ('platform_risk_period_health',       'webhook', true, 'https://open.feishu.cn/open-apis/bot/v2/hook/8efdd4a3-e866-455d-8768-a033a062687e', 'rAon5zWZjaVTDGlc68mRT'),
    ('platform_risk_redis_health',        'webhook', true, 'https://open.feishu.cn/open-apis/bot/v2/hook/8efdd4a3-e866-455d-8768-a033a062687e', 'rAon5zWZjaVTDGlc68mRT'),
    ('budget_alert',                      'webhook', true, 'https://open.feishu.cn/open-apis/bot/v2/hook/8efdd4a3-e866-455d-8768-a033a062687e', 'rAon5zWZjaVTDGlc68mRT'),
    ('expansion_approved',                'webhook', true, 'https://open.feishu.cn/open-apis/bot/v2/hook/8efdd4a3-e866-455d-8768-a033a062687e', 'rAon5zWZjaVTDGlc68mRT'),
    ('expansion_rejected',                'webhook', true, 'https://open.feishu.cn/open-apis/bot/v2/hook/8efdd4a3-e866-455d-8768-a033a062687e', 'rAon5zWZjaVTDGlc68mRT'),
    ('expansion_applied',                 'webhook', true, 'https://open.feishu.cn/open-apis/bot/v2/hook/8efdd4a3-e866-455d-8768-a033a062687e', 'rAon5zWZjaVTDGlc68mRT'),
    ('key_approval_pending',              'webhook', true, 'https://open.feishu.cn/open-apis/bot/v2/hook/8efdd4a3-e866-455d-8768-a033a062687e', 'rAon5zWZjaVTDGlc68mRT'),
    ('system_startup',                    'webhook', true, 'https://open.feishu.cn/open-apis/bot/v2/hook/8efdd4a3-e866-455d-8768-a033a062687e', 'rAon5zWZjaVTDGlc68mRT')
ON CONFLICT (notification_type, channel) DO NOTHING;
