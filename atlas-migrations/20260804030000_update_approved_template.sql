-- expansion_approved 模板补充批准金额：发送方已传 AmountUSD，模板漏渲染。
-- 用 WHERE 守卫仅在模板仍是旧 seed 文案时更新，管理员已自定义的模板不受影响。
INSERT INTO notification_channel_configs (notification_type, channel, enabled, subject_template, body_template)
VALUES
    ('expansion_approved', 'email', true,
     E'扩容申请已批准并生效',
     E'扩容申请已批准并生效\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}{{if .AmountUSD}}批准金额: ${{.AmountUSD}} USD\n{{end}}时间: {{.Timestamp}}'),
    ('expansion_approved', 'webhook', true,
     E'扩容申请已批准并生效',
     E'扩容申请已批准并生效\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}{{if .AmountUSD}}批准金额: ${{.AmountUSD}} USD\n{{end}}时间: {{.Timestamp}}')
ON CONFLICT (notification_type, channel) DO UPDATE SET
    subject_template = EXCLUDED.subject_template,
    body_template    = EXCLUDED.body_template
WHERE notification_channel_configs.subject_template = E'扩容申请已批准并生效'
  AND notification_channel_configs.body_template = E'扩容申请已批准并生效\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}时间: {{.Timestamp}}';
