-- period_health 检测器修复：改为按真实作用域检查计数器 key
-- （budget:v2:used:<scopeType>:<scopeID>:<windowKey>），告警新增 MissingScopes 变量列出缺失作用域。
-- 用 WHERE 守卫仅在模板仍是旧 seed 文案时更新，管理员已自定义的模板不受影响。
INSERT INTO notification_channel_configs (notification_type, channel, enabled, subject_template, body_template)
VALUES
    ('platform_risk_period_health', 'email', true,
     E'⚠️ 周期健康告警：{{.PeriodKey}} 预算 key 缺失',
     E'**周期健康检查失败**\n\n- 周期 key：{{.PeriodKey}}\n- 缺失作用域：{{.MissingScopes}}\n- 检查时间：{{.Timestamp}}\n\n以下作用域当前周期的预算计数器未写入 Redis，可能影响预算控制。\n\n> 告警冷却时间：{{.Cooldown}}'),
    ('platform_risk_period_health', 'webhook', true,
     E'⚠️ 周期健康告警：{{.PeriodKey}} 预算 key 缺失',
     E'**周期健康检查失败**\n\n- 周期 key：{{.PeriodKey}}\n- 缺失作用域：{{.MissingScopes}}\n- 检查时间：{{.Timestamp}}\n\n以下作用域当前周期的预算计数器未写入 Redis，可能影响预算控制。\n\n> 告警冷却时间：{{.Cooldown}}')
ON CONFLICT (notification_type, channel) DO UPDATE SET
    subject_template = EXCLUDED.subject_template,
    body_template    = EXCLUDED.body_template
WHERE notification_channel_configs.subject_template = E'⚠️ 周期健康告警：{{.PeriodKey}} 预算 key 缺失'
  AND notification_channel_configs.body_template = E'**周期健康检查失败**\n\n- 周期 key：{{.PeriodKey}}\n- 检查时间：{{.Timestamp}}\n\nRedis 中未找到当前周期的预算计数器，可能影响预算控制。\n\n> 告警冷却时间：{{.Cooldown}}';
