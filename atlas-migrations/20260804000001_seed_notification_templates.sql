-- 通知模板 seed：把代码内 DefaultTemplates 的内容种进 config 表（email + webhook 渠道），
-- UI 可直接查看/编辑，改文案不用发版。内容来源：internal/controlplane/service/notification_templates.go。
-- 幂等：ON CONFLICT DO UPDATE 仅填充空模板（COALESCE），不覆盖管理员已自定义的模板。
-- 行内 enabled=true：email 实际发送仍受全局 email_enabled 门控；webhook 取决于端点/URL 配置。

-- ─── email 渠道（15 类型，registry Channels 含 email）───
INSERT INTO notification_channel_configs (notification_type, channel, enabled, subject_template, body_template)
VALUES
    ('budget_alert', 'email', true,
     E'预算告警：{{.ScopeType}} 用量已达 {{.Threshold}}%',
     E'**{{.ScopeType}}**（ID: {{.ScopeID}}）本月预算用量已达 **{{.Threshold}}%**（${{.UsedUSD}} / ${{.AmountUSD}} USD），请及时关注并考虑扩容申请。\n\n- 已用：${{.UsedUSD}} USD\n- 总额：${{.AmountUSD}} USD\n- 告警档位：{{.Threshold}}%\n\n> 告警不等于拦截，预算真正用尽时才会触发拦截。'),
    ('budget_owner_attention', 'email', true,
     E'预算负责人处理提醒',
     E'{{.Message}}\n\n请及时进入预算管理处理该预算事项。'),
    ('budget_schedule_applied', 'email', true,
     E'预算计划已生效',
     E'{{.PeriodType}}预算计划已于 {{.AppliedAt}} 生效。'),
    ('budget_schedule_failed', 'email', true,
     E'预算计划生效失败',
     E'{{.PeriodType}}预算计划于 {{.AppliedAt}} 生效失败：{{.Reason}}。请进入预算管理重新配置。'),
    ('expansion_submitted', 'email', true,
     E'新的扩容审批申请',
     E'新的扩容审批申请\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}{{if .AmountUSD}}申请金额: ${{.AmountUSD}} USD\n{{end}}时间: {{.Timestamp}}'),
    ('expansion_approved', 'email', true,
     E'扩容申请已批准并生效',
     E'扩容申请已批准并生效\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}时间: {{.Timestamp}}'),
    ('expansion_rejected', 'email', true,
     E'扩容申请已驳回',
     E'扩容申请已驳回\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}审批意见: {{.Reason}}\n{{end}}时间: {{.Timestamp}}'),
    ('expansion_decision', 'email', true,
     E'扩容审批决策',
     E'扩容审批决策\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Decision}}决策: {{.Decision}}\n{{end}}{{if .Reason}}审批意见: {{.Reason}}\n{{end}}时间: {{.Timestamp}}'),
    ('expansion_apply_failed', 'email', true,
     E'扩容申请生效失败',
     E'扩容申请生效失败\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Error}}错误: {{.Error}}\n{{end}}时间: {{.Timestamp}}'),
    ('expansion_expiring', 'email', true,
     E'临时扩容即将到期',
     E'临时扩容即将到期\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .ExpireTime}}到期时间: {{.ExpireTime}}\n{{end}}时间: {{.Timestamp}}'),
    ('expansion_restored', 'email', true,
     E'临时扩容已到期',
     E'临时额度已于 {{.ExpireTime}} 到期，当前额度恢复为届时生效的长期基础额度。'),
    ('approval_pool_empty', 'email', true,
     E'扩容审批池为空',
     E'扩容审批池为空\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}请补充当前作用域的有效负责人。'),
    ('key_activity', 'email', true,
     E'{{if eq .EventType \"spike\"}}Key 用量异常提醒{{else if eq .EventType \"wakeup\"}}Key 重新启用提醒{{else if eq .EventType \"first_use\"}}Key 首次使用{{else}}Key 新设备使用提醒{{end}}',
     E'{{if eq .EventType \"spike\"}}你的 Key〔{{.KeyName}}〕用量异常：最近 1 小时调用 {{.Count}} 次，明显高于你的日常水平（基线约 {{.Mean}}）。{{else if eq .EventType \"wakeup\"}}你的 Key〔{{.KeyName}}〕沉睡 {{.DormantDays}} 天后重新被使用：客户端 {{.UserAgent}}，IP {{.IPAddress}}，时间 {{.Timestamp}}。{{else if eq .EventType \"first_use\"}}你的 Key〔{{.KeyName}}〕首次被使用：客户端 {{.UserAgent}}，IP {{.IPAddress}}，时间 {{.Timestamp}}。{{else}}你的 Key〔{{.KeyName}}〕检测到新设备使用：客户端 {{.UserAgent}}，IP {{.IPAddress}}，时间 {{.Timestamp}}。若非本人操作，请尽快重置 Key。{{end}}'),
    ('key_request_approved', 'email', true,
     E'密钥申请审批通过',
     E'密钥申请审批已通过\n\n{{if .KeyID}}密钥ID: {{.KeyID}}\n{{end}}时间: {{.Timestamp}}'),
    ('key_approval_pending', 'email', true,
     E'密钥审批待处理',
     E'有新的 API Key 申请等待审批\n\n{{if .RequestID}}申请ID: {{.RequestID}}\n{{end}}{{if .Applicant}}申请人: {{.Applicant}}\n{{end}}{{if .Department}}部门: {{.Department}}\n{{end}}时间: {{.Timestamp}}')
ON CONFLICT (notification_type, channel) DO UPDATE SET
    subject_template = COALESCE(notification_channel_configs.subject_template, EXCLUDED.subject_template),
    body_template    = COALESCE(notification_channel_configs.body_template, EXCLUDED.body_template);

-- ─── webhook 渠道（9 类型，registry Channels 含 webhook；已存在行只填模板，不动 URL/secret）───
INSERT INTO notification_channel_configs (notification_type, channel, enabled, subject_template, body_template)
VALUES
    ('budget_alert', 'webhook', true,
     E'预算告警：{{.ScopeType}} 用量已达 {{.Threshold}}%',
     E'**{{.ScopeType}}**（ID: {{.ScopeID}}）本月预算用量已达 **{{.Threshold}}%**（${{.UsedUSD}} / ${{.AmountUSD}} USD），请及时关注并考虑扩容申请。\n\n- 已用：${{.UsedUSD}} USD\n- 总额：${{.AmountUSD}} USD\n- 告警档位：{{.Threshold}}%\n\n> 告警不等于拦截，预算真正用尽时才会触发拦截。'),
    ('expansion_approved', 'webhook', true,
     E'扩容申请已批准并生效',
     E'扩容申请已批准并生效\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}时间: {{.Timestamp}}'),
    ('expansion_rejected', 'webhook', true,
     E'扩容申请已驳回',
     E'扩容申请已驳回\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}审批意见: {{.Reason}}\n{{end}}时间: {{.Timestamp}}'),
    ('key_request_approved', 'webhook', true,
     E'密钥申请审批通过',
     E'密钥申请审批已通过\n\n{{if .KeyID}}密钥ID: {{.KeyID}}\n{{end}}时间: {{.Timestamp}}'),
    ('key_approval_pending', 'webhook', true,
     E'密钥审批待处理',
     E'有新的 API Key 申请等待审批\n\n{{if .RequestID}}申请ID: {{.RequestID}}\n{{end}}{{if .Applicant}}申请人: {{.Applicant}}\n{{end}}{{if .Department}}部门: {{.Department}}\n{{end}}时间: {{.Timestamp}}'),
    ('platform_risk_counter_integrity', 'webhook', true,
     E'🚨 计数器完整性告警：发现 {{.Violations}} 个异常',
     E'**计数器完整性检查失败**\n\n- 异常计数器数量：{{.Violations}}\n- 检查时间：{{.Timestamp}}\n- 数据来源行数：{{.SourceRows}}\n- 写入 key 数量：{{.KeysWritten}}\n\n请立即检查 Redis 计数器状态，必要时执行重建。\n\n> 告警冷却时间：{{.Cooldown}}'),
    ('platform_risk_period_health', 'webhook', true,
     E'⚠️ 周期健康告警：{{.PeriodKey}} 预算 key 缺失',
     E'**周期健康检查失败**\n\n- 周期 key：{{.PeriodKey}}\n- 检查时间：{{.Timestamp}}\n\nRedis 中未找到当前周期的预算计数器，可能影响预算控制。\n\n> 告警冷却时间：{{.Cooldown}}'),
    ('platform_risk_redis_health', 'webhook', true,
     E'🔥 Redis 健康告警：连接异常',
     E'**Redis 健康检查失败**\n\n- 错误信息：{{.Message}}\n- 检查时间：{{.Timestamp}}\n\nRedis 连接异常可能影响预算控制、限流等核心功能。\n\n> 告警冷却时间：{{.Cooldown}}'),
    ('system_startup', 'webhook', true,
     E'系统启动',
     E'系统已启动\n\n{{if .Version}}版本: {{.Version}}\n{{end}}{{if .StartedAt}}启动时间: {{.StartedAt}}\n{{end}}时间: {{.Timestamp}}')
ON CONFLICT (notification_type, channel) DO UPDATE SET
    subject_template = COALESCE(notification_channel_configs.subject_template, EXCLUDED.subject_template),
    body_template    = COALESCE(notification_channel_configs.body_template, EXCLUDED.body_template);
