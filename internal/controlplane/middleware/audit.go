package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
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
	"POST:/api/v1/models/batch":             {audit.ActionModelCreate, audit.ResourceModel},
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
)

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
	if row.Name == "" {
		var devRow struct {
			Username string `gorm:"column:username"`
		}
		db.Raw(`SELECT username FROM dev_users WHERE id::text = ? LIMIT 1`, userID).Scan(&devRow)
		if devRow.Username != "" {
			row.Name = devRow.Username
		}
	}
	entry := &userIdentity{Name: row.Name, DeptID: row.DeptID, DeptName: row.DeptName, expireAt: time.Now().Add(auditCacheTTL)}
	auditUserCache.Store(userID, entry)
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
		if rqBody := sanitizeBody(bodyBytes); rqBody != "" {
			details["request_body"] = rqBody
		}
		if rpBody := sanitizeBody(bw.body); rpBody != "" {
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
			details["before"] = before
			details["after"] = after
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

// looksLikeSecret 判断字符串值是否疑似密钥（sk- 前缀、Bearer token 等）。
func looksLikeSecret(v string) bool {
	t := strings.ToLower(strings.TrimSpace(v))
	return strings.HasPrefix(t, "sk-") || strings.HasPrefix(t, "bearer ")
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
		// 字符串化的 JSON（如 credentials 字段）也要递归脱敏
		if s := strings.TrimSpace(t); strings.HasPrefix(s, "{") {
			var inner any
			if json.Unmarshal([]byte(s), &inner) == nil {
				if out, err := json.Marshal(redactValue(inner)); err == nil {
					return string(out)
				}
			}
		}
		return t
	default:
		return v
	}
}

func sanitizeBody(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if !json.Valid(data) {
		return truncateBody(string(data))
	}
	var obj any
	if err := json.Unmarshal(data, &obj); err != nil {
		return truncateBody(string(data))
	}
	out, err := json.Marshal(redactValue(obj))
	if err != nil {
		return truncateBody(string(data))
	}
	return truncateBody(string(out))
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
