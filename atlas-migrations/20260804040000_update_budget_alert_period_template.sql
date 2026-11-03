-- budget_alert 模板按周期动态措辞：发送方新增 PeriodType（daily/weekly/monthly），
-- 模板由硬编码"本月"改为按周期渲染"今日/本周/本月"。
-- 用 WHERE 守卫仅在模板仍是旧 seed 文案时更新，管理员已自定义的模板不受影响。
INSERT INTO notification_channel_configs (notification_type, channel, enabled, subject_template, body_template)
VALUES
    ('budget_alert', 'email', true,
     E'预算告警：{{.ScopeType}} 用量已达 {{.Threshold}}%',
     E'**{{.ScopeType}}**（ID: {{.ScopeID}}）{{if eq .PeriodType \"daily\"}}今日{{else if eq .PeriodType \"weekly\"}}本周{{else}}本月{{end}}预算用量已达 **{{.Threshold}}%**（${{.UsedUSD}} / ${{.AmountUSD}} USD），请及时关注并考虑扩容申请。\n\n- 已用：${{.UsedUSD}} USD\n- 总额：${{.AmountUSD}} USD\n- 告警档位：{{.Threshold}}%\n\n> 告警不等于拦截，预算真正用尽时才会触发拦截。'),
    ('budget_alert', 'webhook', true,
     E'预算告警：{{.ScopeType}} 用量已达 {{.Threshold}}%',
     E'**{{.ScopeType}}**（ID: {{.ScopeID}}）{{if eq .PeriodType \"daily\"}}今日{{else if eq .PeriodType \"weekly\"}}本周{{else}}本月{{end}}预算用量已达 **{{.Threshold}}%**（${{.UsedUSD}} / ${{.AmountUSD}} USD），请及时关注并考虑扩容申请。\n\n- 已用：${{.UsedUSD}} USD\n- 总额：${{.AmountUSD}} USD\n- 告警档位：{{.Threshold}}%\n\n> 告警不等于拦截，预算真正用尽时才会触发拦截。')
ON CONFLICT (notification_type, channel) DO UPDATE SET
    subject_template = EXCLUDED.subject_template,
    body_template    = EXCLUDED.body_template
WHERE notification_channel_configs.subject_template = E'预算告警：{{.ScopeType}} 用量已达 {{.Threshold}}%'
  AND notification_channel_configs.body_template = E'**{{.ScopeType}}**（ID: {{.ScopeID}}）本月预算用量已达 **{{.Threshold}}%**（${{.UsedUSD}} / ${{.AmountUSD}} USD），请及时关注并考虑扩容申请。\n\n- 已用：${{.UsedUSD}} USD\n- 总额：${{.AmountUSD}} USD\n- 告警档位：{{.Threshold}}%\n\n> 告警不等于拦截，预算真正用尽时才会触发拦截。';
