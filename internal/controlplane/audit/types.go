package audit

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	contextKeyRecorded     = "_audit_recorded"
	contextKeyResourceName = "_audit_resource_name"
	contextKeyBefore       = "_audit_before"
	contextKeyAfter        = "_audit_after"
	contextKeyReason       = "_audit_reason"
	contextKeyScopeType    = "_audit_scope_type"
	contextKeyScopeID      = "_audit_scope_id"
)

// Action 审计操作类型标识（类型安全字符串）
type Action string

// Resource 被操作的资源类型
type Resource string

// ── 操作动作（按资源域分组，便于 grep）──────────────────────────

const (
	ActionServiceKeyCreate    Action = "service_key.create"
	ActionServiceKeyDelete    Action = "service_key.delete"
	ActionServiceKeyToggle    Action = "service_key.toggle"
	ActionServiceKeyIPChanged Action = "service_key.ip_allowlist_changed"
	ActionServiceKeyReset     Action = "service_key.reset"

	ActionProviderCreate     Action = "provider.create"
	ActionProviderUpdate     Action = "provider.update"
	ActionProviderDelete     Action = "provider.delete"
	ActionProviderToggle     Action = "provider.toggle"
	ActionProviderTest       Action = "provider.test"
	ActionProviderSyncModels Action = "provider.sync_models"

	ActionModelCreate            Action = "model.create"
	ActionModelUpdate            Action = "model.update"
	ActionModelDelete            Action = "model.delete"
	ActionModelBatchUpdateStatus Action = "model.batch_update_status"

	ActionGroupCreate            Action = "group.create"
	ActionGroupUpdate            Action = "group.update"
	ActionGroupDelete            Action = "group.delete"
	ActionGroupAddModels         Action = "group.add_models"
	ActionGroupRemoveModel       Action = "group.remove_model"
	ActionGroupEntitlementGrant  Action = "group.entitlement_grant"
	ActionGroupEntitlementRevoke Action = "group.entitlement_revoke"

	ActionBudgetSet                 Action = "budget.set"
	ActionBudgetDelete              Action = "budget.delete"
	ActionModelTierCreate           Action = "model_tier.create"
	ActionModelTierUpdate           Action = "model_tier.update"
	ActionModelTierStatus           Action = "model_tier.status"
	ActionModelTierReorder          Action = "model_tier.reorder"
	ActionModelTierArchive          Action = "model_tier.archive"
	ActionModelTierDelete           Action = "model_tier.delete"
	ActionBudgetTierLimitSet        Action = "budget_tier_limit.set"
	ActionBudgetTierLimitSchedule   Action = "budget_tier_limit.schedule"
	ActionBudgetTierReplaceSchedule Action = "budget_tier_limit.replace_schedule"

	ActionExpansionCreate      Action = "expansion.create"
	ActionExpansionDecision    Action = "expansion.decision"
	ActionExpansionPrecheck    Action = "expansion.precheck"
	ActionExpansionReprecheck  Action = "expansion.reprecheck"
	ActionExpansionNotifyOwner Action = "expansion.notify_owner"
	ActionExpansionApplyFailed Action = "expansion.apply_failed"
	ActionBudgetAdjustPreview  Action = "budget.adjust.preview"
	ActionBudgetAdjustApply    Action = "budget.adjust.apply"

	ActionKeyCreate          Action = "key.create"
	ActionKeyDisable         Action = "key.disable"
	ActionKeyEnable          Action = "key.enable"
	ActionKeyDelete          Action = "key.delete"
	ActionKeyReset           Action = "key.reset"
	ActionKeyToggleLog       Action = "key.toggle_log"
	ActionKeyToggleAccessAll Action = "key.toggle_access_all"
	ActionKeyBindGroups      Action = "key.bind_groups"

	ActionUserLogin  Action = "user.login"
	ActionUserLogout Action = "user.logout"

	ActionSettingsUpdate Action = "settings.update"

	ActionAnnouncementPublish Action = "announcement.publish"
	ActionAnnouncementDelete  Action = "announcement.delete"

	ActionHealthCheckRun Action = "health_check.run"

	ActionEmployeeImport Action = "employee.import"

	ActionEmailTest     Action = "email.send_test"
	ActionEmailAlert    Action = "email.send_alert"
	ActionWebhookAlert  Action = "webhook.send_alert"

	// 通知配置审计
	ActionNotificationChannelRulesUpdate Action = "notification_channel_rules.update"
	ActionNotificationConfigUpdate       Action = "notification_config.update"
	ActionNotificationConfigTest         Action = "notification_config.test"
	ActionWebhookTargetCreate            Action = "webhook_target.create"
	ActionWebhookTargetUpdate            Action = "webhook_target.update"
	ActionWebhookTargetDelete            Action = "webhook_target.delete"

	// v0.15 配额审计
	ActionQuotaDefaultUpdate    Action = "quota.default.update"
	ActionQuotaUserUpdate       Action = "quota.user.update"
	ActionQuotaServiceKeyUpdate Action = "quota.service_key.update"
	ActionQuotaGroupUpdate      Action = "quota.group.update"
	ActionQuotaModelUpdate      Action = "quota.model.update"
	ActionQuotaExemptionCreate  Action = "quota.exemption.create"
	ActionQuotaExemptionRevoke  Action = "quota.exemption.revoke"
)

// ── 资源类型常量 ──────────────────────────────────────────────

const (
	ResourceServiceKey   Resource = "service_key"
	ResourceProvider     Resource = "provider"
	ResourceModel        Resource = "model"
	ResourceGroup        Resource = "group"
	ResourceBudget       Resource = "budget"
	ResourceModelTier    Resource = "model_tier"
	ResourceExpansion    Resource = "expansion_request"
	ResourceKey          Resource = "api_key"
	ResourceUser         Resource = "user"
	ResourceSettings     Resource = "settings"
	ResourceAnnouncement Resource = "announcement"
	ResourceHealthCheck  Resource = "health_check"
	ResourceEmployee     Resource = "employee"
	ResourceEmail        Resource = "email"
	ResourceWebhook      Resource = "webhook"
	ResourceNotificationChannelRules Resource = "notification_channel_rules"
	ResourceNotificationConfig       Resource = "notification_config"
	ResourceWebhookTarget            Resource = "webhook_target"
	ResourceQuota        Resource = "quota"
	ResourceUnknown      Resource = "unknown"
)

// Input 审计日志输入（所有字段由调用方组装）
type Input struct {
	UserID       uuid.UUID
	UserName     string
	Action       Action
	ResourceType Resource
	ResourceID   string
	ResourceName string
	DeptID       string
	DeptName     string
	ClientIP     string
	UserAgent    string
	StatusCode   int // HTTP 状态码，0 默认 200
	Details      any // 可选，会被 JSON 序列化存储
	Timestamp    time.Time
}

// MarkRecorded 供 handler 在手动埋点后调用，标记已审计，使全局中间件跳过自动记录
func MarkRecorded(c *gin.Context) {
	c.Set(contextKeyRecorded, true)
}

// IsRecorded 全局中间件调用，判断当前请求是否已被手动记录
func IsRecorded(c *gin.Context) bool {
	v, _ := c.Get(contextKeyRecorded)
	return v == true
}

// SetResourceName 供 handler 注入资源名（如 provider 名、model 名），供中间件读取
func SetResourceName(c *gin.Context, name string) {
	c.Set(contextKeyResourceName, name)
}

// GetResourceNameCtx 中间件读取 handler 注入的资源名
func GetResourceNameCtx(c *gin.Context) string {
	v, ok := c.Get(contextKeyResourceName)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func SetBeforeAfter(c *gin.Context, before, after any) {
	c.Set(contextKeyBefore, before)
	c.Set(contextKeyAfter, after)
}

func GetBeforeAfter(c *gin.Context) (any, any) {
	before, _ := c.Get(contextKeyBefore)
	after, _ := c.Get(contextKeyAfter)
	return before, after
}

func SetReason(c *gin.Context, reason string) {
	c.Set(contextKeyReason, reason)
}

func GetReason(c *gin.Context) string {
	reason, _ := c.Get(contextKeyReason)
	value, _ := reason.(string)
	return value
}

func SetScope(c *gin.Context, scopeType, scopeID string) {
	c.Set(contextKeyScopeType, scopeType)
	c.Set(contextKeyScopeID, scopeID)
}

func GetScope(c *gin.Context) (string, string) {
	scopeType, _ := c.Get(contextKeyScopeType)
	scopeID, _ := c.Get(contextKeyScopeID)
	typeValue, _ := scopeType.(string)
	idValue, _ := scopeID.(string)
	return typeValue, idValue
}

const contextKeyActionOverride = "audit_action_override"

func SetActionOverride(c *gin.Context, action Action) {
	c.Set(contextKeyActionOverride, action)
}

func GetActionOverride(c *gin.Context) (Action, bool) {
	value, ok := c.Get(contextKeyActionOverride)
	action, valid := value.(Action)
	return action, ok && valid && action != ""
}
