-- 运维告警模板 seed：3 个新 ops_* 类型（渠道探活失败/恢复、通知投递死信）的 email + webhook 模板。
-- 系统级告警 UserID=Nil，email 早退 / inapp 不可见，webhook 是唯一实际送达渠道，
-- 但仍种 email 模板供管理端查看/自定义（若后续支持全局收件人也可直接用）。
-- 内容来源：internal/controlplane/service/notification_templates.go。
-- 幂等：ON CONFLICT DO UPDATE 仅填充空模板（COALESCE），不覆盖管理员已自定义的模板。

INSERT INTO notification_channel_configs (notification_type, channel, enabled, subject_template, body_template)
VALUES
    ('ops_channel_down', 'email', true,
     E'🚨 渠道探活失败{{if .ProviderName}}：{{.ProviderName}}{{end}}{{if .MonitorName}} / {{.MonitorName}}{{end}}',
     E'**渠道探活失败**\n\n- 来源：{{.Source}}\n{{if .ProviderName}}- 渠道：{{.ProviderName}}\n{{end}}{{if .ProviderID}}- ProviderID：{{.ProviderID}}\n{{end}}{{if .MonitorName}}- 监控：{{.MonitorName}}\n{{end}}{{if .MonitorID}}- MonitorID：{{.MonitorID}}\n{{end}}{{if .Failures}}- 连续失败：{{.Failures}} 次（阈值 {{.Threshold}}）\n{{end}}{{if .Error}}- 错误：{{.Error}}\n{{end}}时间：{{.Timestamp}}\n\n请立即检查上游渠道连通性与凭据配置。'),
    ('ops_channel_recovered', 'email', true,
     E'✅ 渠道已恢复{{if .ProviderName}}：{{.ProviderName}}{{end}}{{if .MonitorName}} / {{.MonitorName}}{{end}}',
     E'**渠道已恢复**\n\n- 来源：{{.Source}}\n{{if .ProviderName}}- 渠道：{{.ProviderName}}\n{{end}}{{if .ProviderID}}- ProviderID：{{.ProviderID}}\n{{end}}{{if .MonitorName}}- 监控：{{.MonitorName}}\n{{end}}{{if .MonitorID}}- MonitorID：{{.MonitorID}}\n{{end}}时间：{{.Timestamp}}\n\n自动禁用/失败状态已恢复，流量已重新接入。'),
    ('ops_delivery_failed', 'email', true,
     E'🔥 通知投递失败：{{.Type}}',
     E'**通知投递失败**\n\n- 通知类型：{{.Type}}\n{{if .Title}}- 标题：{{.Title}}\n{{end}}- 失败原因：{{.Reason}}\n- 重试次数：{{.Attempts}}\n- 时间：{{.Timestamp}}\n\n通知已转入死信队列，请检查 webhook 端点 / 邮件网关配置。')
ON CONFLICT (notification_type, channel) DO UPDATE SET
    subject_template = COALESCE(notification_channel_configs.subject_template, EXCLUDED.subject_template),
    body_template    = COALESCE(notification_channel_configs.body_template, EXCLUDED.body_template);

INSERT INTO notification_channel_configs (notification_type, channel, enabled, subject_template, body_template)
VALUES
    ('ops_channel_down', 'webhook', true,
     E'🚨 渠道探活失败{{if .ProviderName}}：{{.ProviderName}}{{end}}{{if .MonitorName}} / {{.MonitorName}}{{end}}',
     E'**渠道探活失败**\n\n- 来源：{{.Source}}\n{{if .ProviderName}}- 渠道：{{.ProviderName}}\n{{end}}{{if .ProviderID}}- ProviderID：{{.ProviderID}}\n{{end}}{{if .MonitorName}}- 监控：{{.MonitorName}}\n{{end}}{{if .MonitorID}}- MonitorID：{{.MonitorID}}\n{{end}}{{if .Failures}}- 连续失败：{{.Failures}} 次（阈值 {{.Threshold}}）\n{{end}}{{if .Error}}- 错误：{{.Error}}\n{{end}}时间：{{.Timestamp}}\n\n请立即检查上游渠道连通性与凭据配置。'),
    ('ops_channel_recovered', 'webhook', true,
     E'✅ 渠道已恢复{{if .ProviderName}}：{{.ProviderName}}{{end}}{{if .MonitorName}} / {{.MonitorName}}{{end}}',
     E'**渠道已恢复**\n\n- 来源：{{.Source}}\n{{if .ProviderName}}- 渠道：{{.ProviderName}}\n{{end}}{{if .ProviderID}}- ProviderID：{{.ProviderID}}\n{{end}}{{if .MonitorName}}- 监控：{{.MonitorName}}\n{{end}}{{if .MonitorID}}- MonitorID：{{.MonitorID}}\n{{end}}时间：{{.Timestamp}}\n\n自动禁用/失败状态已恢复，流量已重新接入。'),
    ('ops_delivery_failed', 'webhook', true,
     E'🔥 通知投递失败：{{.Type}}',
     E'**通知投递失败**\n\n- 通知类型：{{.Type}}\n{{if .Title}}- 标题：{{.Title}}\n{{end}}- 失败原因：{{.Reason}}\n- 重试次数：{{.Attempts}}\n- 时间：{{.Timestamp}}\n\n通知已转入死信队列，请检查 webhook 端点 / 邮件网关配置。')
ON CONFLICT (notification_type, channel) DO UPDATE SET
    subject_template = COALESCE(notification_channel_configs.subject_template, EXCLUDED.subject_template),
    body_template    = COALESCE(notification_channel_configs.body_template, EXCLUDED.body_template);
