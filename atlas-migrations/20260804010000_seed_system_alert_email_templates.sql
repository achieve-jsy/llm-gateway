-- 系统级告警邮件渠道模板 seed：platform_risk_* 与 system_startup 在 registry 开放 email 渠道后，
-- 补齐 email 模板（文案复用 20260804000001 中已 seed 的 webhook 模板）。
-- 幂等：ON CONFLICT DO UPDATE 仅填充空模板（COALESCE），不覆盖管理员已自定义的模板。
-- 行内 enabled=true：实际发送仍受全局 email_enabled 门控；系统告警无用户接收人，按现有分发逻辑不会送达 email。

INSERT INTO notification_channel_configs (notification_type, channel, enabled, subject_template, body_template)
VALUES
    ('platform_risk_counter_integrity', 'email', true,
     E'🚨 计数器完整性告警：发现 {{.Violations}} 个异常',
     E'**计数器完整性检查失败**\n\n- 异常计数器数量：{{.Violations}}\n- 检查时间：{{.Timestamp}}\n- 数据来源行数：{{.SourceRows}}\n- 写入 key 数量：{{.KeysWritten}}\n\n请立即检查 Redis 计数器状态，必要时执行重建。\n\n> 告警冷却时间：{{.Cooldown}}'),
    ('platform_risk_period_health', 'email', true,
     E'⚠️ 周期健康告警：{{.PeriodKey}} 预算 key 缺失',
     E'**周期健康检查失败**\n\n- 周期 key：{{.PeriodKey}}\n- 检查时间：{{.Timestamp}}\n\nRedis 中未找到当前周期的预算计数器，可能影响预算控制。\n\n> 告警冷却时间：{{.Cooldown}}'),
    ('platform_risk_redis_health', 'email', true,
     E'🔥 Redis 健康告警：连接异常',
     E'**Redis 健康检查失败**\n\n- 错误信息：{{.Message}}\n- 检查时间：{{.Timestamp}}\n\nRedis 连接异常可能影响预算控制、限流等核心功能。\n\n> 告警冷却时间：{{.Cooldown}}'),
    ('system_startup', 'email', true,
     E'系统启动',
     E'系统已启动\n\n{{if .Version}}版本: {{.Version}}\n{{end}}{{if .StartedAt}}启动时间: {{.StartedAt}}\n{{end}}时间: {{.Timestamp}}')
ON CONFLICT (notification_type, channel) DO UPDATE SET
    subject_template = COALESCE(notification_channel_configs.subject_template, EXCLUDED.subject_template),
    body_template    = COALESCE(notification_channel_configs.body_template, EXCLUDED.body_template);
