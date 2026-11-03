package service

import "github.com/example/llm-gateway/internal/controlplane/model"

// DefaultTemplates 默认通知模板：每种通知类型有默认的 subject 和 body
var DefaultTemplates = map[string]struct {
	Subject string
	Body    string
}{
	// 预算相关
	model.NotificationTypeBudgetAlert: {
		Subject: "[LLM Gateway] 预算告警",
		Body:    "预算告警触发\n\n{{if .BudgetName}}预算名称: {{.BudgetName}}\n{{end}}{{if .CurrentUsage}}当前用量: {{.CurrentUsage}}\n{{end}}{{if .Threshold}}阈值: {{.Threshold}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeBudgetOwnerAttention: {
		Subject: "[LLM Gateway] 预算负责人提醒",
		Body:    "预算负责人提醒\n\n{{if .BudgetName}}预算名称: {{.BudgetName}}\n{{end}}{{if .Owner}}负责人: {{.Owner}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeBudgetScheduleApplied: {
		Subject: "[LLM Gateway] 预算计划已生效",
		Body:    "预算计划已生效\n\n{{if .ScheduleID}}计划ID: {{.ScheduleID}}\n{{end}}{{if .AllocationID}}分配ID: {{.AllocationID}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeBudgetScheduleFailed: {
		Subject: "[LLM Gateway] 预算计划生效失败",
		Body:    "预算计划生效失败\n\n{{if .ScheduleID}}计划ID: {{.ScheduleID}}\n{{end}}{{if .AllocationID}}分配ID: {{.AllocationID}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeApprovalPoolEmpty: {
		Subject: "[LLM Gateway] 扩容审批池为空",
		Body:    "扩容审批池为空\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}请补充当前作用域的有效负责人。\n时间: {{.Timestamp}}",
	},

	// 扩容相关
	model.NotificationTypeExpansionSubmitted: {
		Subject: "[LLM Gateway] 扩容申请已提交",
		Body:    "扩容申请已提交\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Applicant}}申请人: {{.Applicant}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionApproved: {
		Subject: "[LLM Gateway] 扩容审批通过",
		Body:    "扩容审批已通过\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Approver}}审批人: {{.Approver}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionRejected: {
		Subject: "[LLM Gateway] 扩容审批拒绝",
		Body:    "扩容审批被拒绝\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Approver}}审批人: {{.Approver}}\n{{end}}{{if .Reason}}原因: {{.Reason}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionDecision: {
		Subject: "[LLM Gateway] 扩容审批决策",
		Body:    "扩容审批决策\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Decision}}决策: {{.Decision}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionApplyFailed: {
		Subject: "[LLM Gateway] 扩容执行失败",
		Body:    "扩容执行失败\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .Error}}错误: {{.Error}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionExpiring: {
		Subject: "[LLM Gateway] 临时扩容即将到期",
		Body:    "临时扩容即将到期\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}{{if .ExpireTime}}到期时间: {{.ExpireTime}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeExpansionRestored: {
		Subject: "[LLM Gateway] 扩容已恢复",
		Body:    "扩容已恢复\n\n{{if .ExpansionID}}申请ID: {{.ExpansionID}}\n{{end}}时间: {{.Timestamp}}",
	},

	// 密钥相关
	model.NotificationTypeKeyActivity: {
		Subject: "[LLM Gateway] 密钥活动异常",
		Body:    "密钥活动异常\n\n{{if .KeyID}}密钥ID: {{.KeyID}}\n{{end}}{{if .Activity}}活动: {{.Activity}}\n{{end}}{{if .Detail}}详情: {{.Detail}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeKeyRequestApproved: {
		Subject: "[LLM Gateway] 密钥申请审批通过",
		Body:    "密钥申请审批已通过\n\n{{if .RequestID}}申请ID: {{.RequestID}}\n{{end}}{{if .KeyID}}密钥ID: {{.KeyID}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypeKeyApprovalPending: {
		Subject: "[LLM Gateway] 密钥审批待处理",
		Body:    "有新的 API Key 申请等待审批\n\n{{if .RequestID}}申请ID: {{.RequestID}}\n{{end}}{{if .Applicant}}申请人: {{.Applicant}}\n{{end}}{{if .Department}}部门: {{.Department}}\n{{end}}时间: {{.Timestamp}}",
	},

	// 平台风险
	model.NotificationTypePlatformRiskCounterIntegrity: {
		Subject: "[LLM Gateway] 平台风险计数器异常",
		Body:    "平台风险计数器完整性检查失败\n\n{{if .RiskType}}风险类型: {{.RiskType}}\n{{end}}{{if .Counter}}计数器: {{.Counter}}\n{{end}}{{if .Expected}}期望值: {{.Expected}}\n{{end}}{{if .Actual}}实际值: {{.Actual}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypePlatformRiskPeriodHealth: {
		Subject: "[LLM Gateway] 平台风险周期健康",
		Body:    "预算周期健康检查失败\n\n{{if .PeriodID}}周期: {{.PeriodID}}\n{{end}}{{if .Status}}状态: {{.Status}}\n{{end}}{{if .Message}}详情: {{.Message}}\n{{end}}时间: {{.Timestamp}}",
	},
	model.NotificationTypePlatformRiskRedisHealth: {
		Subject: "[LLM Gateway] 平台风险 Redis 健康",
		Body:    "Redis 健康检查失败\n\n{{if .Node}}节点: {{.Node}}\n{{end}}{{if .ErrorType}}错误类型: {{.ErrorType}}\n{{end}}{{if .Message}}详情: {{.Message}}\n{{end}}时间: {{.Timestamp}}",
	},

	// 系统
	model.NotificationTypeSystemStartup: {
		Subject: "[LLM Gateway] 系统启动",
		Body:    "系统已启动\n\n{{if .Version}}版本: {{.Version}}\n{{end}}{{if .StartedAt}}启动时间: {{.StartedAt}}\n{{end}}时间: {{.Timestamp}}",
	},
}
