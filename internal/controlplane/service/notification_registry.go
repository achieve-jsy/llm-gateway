package service

import "github.com/example/llm-gateway/internal/controlplane/model"

// 渠道标识（统一全渠道视图）
const (
	ChannelInapp   = "inapp"
	ChannelEmail   = "email"
	ChannelWebhook = "webhook"
)

// NotificationTypeDef 通知类型定义：唯一事实源，前端矩阵与所有元数据 API 都从这里读。
type NotificationTypeDef struct {
	Name             string
	DisplayName      string
	Description      string
	// Category 业务域分类（budget/expansion/key/platform_risk/ops/notification/system），前端按此分组。
	Category string
	// CategoryName 分类展示名（预算/扩容/密钥/平台风险/运维/通知/系统）。
	CategoryName string
	// DefaultVariables 模板可用变量（前端配置弹窗提示用），与 DefaultTemplates 的变量名契约一致。
	DefaultVariables []string
	// Channels 该类型可用的渠道（决定前端矩阵中哪些列可交互）
	Channels []string
}

// NotificationCategoryDef 通知分类定义（去重、稳定顺序）。
type NotificationCategoryDef struct {
	Name        string
	DisplayName string
}

// notificationRegistry 全量通知类型注册表。
// 收录所有运行时真实发出的类型 + 计划中但暂无发送者的类型（保持已有 seed 配置有效）。
// DefaultVariables 必须与 DefaultTemplates / 消费者 TemplateData 的变量名契约保持一致。
var notificationRegistry = []NotificationTypeDef{
	// ── 预算 ──────────────────────────────────────────────────────────────
	{
		Name:             model.NotificationTypeBudgetAlert,
		DisplayName:      "预算告警",
		Description:      "预算使用率超过阈值告警",
		Category:         "budget",
		CategoryName:     "预算",
		DefaultVariables: []string{"ScopeType", "ScopeID", "PeriodType", "Threshold", "UsedUSD", "AmountUSD"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},
	{
		Name:             model.NotificationTypeBudgetOwnerAttention,
		DisplayName:      "预算负责人提醒",
		Description:      "预算负责人处理提醒",
		Category:         "budget",
		CategoryName:     "预算",
		DefaultVariables: []string{"Message"},
		Channels:         []string{ChannelInapp, ChannelEmail},
	},
	{
		Name:             model.NotificationTypeBudgetScheduleApplied,
		DisplayName:      "预算计划已生效",
		Description:      "预算计划已成功应用",
		Category:         "budget",
		CategoryName:     "预算",
		DefaultVariables: []string{"PeriodType", "AppliedAt"},
		Channels:         []string{ChannelInapp, ChannelEmail},
	},
	{
		Name:             model.NotificationTypeBudgetScheduleFailed,
		DisplayName:      "预算计划生效失败",
		Description:      "预算计划应用失败",
		Category:         "budget",
		CategoryName:     "预算",
		DefaultVariables: []string{"PeriodType", "AppliedAt", "Reason"},
		Channels:         []string{ChannelInapp, ChannelEmail},
	},

	// ── 扩容 ──────────────────────────────────────────────────────────────
	{
		Name:             model.NotificationTypeExpansionSubmitted,
		DisplayName:      "扩容申请提交",
		Description:      "新的扩容申请已提交",
		Category:         "expansion",
		CategoryName:     "扩容",
		DefaultVariables: []string{"ExpansionID", "Reason", "AmountUSD"},
		Channels:         []string{ChannelInapp, ChannelEmail},
	},
	{
		Name:             model.NotificationTypeExpansionApproved,
		DisplayName:      "扩容审批通过",
		Description:      "扩容申请审批已通过",
		Category:         "expansion",
		CategoryName:     "扩容",
		DefaultVariables: []string{"ExpansionID", "Reason", "AmountUSD"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},
	{
		Name:             model.NotificationTypeExpansionRejected,
		DisplayName:      "扩容审批拒绝",
		Description:      "扩容申请审批被拒绝",
		Category:         "expansion",
		CategoryName:     "扩容",
		DefaultVariables: []string{"ExpansionID", "Reason"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},
	{
		Name:             model.NotificationTypeExpansionDecision,
		DisplayName:      "扩容审批决策",
		Description:      "扩容审批决策已作出",
		Category:         "expansion",
		CategoryName:     "扩容",
		DefaultVariables: []string{"ExpansionID", "Decision", "Reason"},
		Channels:         []string{ChannelInapp, ChannelEmail},
	},
	{
		Name:             model.NotificationTypeExpansionApplyFailed,
		DisplayName:      "扩容执行失败",
		Description:      "扩容执行失败",
		Category:         "expansion",
		CategoryName:     "扩容",
		DefaultVariables: []string{"ExpansionID", "Error"},
		Channels:         []string{ChannelInapp, ChannelEmail},
	},
	{
		Name:             model.NotificationTypeExpansionExpiring,
		DisplayName:      "临时扩容即将到期",
		Description:      "临时扩容额度即将到期",
		Category:         "expansion",
		CategoryName:     "扩容",
		DefaultVariables: []string{"ExpansionID", "ExpireTime"},
		Channels:         []string{ChannelInapp, ChannelEmail},
	},
	{
		Name:             model.NotificationTypeExpansionRestored,
		DisplayName:      "扩容已恢复",
		Description:      "临时扩容已到期恢复",
		Category:         "expansion",
		CategoryName:     "扩容",
		DefaultVariables: []string{"ExpireTime"},
		Channels:         []string{ChannelInapp, ChannelEmail},
	},
	{
		Name:             model.NotificationTypeApprovalPoolEmpty,
		DisplayName:      "扩容审批池为空",
		Description:      "扩容审批池为空，无有效负责人",
		Category:         "expansion",
		CategoryName:     "扩容",
		DefaultVariables: []string{"ExpansionID"},
		Channels:         []string{ChannelInapp, ChannelEmail},
	},

	// ── 密钥 ──────────────────────────────────────────────────────────────
	{
		Name:             model.NotificationTypeKeyActivity,
		DisplayName:      "密钥活动异常",
		Description:      "密钥活动异常告警",
		Category:         "key",
		CategoryName:     "密钥",
		DefaultVariables: []string{"EventType", "KeyName", "UserAgent", "IPAddress", "Timestamp", "Detail"},
		Channels:         []string{ChannelInapp, ChannelEmail},
	},
	{
		Name:             model.NotificationTypeKeyRequestApproved,
		DisplayName:      "密钥申请审批通过",
		Description:      "密钥申请审批已通过",
		Category:         "key",
		CategoryName:     "密钥",
		DefaultVariables: []string{"KeyID"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},
	{
		Name:             model.NotificationTypeKeyApprovalPending,
		DisplayName:      "密钥审批待处理",
		Description:      "有新的 API Key 申请等待审批",
		Category:         "key",
		CategoryName:     "密钥",
		DefaultVariables: []string{"RequestID", "Applicant", "Department"},
		Channels:         []string{ChannelEmail, ChannelWebhook},
	},

	// ── 平台风险 ──────────────────────────────────────────────────────────
	{
		Name:             model.NotificationTypePlatformRiskCounterIntegrity,
		DisplayName:      "计数器完整性",
		Description:      "预算计数器完整性检查失败告警",
		Category:         "platform_risk",
		CategoryName:     "平台风险",
		DefaultVariables: []string{"Violations", "SourceRows", "KeysWritten", "Cooldown"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},
	{
		Name:             model.NotificationTypePlatformRiskPeriodHealth,
		DisplayName:      "周期健康",
		Description:      "预算周期健康检查异常告警",
		Category:         "platform_risk",
		CategoryName:     "平台风险",
		DefaultVariables: []string{"PeriodKey", "MissingScopes", "Cooldown"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},
	{
		Name:             model.NotificationTypePlatformRiskRedisHealth,
		DisplayName:      "Redis健康",
		Description:      "Redis 连接或数据一致性异常告警",
		Category:         "platform_risk",
		CategoryName:     "平台风险",
		DefaultVariables: []string{"Message", "Cooldown"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},

	// ── 运维（渠道/上游探活，系统级 UserID=Nil） ──────────────────────────
	{
		Name:             model.NotificationTypeOpsChannelDown,
		DisplayName:      "渠道探活失败",
		Description:      "上游渠道/模型探活失败（health check 自动禁用或 monitor 检测失败）",
		Category:         "ops",
		CategoryName:     "运维",
		DefaultVariables: []string{"Source", "ProviderName", "ProviderID", "MonitorName", "MonitorID", "Failures", "Threshold", "Error"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},
	{
		Name:             model.NotificationTypeOpsChannelRecovered,
		DisplayName:      "渠道恢复",
		Description:      "自动禁用渠道检测成功恢复",
		Category:         "ops",
		CategoryName:     "运维",
		DefaultVariables: []string{"Source", "ProviderName", "ProviderID", "MonitorName", "MonitorID"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},

	// ── 通知（通知系统自身，系统级 UserID=Nil） ────────────────────────────
	{
		Name:             model.NotificationTypeOpsDeliveryFailed,
		DisplayName:      "通知投递失败",
		Description:      "通知投递重试耗尽转入死信队列",
		Category:         "notification",
		CategoryName:     "通知",
		DefaultVariables: []string{"Type", "Title", "Reason", "Attempts"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},

	// ── 系统 ──────────────────────────────────────────────────────────────
	{
		Name:             model.NotificationTypeSystemStartup,
		DisplayName:      "系统启动",
		Description:      "系统启动通知",
		Category:         "system",
		CategoryName:     "系统",
		DefaultVariables: []string{"Version", "StartedAt"},
		Channels:         []string{ChannelInapp, ChannelEmail, ChannelWebhook},
	},
}

// GetNotificationTypeRegistry 返回全量通知类型注册表。
func GetNotificationTypeRegistry() []NotificationTypeDef {
	return notificationRegistry
}

// FindNotificationTypeDef 按名称查找类型定义。
func FindNotificationTypeDef(name string) (NotificationTypeDef, bool) {
	for _, t := range notificationRegistry {
		if t.Name == name {
			return t, true
		}
	}
	return NotificationTypeDef{}, false
}

// allRegistryChannels 返回注册表中所有类型渠道的并集（保持稳定顺序：inapp/email/webhook）。
func allRegistryChannels() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, ch := range []string{ChannelInapp, ChannelEmail, ChannelWebhook} {
		seen[ch] = true
		out = append(out, ch)
	}
	return out
}

// allNotificationCategories 返回注册表去重后的分类列表（按首次出现顺序，保持前端展示稳定）。
func allNotificationCategories() []NotificationCategoryDef {
	seen := map[string]bool{}
	out := []NotificationCategoryDef{}
	for _, t := range notificationRegistry {
		if t.Category == "" || seen[t.Category] {
			continue
		}
		seen[t.Category] = true
		out = append(out, NotificationCategoryDef{Name: t.Category, DisplayName: t.CategoryName})
	}
	return out
}
