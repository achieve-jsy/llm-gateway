package service

import "github.com/example/llm-gateway/internal/controlplane/model"

// DefaultTemplates 默认通知模板：每种通知类型有默认的 subject 和 body。
// 渲染优先级：config 模板（DB，可编辑）→ 此处默认模板（站内信默认 + seed 来源 + 兜底）→ n.Title/n.Content。
// 变量名契约：消费者传入的 Notification.TemplateData 的 key 必须与此处模板引用的变量名一致
//（数值统一由消费者预格式化为字符串，避免模板内 printf 在缺值时产生 <no value>）。
// 这是 seed 迁移（20260804000001_seed_notification_templates.sql）的内容来源。
var DefaultTemplates = map[string]struct {
	Subject string
	Body    string
}{
	// 预算相关
	model.NotificationTypeBudgetAlert: {
		Subject: "预算告警：{{.ScopeType}} 用量已达 {{.Threshold}}%",
		Body:    "**{{.ScopeType}}**（ID: {{.ScopeID}}）{{if eq .PeriodType \"daily\"}}今日{{else if eq .PeriodType \"weekly\"}}本周{{else}}本月{{end}}预算用量已达 **{{.Threshold}}%**（${{.UsedUSD}} / ${{.AmountUSD}} USD），请及时关注并考虑扩容申请。\n\n- 已用：${{.UsedUSD}} USD\n- 总额：${{.AmountUSD}} USD\n- 告警档位：{{.Threshold}}%\n\n> 告警不等于拦截，预算真正用尽时才会触发拦截。",
	},
	model.NotificationTypeBudgetOwnerAttention: {
		Subject: "预算负责人处理提醒",
		Body:    "{{.Message}}\n\n请及时进入预算管理处理该预算事项。",
	},
	model.NotificationTypeBudgetScheduleApplied: {
		Subject: "预算计划已生效",
		Body:    "{{.PeriodType}}预算计划已于 {{.AppliedAt}} 生效。",
	},
	model.NotificationTypeBudgetScheduleFailed: {
		Subject: "预算计划生效失败",
		Body:    "{{.PeriodType}}预算计划于 {{.AppliedAt}} 生效失败：{{.Reason}}。请进入预算管理重新配置。",
	},
	model.NotificationTypeApprovalPoolEmpty: {
		Subject: "扩容审批池为空",
		Body:    "扩容审批池为空\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}请补充当前作用域的有效负责人。",
	},

	// 扩容相关
	model.NotificationTypeExpansionSubmitted: {
		Subject: "新的扩容审批申请",
		Body:    "新的扩容审批申请\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}{{if .AmountUSD}}申请金额: ${{.AmountUSD}} USD\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionApproved: {
		Subject: "扩容申请已批准并生效",
		Body:    "扩容申请已批准并生效\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}{{if .AmountUSD}}批准金额: ${{.AmountUSD}} USD\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionRejected: {
		Subject: "扩容申请已驳回",
		Body:    "扩容申请已驳回\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Reason}}审批意见: {{.Reason}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionDecision: {
		Subject: "扩容审批决策",
		Body:    "扩容审批决策\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Decision}}决策: {{.Decision}}\n{{end}}{{if .Reason}}审批意见: {{.Reason}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionApplyFailed: {
		Subject: "扩容申请生效失败",
		Body:    "扩容申请生效失败\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Error}}错误: {{.Error}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionExpiring: {
		Subject: "临时扩容即将到期",
		Body:    "临时扩容即将到期\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .ExpireTime}}到期时间: {{.ExpireTime}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionRestored: {
		Subject: "临时扩容已到期",
		Body:    "临时额度已于 {{.ExpireTime}} 到期，当前额度恢复为届时生效的长期基础额度。",
	},

	// 密钥相关
	model.NotificationTypeKeyActivity: {
		Subject: "{{if eq .EventType \"spike\"}}Key 用量异常提醒{{else if eq .EventType \"wakeup\"}}Key 重新启用提醒{{else if eq .EventType \"first_use\"}}Key 首次使用{{else}}Key 新设备使用提醒{{end}}",
		Body:    "{{if eq .EventType \"spike\"}}你的 Key〔{{.KeyName}}〕用量异常：最近 1 小时调用 {{.Count}} 次，明显高于你的日常水平（基线约 {{.Mean}}）。{{else if eq .EventType \"wakeup\"}}你的 Key〔{{.KeyName}}〕沉睡 {{.DormantDays}} 天后重新被使用：客户端 {{.UserAgent}}，IP {{.IPAddress}}，时间 {{.Timestamp}}。{{else if eq .EventType \"first_use\"}}你的 Key〔{{.KeyName}}〕首次被使用：客户端 {{.UserAgent}}，IP {{.IPAddress}}，时间 {{.Timestamp}}。{{else}}你的 Key〔{{.KeyName}}〕检测到新设备使用：客户端 {{.UserAgent}}，IP {{.IPAddress}}，时间 {{.Timestamp}}。若非本人操作，请尽快重置 Key。{{end}}",
	},
	model.NotificationTypeKeyRequestApproved: {
		Subject: "密钥申请审批通过",
		Body:    "密钥申请审批已通过\n\n{{if .KeyID}}密钥ID: {{.KeyID}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeKeyApprovalPending: {
		Subject: "密钥审批待处理",
		Body:    "有新的 API Key 申请等待审批\n\n{{if .RequestID}}申请ID: {{.RequestID}}\n{{end}}{{if .Applicant}}申请人: {{.Applicant}}\n{{end}}{{if .Department}}部门: {{.Department}}\n{{end}}时间: {{.Timestamp}}",
	},

	// 平台风险
	model.NotificationTypePlatformRiskCounterIntegrity: {
		Subject: "🚨 计数器完整性告警：发现 {{.Violations}} 个异常",
		Body:    "**计数器完整性检查失败**\n\n- 异常计数器数量：{{.Violations}}\n- 检查时间：{{.Timestamp}}\n- 数据来源行数：{{.SourceRows}}\n- 写入 key 数量：{{.KeysWritten}}\n\n请立即检查 Redis 计数器状态，必要时执行重建。\n\n> 告警冷却时间：{{.Cooldown}}",
	},
	model.NotificationTypePlatformRiskPeriodHealth: {
		Subject: "⚠️ 周期健康告警：{{.PeriodKey}} 预算 key 缺失",
		Body:    "**周期健康检查失败**\n\n- 周期 key：{{.PeriodKey}}\n- 缺失作用域：{{.MissingScopes}}\n- 检查时间：{{.Timestamp}}\n\n以下作用域当前周期的预算计数器未写入 Redis，可能影响预算控制。\n\n> 告警冷却时间：{{.Cooldown}}",
	},
	model.NotificationTypePlatformRiskRedisHealth: {
		Subject: "🔥 Redis 健康告警：连接异常",
		Body:    "**Redis 健康检查失败**\n\n- 错误信息：{{.Message}}\n- 检查时间：{{.Timestamp}}\n\nRedis 连接异常可能影响预算控制、限流等核心功能。\n\n> 告警冷却时间：{{.Cooldown}}",
	},

	// 系统
	model.NotificationTypeSystemStartup: {
		Subject: "系统启动",
		Body:    "系统已启动\n\n{{if .Version}}版本: {{.Version}}\n{{end}}{{if .StartedAt}}启动时间: {{.StartedAt}}\n{{end}}时间: {{.Timestamp}}",
	},

	// 运维告警（系统级，UserID=Nil，实际只送达 webhook）
	model.NotificationTypeOpsChannelDown: {
		Subject: "🚨 渠道探活失败{{if .ProviderName}}：{{.ProviderName}}{{end}}{{if .MonitorName}} / {{.MonitorName}}{{end}}",
		Body:    "**渠道探活失败**\n\n- 来源：{{.Source}}\n{{if .ProviderName}}- 渠道：{{.ProviderName}}\n{{end}}{{if .ProviderID}}- ProviderID：{{.ProviderID}}\n{{end}}{{if .MonitorName}}- 监控：{{.MonitorName}}\n{{end}}{{if .MonitorID}}- MonitorID：{{.MonitorID}}\n{{end}}{{if .Failures}}- 连续失败：{{.Failures}} 次（阈值 {{.Threshold}}）\n{{end}}{{if .Error}}- 错误：{{.Error}}\n{{end}}时间：{{.Timestamp}}\n\n请立即检查上游渠道连通性与凭据配置。",
	},
	model.NotificationTypeOpsChannelRecovered: {
		Subject: "✅ 渠道已恢复{{if .ProviderName}}：{{.ProviderName}}{{end}}{{if .MonitorName}} / {{.MonitorName}}{{end}}",
		Body:    "**渠道已恢复**\n\n- 来源：{{.Source}}\n{{if .ProviderName}}- 渠道：{{.ProviderName}}\n{{end}}{{if .ProviderID}}- ProviderID：{{.ProviderID}}\n{{end}}{{if .MonitorName}}- 监控：{{.MonitorName}}\n{{end}}{{if .MonitorID}}- MonitorID：{{.MonitorID}}\n{{end}}时间：{{.Timestamp}}\n\n自动禁用/失败状态已恢复，流量已重新接入。",
	},
	model.NotificationTypeOpsDeliveryFailed: {
		Subject: "🔥 通知投递失败：{{.Type}}",
		Body:    "**通知投递失败**\n\n- 通知类型：{{.Type}}\n{{if .Title}}- 标题：{{.Title}}\n{{end}}- 失败原因：{{.Reason}}\n- 重试次数：{{.Attempts}}\n- 时间：{{.Timestamp}}\n\n通知已转入死信队列，请检查 webhook 端点 / 邮件网关配置。",
	},
}
