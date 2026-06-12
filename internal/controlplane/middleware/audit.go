package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type routeAction struct {
	Action   audit.Action
	Resource audit.Resource
}

var auditRouteMap = map[string]routeAction{
	// ── Providers ──────────────────────────────────────────────────
	"POST:/api/v1/providers":                         {audit.ActionProviderCreate, audit.ResourceProvider},
	"PUT:/api/v1/providers/:id":                      {audit.ActionProviderUpdate, audit.ResourceProvider},
	"PATCH:/api/v1/providers/:id/status":             {audit.ActionProviderToggle, audit.ResourceProvider},
	"DELETE:/api/v1/providers/:id":                   {audit.ActionProviderDelete, audit.ResourceProvider},
	"POST:/api/v1/providers/:id/test":                {audit.ActionProviderTest, audit.ResourceProvider},
	"POST:/api/v1/providers/:id/check":               {audit.ActionHealthCheckRun, audit.ResourceHealthCheck},
	"POST:/api/v1/providers/:id/sync-models":         {audit.ActionProviderSyncModels, audit.ResourceProvider},
	"POST:/api/v1/providers/:id/sync-models/confirm": {audit.Action("provider.sync_models_confirm"), audit.ResourceProvider},
	"POST:/api/v1/providers/:id/check-models":        {audit.ActionProviderSyncModels, audit.ResourceProvider},
	"POST:/api/v1/providers/check-all":               {audit.ActionHealthCheckRun, audit.ResourceHealthCheck},

	// ── Models ─────────────────────────────────────────────────────
	"POST:/api/v1/models":                    {audit.ActionModelCreate, audit.ResourceModel},
	"PUT:/api/v1/models/:id":                 {audit.ActionModelUpdate, audit.ResourceModel},
	"DELETE:/api/v1/models/:id":              {audit.ActionModelDelete, audit.ResourceModel},
	"DELETE:/api/v1/models/batch":            {audit.ActionModelDelete, audit.ResourceModel},
	"PATCH:/api/v1/models/batch-status":      {audit.ActionModelBatchUpdateStatus, audit.ResourceModel},
	"PUT:/api/v1/models/batch-tags":          {audit.Action("model.tag.update"), audit.ResourceModel},
	"POST:/api/v1/models/from-catalog":       {audit.ActionModelCreate, audit.ResourceModel},
	"POST:/api/v1/models/batch":              {audit.ActionModelCreate, audit.ResourceModel},
	"POST:/api/v1/models/batch-from-catalog": {audit.ActionModelCreate, audit.ResourceModel},
	"POST:/api/v1/models/batch-bind-catalog": {audit.Action("model.bind_catalog"), audit.ResourceModel},
	"POST:/api/v1/models/check-all":          {audit.Action("model.check"), audit.ResourceModel},
	"POST:/api/v1/models/:id/check":          {audit.Action("model.check"), audit.ResourceModel},
	"POST:/api/v1/models/:id/reset-override": {audit.Action("model.reset_override"), audit.ResourceModel},
	"POST:/api/v1/models/:id/bind-catalog":   {audit.Action("model.bind_catalog"), audit.ResourceModel},
	"POST:/api/v1/models/:id/unbind-catalog": {audit.Action("model.unbind_catalog"), audit.ResourceModel},

	// ── Model Tags ─────────────────────────────────────────────────
	"POST:/api/v1/model-tags":              {audit.Action("tag.create"), audit.ResourceModel},
	"PUT:/api/v1/model-tags/:id":           {audit.Action("tag.update"), audit.ResourceModel},
	"DELETE:/api/v1/model-tags/:id":        {audit.Action("tag.delete"), audit.ResourceModel},
	"PUT:/api/v1/models/:id/tags":          {audit.Action("model.tag.update"), audit.ResourceModel},
	"POST:/api/v1/models/:id/tags":         {audit.Action("model.tag.update"), audit.ResourceModel},
	"POST:/api/v1/models/batch-tags/query": {audit.Action("model.tag.query"), audit.ResourceModel},

	// ── Health Check ───────────────────────────────────────────────
	"PUT:/api/v1/health-check/config": {audit.ActionSettingsUpdate, audit.ResourceSettings},
	"POST:/api/v1/health-check/run":   {audit.ActionHealthCheckRun, audit.ResourceHealthCheck},

	// ── Keys ───────────────────────────────────────────────────────
	"POST:/api/v1/keys":                           {audit.ActionKeyCreate, audit.ResourceKey},
	"PATCH:/api/v1/keys/:id/disable":              {audit.ActionKeyDisable, audit.ResourceKey},
	"PATCH:/api/v1/keys/:id/enable":               {audit.ActionKeyEnable, audit.ResourceKey},
	"DELETE:/api/v1/keys/:id":                     {audit.ActionKeyDelete, audit.ResourceKey},
	"POST:/api/v1/keys/:id/reset":                 {audit.ActionKeyReset, audit.ResourceKey},
	"POST:/api/v1/keys/:id/reveal":                {audit.Action("key.reveal"), audit.ResourceKey},
	"POST:/api/v1/keys/:id/groups":                {audit.ActionKeyBindGroups, audit.ResourceKey},
	"POST:/api/v1/keys/:id/access-all-groups":     {audit.ActionKeyToggleAccessAll, audit.ResourceKey},
	"PATCH:/api/v1/keys/:id/log-enabled":          {audit.ActionKeyToggleLog, audit.ResourceKey},
	"POST:/api/v1/keys/:id/opencode-config/token": {audit.Action("key.opencode_config"), audit.ResourceKey},

	// ── Catalog ────────────────────────────────────────────────────
	"POST:/api/v1/model-catalog/sync":       {audit.Action("catalog.sync"), audit.ResourceModel},
	"POST:/api/v1/model-catalog/apply-diff": {audit.Action("catalog.apply_diff"), audit.ResourceModel},
	"POST:/api/v1/model-catalog/auto-bind":  {audit.Action("catalog.auto_bind"), audit.ResourceModel},

	// ── Groups ─────────────────────────────────────────────────────
	"POST:/api/v1/groups/check-all":               {audit.Action("group.check"), audit.ResourceGroup},
	"POST:/api/v1/groups":                         {audit.ActionGroupCreate, audit.ResourceGroup},
	"PUT:/api/v1/groups/:id":                      {audit.ActionGroupUpdate, audit.ResourceGroup},
	"DELETE:/api/v1/groups/:id":                   {audit.ActionGroupDelete, audit.ResourceGroup},
	"POST:/api/v1/groups/:id/models":              {audit.ActionGroupAddModels, audit.ResourceGroup},
	"DELETE:/api/v1/groups/:id/models/:model_id":  {audit.ActionGroupRemoveModel, audit.ResourceGroup},
	"POST:/api/v1/groups/:id/check":               {audit.Action("group.check"), audit.ResourceGroup},
	"PUT:/api/v1/groups/:id/open-mode":            {audit.ActionGroupEntitlementGrant, audit.ResourceGroup},
	"POST:/api/v1/groups/:id/entitlements":        {audit.ActionGroupEntitlementGrant, audit.ResourceGroup},
	"DELETE:/api/v1/groups/:id/entitlements/:eid": {audit.ActionGroupEntitlementRevoke, audit.ResourceGroup},
	"PUT:/api/v1/groups/:id/budget-tier":          {audit.ActionModelTierUpdate, audit.ResourceGroup},
	"PUT:/api/v1/groups/budget-tiers":             {audit.ActionModelTierUpdate, audit.ResourceGroup},

	// ── Settings ───────────────────────────────────────────────────
	"PUT:/api/v1/settings":               {audit.ActionSettingsUpdate, audit.ResourceSettings},
	"POST:/api/v1/settings/test-webhook": {audit.Action("settings.test_webhook"), audit.ResourceSettings},
	"POST:/api/v1/settings/test-email":   {audit.ActionEmailTest, audit.ResourceSettings},
	"POST:/api/v1/settings/export":       {audit.Action("settings.export"), audit.ResourceSettings},
	"POST:/api/v1/settings/import":       {audit.Action("settings.import"), audit.ResourceSettings},

	// ── Announcements ──────────────────────────────────────────────
	"POST:/api/v1/announcements":              {audit.Action("announcement.create"), audit.ResourceAnnouncement},
	"PUT:/api/v1/announcements/:id":           {audit.Action("announcement.update"), audit.ResourceAnnouncement},
	"DELETE:/api/v1/announcements/:id":        {audit.ActionAnnouncementDelete, audit.ResourceAnnouncement},
	"PATCH:/api/v1/announcements/:id/publish": {audit.ActionAnnouncementPublish, audit.ResourceAnnouncement},
	"PATCH:/api/v1/announcements/:id/archive": {audit.Action("announcement.archive"), audit.ResourceAnnouncement},
	"POST:/api/v1/announcements/:id/read":     {audit.Action("announcement.read"), audit.ResourceAnnouncement},

	// ── Notifications ──────────────────────────────────────────────
	"POST:/api/v1/notifications/:id/read": {audit.Action("notification.read"), audit.ResourceUser},
	"POST:/api/v1/notifications/read-all": {audit.Action("notification.read_all"), audit.ResourceUser},

	// ── Service Keys ───────────────────────────────────────────────
	"POST:/api/v1/service-keys":                   {audit.ActionServiceKeyCreate, audit.ResourceServiceKey},
	"PATCH:/api/v1/service-keys/:id/status":       {audit.ActionServiceKeyToggle, audit.ResourceServiceKey},
	"PATCH:/api/v1/service-keys/:id/ip-allowlist": {audit.ActionServiceKeyIPChanged, audit.ResourceServiceKey},
	"DELETE:/api/v1/service-keys/:id":             {audit.ActionServiceKeyDelete, audit.ResourceServiceKey},

	// ── Budgets ────────────────────────────────────────────────────
	"PUT:/api/v1/budgets":                            {audit.ActionBudgetSet, audit.ResourceBudget},
	"PUT:/api/v1/budgets/batch":                      {audit.ActionBudgetSet, audit.ResourceBudget},
	"DELETE:/api/v1/budgets":                         {audit.ActionBudgetDelete, audit.ResourceBudget},
	"POST:/api/v1/budget/rebuild-counters":           {audit.ActionBudgetSet, audit.ResourceBudget},
	"PUT:/api/v1/budgets/:allocation_id/tier-limits": {audit.ActionBudgetTierLimitSet, audit.ResourceBudget},
	"POST:/api/v1/budgets/tier-limits/batch/apply":   {audit.ActionBudgetTierLimitSet, audit.ResourceBudget},
	"POST:/api/v1/model-tiers":                       {audit.ActionModelTierCreate, audit.ResourceModelTier},
	"PUT:/api/v1/model-tiers/:id":                    {audit.ActionModelTierUpdate, audit.ResourceModelTier},
	"PATCH:/api/v1/model-tiers/:id/status":           {audit.ActionModelTierStatus, audit.ResourceModelTier},
	"PATCH:/api/v1/model-tiers/:id/archive":          {audit.ActionModelTierArchive, audit.ResourceModelTier},
	"DELETE:/api/v1/model-tiers/:id":                 {audit.ActionModelTierDelete, audit.ResourceModelTier},
	"PUT:/api/v1/model-tiers/reorder":                {audit.ActionModelTierReorder, audit.ResourceModelTier},

	// ── Expansion Requests ─────────────────────────────────────────
	"POST:/api/v1/expansion-requests":                {audit.ActionExpansionCreate, audit.ResourceExpansion},
	"POST:/api/v1/expansion-requests/precheck":       {audit.ActionExpansionPrecheck, audit.ResourceExpansion},
	"POST:/api/v1/expansion-requests/notify-owner":   {audit.ActionExpansionNotifyOwner, audit.ResourceExpansion},
	"POST:/api/v1/expansion-requests/:id/reprecheck": {audit.ActionExpansionReprecheck, audit.ResourceExpansion},
	"POST:/api/v1/expansion-requests/:id/decision":   {audit.ActionExpansionDecision, audit.ResourceExpansion},
	"POST:/api/v1/expansion-requests/batch-decision": {audit.ActionExpansionDecision, audit.ResourceExpansion},

	// ── Org: Studios ───────────────────────────────────────────────
	"POST:/api/v1/org/studios":       {audit.Action("studio.create"), audit.Resource("studio")},
	"DELETE:/api/v1/org/studios/:id": {audit.Action("studio.delete"), audit.Resource("studio")},

	// ── Org: Departments ───────────────────────────────────────────
	"PUT:/api/v1/org/departments/:id/studio":  {audit.Action("department.set_studio"), audit.Resource("department")},
	"POST:/api/v1/org/dept-studio-map/import": {audit.Action("department.import_mapping"), audit.Resource("department")},

	// ── Org: Roles ─────────────────────────────────────────────────
	"POST:/api/v1/org/roles/grant":  {audit.Action("role.grant"), audit.Resource("role")},
	"POST:/api/v1/org/roles/revoke": {audit.Action("role.revoke"), audit.Resource("role")},

	// ── Org: Employees ─────────────────────────────────────────────
	"POST:/api/v1/org/employees/import":               {audit.ActionEmployeeImport, audit.ResourceEmployee},
	"PUT:/api/v1/org/users/:id/department":            {audit.Action("user.change_department"), audit.ResourceUser},
	"PUT:/api/v1/org/users/:id/department/follow-sso": {audit.Action("user.follow_sso_department"), audit.ResourceUser},

	// ── Service Keys（补 2 条未手动埋点的写操作）───────────────────
	"POST:/api/v1/service-keys/:id/groups":            {audit.Action("service_key.update_groups"), audit.ResourceServiceKey},
	"POST:/api/v1/service-keys/:id/access-all-groups": {audit.Action("service_key.toggle_access_all"), audit.ResourceServiceKey},

	// ── Quick Onboard ──────────────────────────────────────────────
	"POST:/api/v1/providers/quick-onboard/preview": {audit.Action("provider.quick_onboard_preview"), audit.ResourceProvider},
	"POST:/api/v1/providers/quick-onboard":         {audit.Action("provider.quick_onboard"), audit.ResourceProvider},

	// ── Group capability reset ─────────────────────────────────────
	"POST:/api/v1/groups/:id/reset-capability": {audit.Action("group.reset_capability"), audit.ResourceGroup},

	// ── Expansion（PATCH 更新）─────────────────────────────────────
	"PATCH:/api/v1/expansion-requests/:id": {audit.Action("expansion.update"), audit.ResourceExpansion},

	// ── Key Requests ───────────────────────────────────────────────
	"POST:/api/v1/key-requests":              {audit.Action("key_request.create"), audit.Resource("key_request")},
	"POST:/api/v1/key-requests/:id/cancel":   {audit.Action("key_request.cancel"), audit.Resource("key_request")},
	"POST:/api/v1/key-requests/:id/decision": {audit.Action("key_request.decision"), audit.Resource("key_request")},

	// ── Dept Policies ──────────────────────────────────────────────
	"PUT:/api/v1/dept-policies/:dept_id/key-creation": {audit.Action("dept_policy.update_key_creation"), audit.Resource("dept_policy")},

	// ── Budgets（批量/多周期）──────────────────────────────────────
	"PUT:/api/v1/budgets/multi-period":               {audit.ActionBudgetSet, audit.ResourceBudget},
	"POST:/api/v1/budgets/batch/preview":             {audit.ActionBudgetAdjustPreview, audit.ResourceBudget},
	"POST:/api/v1/budgets/batch/apply":               {audit.ActionBudgetAdjustApply, audit.ResourceBudget},
	"POST:/api/v1/budgets/tier-limits/batch/preview": {audit.ActionBudgetAdjustPreview, audit.ResourceBudget},

	// ── Channel Monitors ───────────────────────────────────────────
	"POST:/api/v1/channel-monitors":                {audit.Action("channel_monitor.create"), audit.Resource("channel_monitor")},
	"PUT:/api/v1/channel-monitors/:id":             {audit.Action("channel_monitor.update"), audit.Resource("channel_monitor")},
	"DELETE:/api/v1/channel-monitors/:id":          {audit.Action("channel_monitor.delete"), audit.Resource("channel_monitor")},
	"POST:/api/v1/channel-monitors/:id/run":        {audit.Action("channel_monitor.run"), audit.Resource("channel_monitor")},
	"POST:/api/v1/channel-monitor-templates":       {audit.Action("channel_monitor_template.create"), audit.Resource("channel_monitor")},
	"PUT:/api/v1/channel-monitor-templates/:id":    {audit.Action("channel_monitor_template.update"), audit.Resource("channel_monitor")},
	"DELETE:/api/v1/channel-monitor-templates/:id": {audit.Action("channel_monitor_template.delete"), audit.Resource("channel_monitor")},

	// ── Ops（运维看板写操作）───────────────────────────────────────
	"POST:/api/v1/ops/alert-rules":                      {audit.Action("ops.alert_rule.create"), audit.Resource("ops")},
	"PUT:/api/v1/ops/alert-rules/:id":                   {audit.Action("ops.alert_rule.update"), audit.Resource("ops")},
	"DELETE:/api/v1/ops/alert-rules/:id":                {audit.Action("ops.alert_rule.delete"), audit.Resource("ops")},
	"PUT:/api/v1/ops/alert-events/:id/status":           {audit.Action("ops.alert_event.update_status"), audit.Resource("ops")},
	"POST:/api/v1/ops/alert-silences":                   {audit.Action("ops.alert_silence.create"), audit.Resource("ops")},
	"POST:/api/v1/ops/system-logs/cleanup":              {audit.Action("ops.system_log.cleanup"), audit.Resource("ops")},
	"PUT:/api/v1/ops/runtime/logging":                   {audit.Action("ops.runtime_logging.update"), audit.Resource("ops")},
	"POST:/api/v1/ops/runtime/logging/reset":            {audit.Action("ops.runtime_logging.reset"), audit.Resource("ops")},
	"PUT:/api/v1/ops/settings/metric-thresholds":        {audit.Action("ops.settings.update_metric_thresholds"), audit.Resource("ops")},
	"PUT:/api/v1/ops/settings/dashboard":                {audit.Action("ops.settings.update_dashboard"), audit.Resource("ops")},
	"PUT:/api/v1/ops/settings/advanced":                 {audit.Action("ops.settings.update_advanced"), audit.Resource("ops")},
	"PUT:/api/v1/ops/settings/email-notification":       {audit.Action("ops.settings.update_email"), audit.Resource("ops")},
	"POST:/api/v1/ops/settings/email-notification/test": {audit.Action("ops.settings.test_email"), audit.Resource("ops")},
	"PUT:/api/v1/ops/settings/alert-runtime":            {audit.Action("ops.settings.update_alert_runtime"), audit.Resource("ops")},
	"PUT:/api/v1/ops/error-logs/:id/resolve":            {audit.Action("ops.error_log.resolve"), audit.Resource("ops")},
	"PUT:/api/v1/ops/request-errors/:id/resolve":        {audit.Action("ops.request_error.resolve"), audit.Resource("ops")},
	"PUT:/api/v1/ops/upstream-errors/:id/resolve":       {audit.Action("ops.upstream_error.resolve"), audit.Resource("ops")},
}

func lookupRouteAction(fullPath, method string) (action audit.Action, resource audit.Resource, ok bool) {
	if fullPath == "" {
		return "", "", false
	}
	key := method + ":" + fullPath
	if entry, found := auditRouteMap[key]; found {
		return entry.Action, entry.Resource, true
	}
	return "", "", false
}

type userIdentity struct {
	Name     string
	DeptID   string
	DeptName string
	expireAt time.Time
}

var (
	auditUserCache sync.Map
	auditCacheTTL  = 5 * time.Minute

	// auditUserCacheCount 记录缓存条目数（含 TTL 过期但仍未删除的条目），
	// 用于在超过上限时整体清空，防止长期运行内存无界增长。
	auditUserCacheCount atomic.Int64
)

// auditUserCacheMaxEntries 审计用户缓存上限，越阈时整体清空（冷启动重建）。
const auditUserCacheMaxEntries = 10000

// bumpAuditUserCache 在每次 Store 后递增计数，越阈则清空整个缓存并重置计数。
// sync.Map 本身并发安全，计数用 atomic；清空为 O(n)，仅在越阈时触发。
func bumpAuditUserCache() {
	if auditUserCacheCount.Add(1) > auditUserCacheMaxEntries {
		auditUserCache.Range(func(k, _ any) bool {
			auditUserCache.Delete(k)
			return true
		})
		auditUserCacheCount.Store(0)
	}
}

func lookupUserIdentity(db *gorm.DB, userID string) (name, deptID, deptName string) {
	if db == nil || userID == "" {
		return "", "", ""
	}
	if v, ok := auditUserCache.Load(userID); ok {
		id := v.(*userIdentity)
		if time.Now().Before(id.expireAt) {
			return id.Name, id.DeptID, id.DeptName
		}
	}

	var row struct {
		Name     string `gorm:"column:name"`
		DeptID   string `gorm:"column:dept_id"`
		DeptName string `gorm:"column:dept_name"`
	}

	// 优先用 UUID 主键直接比较（走主键索引）；非 UUID 的 userID 回退 ::text 字符串比较。
	uid, uidOK := uuid.Parse(userID)
	if uidOK == nil {
		db.Raw(`
			SELECT
				COALESCE(u.name, '') AS name,
				COALESCE(d.dept_id, '') AS dept_id,
				COALESCE(d.dept_name, '') AS dept_name
			FROM users u
			LEFT JOIN departments d ON d.id = u.dept_id
			WHERE u.id = ?
			LIMIT 1
		`, uid).Scan(&row)
	} else {
		db.Raw(`
			SELECT
				COALESCE(u.name, '') AS name,
				COALESCE(d.dept_id, '') AS dept_id,
				COALESCE(d.dept_name, '') AS dept_name
			FROM users u
			LEFT JOIN departments d ON d.id = u.dept_id
			WHERE u.id::text = ?
			LIMIT 1
		`, userID).Scan(&row)
	}

	if row.Name == "" {
		var devRow struct {
			Username string `gorm:"column:username"`
		}
		if uidOK == nil {
			db.Raw(`SELECT username FROM dev_users WHERE id = ? LIMIT 1`, uid).Scan(&devRow)
		} else {
			db.Raw(`SELECT username FROM dev_users WHERE id::text = ? LIMIT 1`, userID).Scan(&devRow)
		}
		if devRow.Username != "" {
			row.Name = devRow.Username
		}
	}

	entry := &userIdentity{Name: row.Name, DeptID: row.DeptID, DeptName: row.DeptName, expireAt: time.Now().Add(auditCacheTTL)}
	auditUserCache.Store(userID, entry)
	bumpAuditUserCache()
	return row.Name, row.DeptID, row.DeptName
}

// AuditMiddleware 全局审计中间件
// 挂在 adminGroup 上，记录所有写操作（POST/PATCH/PUT/DELETE）的审计日志。
// 依赖 audit.Recorder 接口，不与具体 service 实现耦合。
// 若 handler 已通过 audit.MarkRecorded(c) 标记手动记录过，则跳过自动记录。
// 可选传入 db，用于 JWT 中缺失 user_name 时从数据库补齐。
func AuditMiddleware(recorder audit.Recorder, db ...*gorm.DB) gin.HandlerFunc {
	var dbo *gorm.DB
	if len(db) > 0 {
		dbo = db[0]
	}
	return func(c *gin.Context) {
		if !isWriteMethod(c.Request.Method) {
			c.Next()
			return
		}

		// 捕获请求体：只取前 64KB 用于审计，再用 MultiReader 把剩余流接回，
		// 保证下游 handler 读到完整 body（修复 >64KB 请求被截断导致的功能不可用）。
		var bodyBytes []byte
		if c.Request.Body != nil && c.Request.ContentLength != 0 {
			bodyBytes, _ = io.ReadAll(io.LimitReader(c.Request.Body, 64*1024))
			c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bodyBytes), c.Request.Body))
		}
		startTime := time.Now()

		// 包装 ResponseWriter 捕获响应体
		bw := &responseBodyWriter{ResponseWriter: c.Writer}
		c.Writer = bw

		c.Next()

		duration := time.Since(startTime)

		if audit.IsRecorded(c) {
			return
		}

		path := c.Request.URL.Path
		method := c.Request.Method
		statusCode := c.Writer.Status()
		resourceID := inferResourceID(c)

		var action audit.Action
		var resourceType audit.Resource

		if a, r, ok := lookupRouteAction(c.FullPath(), method); ok {
			action = a
			resourceType = r
		} else {
			action = inferAction(method, path)
			resourceType = audit.Resource(inferResourceType(path))
		}
		if override, ok := audit.GetActionOverride(c); ok {
			action = override
		}

		if action == "" {
			action = audit.Action(method + ":" + c.FullPath())
		}
		if resourceType == "" {
			resourceType = audit.ResourceUnknown
		}

		userIDStr := c.GetString("user_id")
		var userID uuid.UUID
		if userIDStr != "" {
			if uid, err := uuid.Parse(userIDStr); err == nil {
				userID = uid
			}
		}

		userName := c.GetString("user_name")
		deptID := c.GetString("dept_id")
		deptName := c.GetString("dept_name")

		if userName == "" && userIDStr != "" && dbo != nil {
			userName, deptID, deptName = lookupUserIdentity(dbo, userIDStr)
		}

		// 构建详细审计数据
		details := map[string]any{
			"method":       method,
			"path":         path,
			"full_route":   c.FullPath(),
			"duration_ms":  duration.Milliseconds(),
			"status_code":  statusCode,
			"content_type": strings.TrimRight(c.GetHeader("Content-Type"), "; charset=utf-8"),
		}
		if reqID := c.GetString("request_id"); reqID != "" {
			details["request_id"] = reqID
		}
		if ref := c.GetHeader("Referer"); ref != "" {
			details["referer"] = ref
		}
		if ct := c.GetHeader("Content-Type"); ct != "" {
			details["content_type"] = ct
		}
		if rqBody := sanitizeBody(bodyBytes, c.GetHeader("Content-Type")); rqBody != "" {
			details["request_body"] = rqBody
		}
		if rpBody := sanitizeBody(bw.body, c.Writer.Header().Get("Content-Type")); rpBody != "" {
			details["response_body"] = rpBody
		}
		if len(bodyBytes) > 0 && json.Valid(bodyBytes) {
			var request map[string]any
			if json.Unmarshal(bodyBytes, &request) == nil {
				if reason, ok := request["reason"].(string); ok {
					details["reason"] = reason
				}
			}
		}
		if reason := audit.GetReason(c); reason != "" {
			details["reason"] = reason
		}
		if before, after := audit.GetBeforeAfter(c); before != nil || after != nil {
			if r := redactJSONValue(before); r != nil {
				details["before"] = r
			}
			if r := redactJSONValue(after); r != nil {
				details["after"] = r
			}
		}
		if scopeType, scopeID := audit.GetScope(c); scopeType != "" || scopeID != "" {
			details["scope_type"] = scopeType
			details["scope_id"] = scopeID
		}

		reqHdrs := make(map[string]string)
		for _, key := range []string{"Referer", "Content-Type", "Origin", "Accept-Language"} {
			if v := c.GetHeader(key); v != "" {
				reqHdrs[key] = v
			}
		}
		if len(reqHdrs) > 0 {
			details["request_headers"] = reqHdrs
		}

		resourceName := audit.GetResourceNameCtx(c)
		if resourceName == "" {
			resourceName = inferResourceName(resourceType, bodyBytes, bw.body, statusCode)
		}

		recorder.Record(audit.Input{
			UserID:       userID,
			UserName:     userName,
			Action:       action,
			ResourceType: resourceType,
			ResourceID:   resourceID,
			ResourceName: resourceName,
			DeptID:       deptID,
			DeptName:     deptName,
			ClientIP:     extractClientIP(c),
			UserAgent:    c.GetHeader("User-Agent"),
			StatusCode:   statusCode,
			Details:      details,
		})
	}
}

// inferResourceName 从请求体/响应体 JSON 推断资源名称
var nameFields = []string{"name", "title", "username"}

func inferResourceName(resourceType audit.Resource, reqBody, respBody []byte, statusCode int) string {
	for _, body := range [][]byte{reqBody, respBody} {
		if len(body) == 0 || !json.Valid(body) {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil {
			continue
		}
		for _, field := range nameFields {
			if v, ok := obj[field]; ok {
				if s, ok := v.(string); ok && s != "" {
					if len(s) > 128 {
						return s[:128]
					}
					return s
				}
			}
		}
	}
	// 响应体含 data 对象的嵌套结构（如 create 返回）
	if statusCode >= 200 && statusCode < 300 && len(respBody) > 0 && json.Valid(respBody) {
		var wrapper map[string]any
		if err := json.Unmarshal(respBody, &wrapper); err == nil {
			if data, ok := wrapper["data"].(map[string]any); ok {
				for _, field := range nameFields {
					if v, ok := data[field]; ok {
						if s, ok := v.(string); ok && s != "" {
							if len(s) > 128 {
								return s[:128]
							}
							return s
						}
					}
				}
			}
		}
	}
	return ""
}

func isWriteMethod(method string) bool {
	switch method {
	case "POST", "PATCH", "PUT", "DELETE":
		return true
	}
	return false
}

// responseBodyWriter 包装响应写入器，捕获响应体用于审计
type responseBodyWriter struct {
	gin.ResponseWriter
	body []byte
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	if len(w.body) < 8*1024 {
		w.body = append(w.body, b...)
	}
	return w.ResponseWriter.Write(b)
}

// sanitizeBody 对请求/响应体进行脱敏处理。
// 脱敏原则「宁多勿少」：宁可多隐藏字段，也不能泄漏密钥。
var sensitiveKeys = []string{
	"password", "passwd", "secret", "token",
	"authorization", "cookie",
	"api_key", "apikey", "credential", "key",
}

// isSensitiveKey 判断 key 名是否命中敏感子串（大小写不敏感）。
func isSensitiveKey(k string) bool {
	kl := strings.ToLower(k)
	for _, sk := range sensitiveKeys {
		if strings.Contains(kl, sk) {
			return true
		}
	}
	return false
}

// isSensitiveQueryKey 判断 URL 查询串/表单参数名是否命中敏感子串。
// 相比 isSensitiveKey 额外覆盖签名字段（sig），避免把 "sig" 加进全局黑名单
// 导致 "design"/"assign" 等普通字段被误杀。
func isSensitiveQueryKey(k string) bool {
	kl := strings.ToLower(k)
	for _, sk := range []string{
		"token", "password", "passwd", "secret", "key",
		"sig", "credential", "authorization", "api_key", "apikey",
	} {
		if strings.Contains(kl, sk) {
			return true
		}
	}
	return false
}

// urlUserinfoRe 匹配 URL 中的 scheme://user:pass@ 段，用于掩码内嵌密码。
var urlUserinfoRe = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^/@\s]*):([^/@\s]*)@`)

// querySecretRe 匹配 URL 查询串中的 key=value 段（分隔符、参数名、参数值）。
var querySecretRe = regexp.MustCompile(`(?i)([?&])([^=&#\s]+)=([^&#\s]*)`)

// maskURLCredentials 掩码 URL/连接串中内嵌的凭证：
//  1. scheme://user:pass@host 中的密码段；
//  2. 查询串中敏感参数（token/password/secret/key/sig 等）的值。
//
// 返回掩码后的字符串及是否发生替换（幂等，已掩码的内容不会再次变化）。
func maskURLCredentials(s string) (string, bool) {
	if s == "" {
		return s, false
	}
	out := urlUserinfoRe.ReplaceAllString(s, "${1}:***@")
	out = querySecretRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := querySecretRe.FindStringSubmatch(m)
		if len(sub) != 4 {
			return m
		}
		if isSensitiveQueryKey(sub[2]) {
			return sub[1] + sub[2] + "=***"
		}
		return m
	})
	return out, out != s
}

// looksLikeSecret 判断字符串值是否疑似密钥（sk-/Bearer 前缀、JWT、AWS 密钥等）。
func looksLikeSecret(v string) bool {
	t := strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(t, "sk-") || strings.HasPrefix(t, "bearer ") {
		return true
	}
	// JWT：三段 base64url，首段 header 固定以 eyJ 开头
	if strings.HasPrefix(t, "eyj") {
		return true
	}
	// AWS Access Key ID：AKIA / ASIA 前缀
	if strings.HasPrefix(t, "akia") || strings.HasPrefix(t, "asia") {
		return true
	}
	return false
}

// redactJSONValue 对任意值做「JSON 归一化 + 脱敏」：
// json.Marshal → json.Unmarshal 到 any → redactValue，返回脱敏后的值。
// 用于 handler 注入的 before/after 等未经 sanitizeBody 处理的结构化对象。
// nil 或无法归一化的值原样回退（保证不 panic）。
func redactJSONValue(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return redactValue(v)
	}
	var normalized any
	if err := json.Unmarshal(b, &normalized); err != nil {
		return redactValue(v)
	}
	return redactValue(normalized)
}

// redactValue 递归脱敏：遍历 map/切片，命中敏感 key 名或疑似密钥值时替换为 "***"。
func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if isSensitiveKey(k) {
				out[k] = "***"
				continue
			}
			out[k] = redactValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactValue(val)
		}
		return out
	case string:
		if looksLikeSecret(t) {
			return "***"
		}
		// 字符串化的 JSON（对象/数组，如 credentials 字段）也要递归脱敏
		if s := strings.TrimSpace(t); strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
			var inner any
			if json.Unmarshal([]byte(s), &inner) == nil {
				if out, err := json.Marshal(redactValue(inner)); err == nil {
					return string(out)
				}
			}
		}
		// URL/连接串内嵌凭证掩码（user:pass@ 与敏感查询参数）
		if masked, ok := maskURLCredentials(t); ok {
			return masked
		}
		return t
	default:
		return v
	}
}

// sanitizeBody 对请求/响应体进行脱敏处理。
// 脱敏原则「宁多勿少」：宁可多隐藏字段，也不能泄漏密钥。
func sanitizeBody(data []byte, contentType string) string {
	if len(data) == 0 {
		return ""
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))

	// multipart/文件上传：只存大小占位，绝不落原始内容（含文件内容与 boundary）。
	if strings.HasPrefix(ct, "multipart/") {
		return fmt.Sprintf("<multipart body, %d bytes>", len(data))
	}

	if !json.Valid(data) {
		// application/x-www-form-urlencoded：解析后对敏感字段脱敏，再重新编码。
		if strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
			return sanitizeFormBody(data)
		}
		// 其它非 JSON 体（纯文本/未知类型）：不落原始内容，只存元数据占位。
		return fmt.Sprintf("<non-json body, %d bytes>", len(data))
	}

	var obj any
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Sprintf("<non-json body, %d bytes>", len(data))
	}
	out, err := json.Marshal(redactValue(obj))
	if err != nil {
		return fmt.Sprintf("<non-json body, %d bytes>", len(data))
	}
	return truncateBody(string(out))
}

// sanitizeFormBody 对 application/x-www-form-urlencoded 体做字段级脱敏：
// 敏感 key 的字段值置 "***"，非敏感字段的值再做一次 URL 内嵌凭证掩码。
// 解析失败时不落原始内容，返回占位。
func sanitizeFormBody(data []byte) string {
	vals, err := url.ParseQuery(string(data))
	if err != nil {
		return fmt.Sprintf("<non-json body, %d bytes>", len(data))
	}
	for k, vv := range vals {
		for i := range vv {
			if isSensitiveKey(k) {
				vv[i] = "***"
				continue
			}
			if masked, ok := maskURLCredentials(vv[i]); ok {
				vv[i] = masked
			}
		}
	}
	return truncateBody(vals.Encode())
}

// truncateBody 截断审计体到 2048 字节，超长追加 "..."。
func truncateBody(s string) string {
	if len(s) > 2048 {
		return s[:2048] + "..."
	}
	return s
}

func extractClientIP(c *gin.Context) string {
	// 校验 IP 格式，拒绝非 IP 的伪造值；仍存在「伪造合法 IP」风险，彻底修复需配置 gin.SetTrustedProxies。
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		if ip := firstValidIP(xff); ip != "" {
			return ip
		}
	}
	if xri := strings.TrimSpace(c.GetHeader("X-Real-IP")); xri != "" && net.ParseIP(xri) != nil {
		return xri
	}
	return c.ClientIP()
}

// firstValidIP 返回 X-Forwarded-For 中第一个合法 IP，跳过非法段。
func firstValidIP(xff string) string {
	for _, part := range strings.Split(xff, ",") {
		if ip := strings.TrimSpace(part); net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

func inferAction(method, path string) audit.Action {
	if !isWriteMethod(method) {
		return ""
	}

	segments := splitPath(path)

	resourceStart := -1
	for i := 0; i < len(segments)-1; i++ {
		if (segments[i] == "api" || segments[i] == "internal") &&
			segments[i+1] == "v1" &&
			i+2 < len(segments) {
			resourceStart = i + 2
			break
		}
	}
	if resourceStart < 0 || resourceStart >= len(segments) {
		return ""
	}

	resourceSegs := segments[resourceStart:]
	baseResource := singularize(segments[resourceStart])

	rest := resourceSegs[1:]

	subResource := ""
	if len(rest) == 1 {
		if isIDSegment(rest[0]) {
		} else {
			subResource = singularize(rest[0])
		}
	} else if len(rest) >= 2 {
		if isIDSegment(rest[0]) && !isIDSegment(rest[1]) {
			subResource = singularize(rest[1])
		}
	}

	verb := methodToVerb(method)
	if subResource != "" {
		return audit.Action(baseResource + "." + subResource + "." + verb)
	}
	return audit.Action(baseResource + "." + verb)
}

func methodToVerb(method string) string {
	switch method {
	case "POST":
		return "create"
	case "PATCH":
		return "update"
	case "PUT":
		return "update"
	case "DELETE":
		return "delete"
	}
	return strings.ToLower(method)
}

func singularize(s string) string {
	if len(s) < 2 {
		return s
	}
	last2 := s[len(s)-2:]
	if last2 == "ss" || last2 == "us" || last2 == "is" {
		return s
	}
	if s[len(s)-1] == 's' {
		return s[:len(s)-1]
	}
	return s
}

func inferResourceType(path string) string {
	segments := splitPath(path)
	for i, seg := range segments {
		if (seg == "api" || seg == "internal") && i+1 < len(segments) {
			if segments[i+1] == "v1" && i+2 < len(segments) {
				return segments[i+2]
			}
		}
	}
	return ""
}

func inferResourceID(c *gin.Context) string {
	// 依次尝试常见资源 ID 参数；补 allocation_id/dept_id/user_id，避免 budget tier-limit 等落库 resource_id 为空。
	for _, key := range []string{"id", "model_id", "eid", "allocation_id", "dept_id", "user_id"} {
		if v := c.Param(key); v != "" {
			return v
		}
	}
	return ""
}

func splitPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func isIDSegment(s string) bool {
	if _, err := uuid.Parse(s); err == nil {
		return true
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
