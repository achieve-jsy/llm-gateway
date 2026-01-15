package audit

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	contextKeyRecorded     = "_audit_recorded"
	contextKeyResourceName = "_audit_resource_name"
)

// Action 审计操作类型标识（类型安全字符串）
type Action string

// Resource 被操作的资源类型
type Resource string

// ── 操作动作（按资源域分组，便于 grep）──────────────────────────

const (
	ActionServiceKeyCreate       Action = "service_key.create"
	ActionServiceKeyDelete       Action = "service_key.delete"
	ActionServiceKeyToggle       Action = "service_key.toggle"
	ActionServiceKeyIPChanged    Action = "service_key.ip_allowlist_changed"
	ActionServiceKeyReset        Action = "service_key.reset"

	ActionProviderCreate         Action = "provider.create"
	ActionProviderUpdate         Action = "provider.update"
	ActionProviderDelete         Action = "provider.delete"
	ActionProviderToggle         Action = "provider.toggle"
	ActionProviderTest           Action = "provider.test"
	ActionProviderSyncModels     Action = "provider.sync_models"

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

	ActionBudgetSet              Action = "budget.set"
	ActionBudgetDelete           Action = "budget.delete"

	ActionExpansionCreate        Action = "expansion.create"
	ActionExpansionDecision      Action = "expansion.decision"

	ActionKeyCreate              Action = "key.create"
	ActionKeyDisable             Action = "key.disable"
	ActionKeyEnable              Action = "key.enable"
	ActionKeyDelete              Action = "key.delete"
	ActionKeyReset               Action = "key.reset"
	ActionKeyToggleLog           Action = "key.toggle_log"
	ActionKeyToggleAccessAll     Action = "key.toggle_access_all"
	ActionKeyBindGroups          Action = "key.bind_groups"

	ActionUserLogin              Action = "user.login"
	ActionUserLogout             Action = "user.logout"

	ActionSettingsUpdate         Action = "settings.update"

	ActionAnnouncementPublish    Action = "announcement.publish"
	ActionAnnouncementDelete     Action = "announcement.delete"

	ActionHealthCheckRun         Action = "health_check.run"

	ActionEmployeeImport         Action = "employee.import"
)

// ── 资源类型常量 ──────────────────────────────────────────────

const (
	ResourceServiceKey  Resource = "service_key"
	ResourceProvider    Resource = "provider"
	ResourceModel       Resource = "model"
	ResourceGroup       Resource = "group"
	ResourceBudget      Resource = "budget"
	ResourceExpansion   Resource = "expansion_request"
	ResourceKey         Resource = "api_key"
	ResourceUser        Resource = "user"
	ResourceSettings    Resource = "settings"
	ResourceAnnouncement Resource = "announcement"
	ResourceHealthCheck  Resource = "health_check"
	ResourceEmployee     Resource = "employee"
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
