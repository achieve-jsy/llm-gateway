//go:build integration

// TDD-6: 控制面 HTTP API 集成测试（针对 v0.7 额度权限相关接口）。
//
// 运行条件：
//   - 控制面进程已启动（http://127.0.0.1:8081）
//   - 网关进程已启动（http://127.0.0.1:8080）
//   - Postgres 已跑通（192.0.2.10:5432）
//   - Redis 已跑通（192.0.2.10:6379）
//
// 运行方式：
//   go test ./tests/integration/... -tags=integration -v -timeout 60s

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── 配置从环境变量 ──

func cpURL() string {
	if u := os.Getenv("CP_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:8081"
}

func gwURL() string {
	if u := os.Getenv("GW_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:8080"
}

func jwtSecret() string {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return s
	}
	// 默认：dev.yaml 里的 dev secret
	return "d8f7a3b2c1e9f4567890abc123def456"
}

func internalToken() string {
	if s := os.Getenv("INTERNAL_TOKEN"); s != "" {
		return s
	}
	return "dev-test-token"
}

// ── 测试 helpers ──

// makeAdminJWT 生成带指定 dept/studio 的 admin JWT
func makeAdminJWT(t *testing.T, userID, deptID, studioID string) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":         "llm-gateway",
		"iat":         now.Unix(),
		"exp":         now.Add(time.Hour).Unix(),
		"user_id":     userID,
		"email":       "tdd-admin@test.local",
		"name":        "TDD Admin",
		"passport":    "",
		"dept_id":     deptID,
		"dept_name":   "TDD Dept",
		"studio_id":   studioID,
		"studio_name": "TDD Studio",
		"role":        "admin",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(jwtSecret()))
	require.NoError(t, err)
	return signed
}

type apiResponse struct {
	Status int
	Body   map[string]any
	Raw    string
}

func doRequest(t *testing.T, method, url string, headers map[string]string, body any) apiResponse {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reqBody)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "HTTP 请求应成功: %s %s", method, url)
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	return apiResponse{Status: resp.StatusCode, Body: parsed, Raw: string(raw)}
}

func authHeader(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// ── TDD-6: Budget API ──

func TestTDD6_BudgetSet_AndMyBudget(t *testing.T) {
	// 场景：admin PUT 设置 dept_daily/studio 预算，然后 GET /api/v1/my/budget 验证
	// 用唯一 dept_id 避免污染
	uniqueDept := fmt.Sprintf("tdd6-dept-%d", time.Now().UnixNano()%1000000)
	uniqueStudio := fmt.Sprintf("tdd6-studio-%d", time.Now().UnixNano()%1000000)
	token := makeAdminJWT(t, "tdd6-user", uniqueDept, uniqueStudio)

	// 1. PUT /api/v1/budgets 设置 dept_daily
	r1 := doRequest(t, "PUT", cpURL()+"/api/v1/budgets", authHeader(token), map[string]any{
		"scope_type": "dept_daily",
		"scope_id":   uniqueDept,
		"amount_usd": 123.45,
	})
	assert.Equal(t, http.StatusOK, r1.Status, "PUT dept_daily 应成功: %s", r1.Raw)
	assert.Equal(t, true, r1.Body["ok"])

	// 2. PUT /api/v1/budgets 设置 studio
	r2 := doRequest(t, "PUT", cpURL()+"/api/v1/budgets", authHeader(token), map[string]any{
		"scope_type": "studio",
		"scope_id":   uniqueStudio,
		"amount_usd": 9999.99,
	})
	assert.Equal(t, http.StatusOK, r2.Status, "PUT studio 应成功: %s", r2.Raw)

	// 3. GET /api/v1/my/budget 应能看到 dept_daily + studio 两层
	r3 := doRequest(t, "GET", cpURL()+"/api/v1/my/budget", authHeader(token), nil)
	assert.Equal(t, http.StatusOK, r3.Status)

	chain, ok := r3.Body["chain"].([]any)
	require.True(t, ok, "chain 应为数组")

	// 应至少包含 dept_daily 和 studio
	foundDeptDaily := false
	foundStudio := false
	for _, item := range chain {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		st, _ := m["scope_type"].(string)
		amountUSD, _ := m["amount_usd"].(float64)
		switch st {
		case "dept_daily":
			foundDeptDaily = true
			assert.InDelta(t, 123.45, amountUSD, 0.01, "dept_daily 额度应等于刚设置的值")
		case "studio":
			foundStudio = true
			assert.InDelta(t, 9999.99, amountUSD, 0.01, "studio 额度应等于刚设置的值")
		}
	}
	assert.True(t, foundDeptDaily, "chain 应包含 dept_daily")
	assert.True(t, foundStudio, "chain 应包含 studio")
}

func TestTDD6_BudgetSet_NegativeAmount_Rejected(t *testing.T) {
	// 场景：amount_usd < 0 应被拒绝
	token := makeAdminJWT(t, "tdd6-user-neg", "tdd6-dept-neg", "tdd6-studio-neg")

	r := doRequest(t, "PUT", cpURL()+"/api/v1/budgets", authHeader(token), map[string]any{
		"scope_type": "studio",
		"scope_id":   "tdd6-studio-neg",
		"amount_usd": -100.0,
	})
	assert.Equal(t, http.StatusInternalServerError, r.Status, "负金额应被拒绝")
}

func TestTDD6_BudgetList_NoFilters_Rejected(t *testing.T) {
	// 场景：GET /api/v1/budgets 不带 filter 对 admin 应返回 400
	token := makeAdminJWT(t, "tdd6-user-list", "tdd6-dept-list", "tdd6-studio-list")

	r := doRequest(t, "GET", cpURL()+"/api/v1/budgets", authHeader(token), nil)
	assert.Equal(t, http.StatusBadRequest, r.Status, "不带 filter 应被拒绝: %s", r.Raw)
}

func TestTDD6_BudgetList_WithScopeFilter(t *testing.T) {
	// 场景：GET /api/v1/budgets?scope_type=...&scope_id=... 应返回单层配置
	uniqueDept := fmt.Sprintf("tdd6-list-dept-%d", time.Now().UnixNano()%1000000)
	token := makeAdminJWT(t, "tdd6-user-lf", uniqueDept, "tdd6-list-studio")

	// 先 PUT 设置
	doRequest(t, "PUT", cpURL()+"/api/v1/budgets", authHeader(token), map[string]any{
		"scope_type": "dept_daily",
		"scope_id":   uniqueDept,
		"amount_usd": 77.77,
	})

	// 再 GET
	url := fmt.Sprintf("%s/api/v1/budgets?scope_type=dept_daily&scope_id=%s", cpURL(), uniqueDept)
	r := doRequest(t, "GET", url, authHeader(token), nil)
	assert.Equal(t, http.StatusOK, r.Status, "带 filter 应成功: %s", r.Raw)

	items, ok := r.Body["items"].([]any)
	require.True(t, ok)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, "dept_daily", first["scope_type"])
	assert.InDelta(t, 77.77, first["amount_usd"].(float64), 0.01)
}

// ── TDD-6: Evaluate Alert (Internal API) ──

func TestTDD6_EvaluateAlert_InternalAPI(t *testing.T) {
	// 场景：POST /internal/v1/budget/evaluate-alert 通过 X-Internal-Token 调用
	headers := map[string]string{"X-Internal-Token": internalToken()}

	r := doRequest(t, "POST", cpURL()+"/internal/v1/budget/evaluate-alert", headers, map[string]any{
		"scope_type": "studio",
		"scope_id":   "tdd6-alert-studio", // 可能不存在
	})
	// 即使 scope 不存在也应 200（幂等，不报错）
	assert.Equal(t, http.StatusOK, r.Status, "evaluate-alert 应幂等返回 200: %s", r.Raw)
	assert.Equal(t, true, r.Body["ok"])
}

func TestTDD6_EvaluateAlert_MissingInternalToken(t *testing.T) {
	// 场景：不带 X-Internal-Token 调 internal API 应 403
	r := doRequest(t, "POST", cpURL()+"/internal/v1/budget/evaluate-alert", nil, map[string]any{
		"scope_type": "studio",
		"scope_id":   "whatever",
	})
	assert.Equal(t, http.StatusForbidden, r.Status, "无 token 应 403: %s", r.Raw)
}

// ── TDD-6: 开放模式切换 ──

func TestTDD6_GroupOpenMode_InvalidMode(t *testing.T) {
	// 场景：PUT /api/v1/groups/:id/open-mode 传无效的 open_mode 应返回 400
	token := makeAdminJWT(t, "tdd6-user-om", "d1", "s1")

	r := doRequest(t, "PUT", cpURL()+"/api/v1/groups/1/open-mode", authHeader(token), map[string]any{
		"open_mode": "invalid_mode",
	})
	assert.Equal(t, http.StatusBadRequest, r.Status, "无效 open_mode 应 400: %s", r.Raw)
}

func TestTDD6_GroupOpenMode_ValidModes(t *testing.T) {
	// 场景：切换一个存在的 group 到 controlled 再切回 public
	token := makeAdminJWT(t, "tdd6-user-om2", "d1", "s1")

	// 找一个存在的 group id
	listResp := doRequest(t, "GET", cpURL()+"/api/v1/groups", authHeader(token), nil)
	if listResp.Status != http.StatusOK {
		t.Skipf("无法获取 groups 列表：%s", listResp.Raw)
		return
	}
	data, ok := listResp.Body["data"].([]any)
	if !ok || len(data) == 0 {
		t.Skipf("group data 为空，无法测试 open-mode 切换：body=%v", listResp.Body)
		return
	}
	first := data[0].(map[string]any)
	idFloat, _ := first["id"].(float64)
	groupID := int(idFloat)

	// controlled
	r1 := doRequest(t, "PUT", fmt.Sprintf("%s/api/v1/groups/%d/open-mode", cpURL(), groupID), authHeader(token), map[string]any{
		"open_mode": "controlled",
	})
	assert.Equal(t, http.StatusOK, r1.Status, "应能切到 controlled (group id=%d): %s", groupID, r1.Raw)

	// public
	r2 := doRequest(t, "PUT", fmt.Sprintf("%s/api/v1/groups/%d/open-mode", cpURL(), groupID), authHeader(token), map[string]any{
		"open_mode": "public",
	})
	assert.Equal(t, http.StatusOK, r2.Status, "应能切回 public: %s", r2.Raw)
}

// ── TDD-6: 授权下放 ──

func TestTDD6_Entitlement_InvalidScopeType(t *testing.T) {
	// 场景：POST /api/v1/group-entitlements 传无效 scope_type 应返回错误
	token := makeAdminJWT(t, "tdd6-user-ent", "d1", "s1")

	r := doRequest(t, "POST", cpURL()+"/api/v1/group-entitlements", authHeader(token), map[string]any{
		"group_id":   1,
		"scope_type": "user", // 无效
		"scope_id":   "u-1",
	})
	// 应是 400/500 错误（service 校验）
	if r.Status != http.StatusBadRequest && r.Status != http.StatusInternalServerError {
		t.Errorf("无效 scope_type 应被拒绝，但得到 HTTP %d: %s", r.Status, r.Raw)
	}
}

func TestTDD6_Entitlement_GrantAndRevoke(t *testing.T) {
	// 场景：完整的 下放 → 列出 → 撤销 流程
	token := makeAdminJWT(t, "tdd6-user-ent2", "d1", "s1")
	uniqueStudio := fmt.Sprintf("tdd6-ent-studio-%d", time.Now().UnixNano()%1000000)

	// Grant studio 范围（假定 group id=1 存在）
	r1 := doRequest(t, "POST", cpURL()+"/api/v1/group-entitlements", authHeader(token), map[string]any{
		"group_id":   1,
		"scope_type": "studio",
		"scope_id":   uniqueStudio,
	})
	assert.Equal(t, http.StatusOK, r1.Status, "Grant 应成功: %s", r1.Raw)

	// 列出 group 1 的所有 entitlements
	r2 := doRequest(t, "GET", cpURL()+"/api/v1/groups/1/entitlements", authHeader(token), nil)
	assert.Equal(t, http.StatusOK, r2.Status)
	items, ok := r2.Body["items"].([]any)
	require.True(t, ok)

	// 应能找到刚刚下放的那条
	found := false
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		st, _ := m["scope_type"].(string)
		sid, _ := m["scope_id"].(string)
		if st == "studio" && sid == uniqueStudio {
			found = true
			break
		}
	}
	assert.True(t, found, "应能找到刚下放的 studio entitlement，items=%v", items)

	// Revoke
	r3 := doRequest(t, "DELETE", cpURL()+"/api/v1/group-entitlements", authHeader(token), map[string]any{
		"group_id":   1,
		"scope_type": "studio",
		"scope_id":   uniqueStudio,
	})
	assert.Equal(t, http.StatusOK, r3.Status, "Revoke 应成功: %s", r3.Raw)
}

// ── TDD-6: Service Key ──

func TestTDD6_ServiceKey_Create_RequiresManager(t *testing.T) {
	// 场景：未登录调 service-keys 创建应 401
	r := doRequest(t, "POST", cpURL()+"/api/v1/service-keys", nil, map[string]any{
		"dept_id": "d1",
		"name":    "test-svc",
	})
	assert.Equal(t, http.StatusUnauthorized, r.Status, "未登录应 401: %s", r.Raw)
}

func TestTDD6_ServiceKey_CreateAndList(t *testing.T) {
	// 场景：admin 创建 service key 然后 list 能看到
	token := makeAdminJWT(t, "tdd6-svc-creator", "tdd6-svc-dept", "s1")
	uniqueName := fmt.Sprintf("tdd6-svc-%d", time.Now().UnixNano()%1000000)

	// 创建 (注意：handler 校验调用者是否是该 dept 负责人，admin 应能通过)
	r1 := doRequest(t, "POST", cpURL()+"/api/v1/service-keys", authHeader(token), map[string]any{
		"dept_id": "tdd6-svc-dept",
		"name":    uniqueName,
		"purpose": "tdd test",
	})
	if r1.Status == http.StatusForbidden {
		t.Skipf("admin 不是该 dept 负责人，跳过此测试（需真实 RBAC 配置）：%s", r1.Raw)
		return
	}
	if !assert.Equal(t, http.StatusCreated, r1.Status, "创建 service key 应 201: %s", r1.Raw) {
		return
	}

	// 返回应包含明文 key
	key, ok := r1.Body["key"].(string)
	require.True(t, ok, "创建响应应包含明文 key")
	assert.True(t, strings.HasPrefix(key, "sk-"), "key 应以 sk- 开头")

	// List 应能看到
	r2 := doRequest(t, "GET", cpURL()+"/api/v1/service-keys?dept_id=tdd6-svc-dept", authHeader(token), nil)
	assert.Equal(t, http.StatusOK, r2.Status)
	items, ok := r2.Body["items"].([]any)
	require.True(t, ok)

	found := false
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := m["name"].(string); name == uniqueName {
			found = true
			// 返回应不包含明文 key，但应有 masked_key
			_, hasPlain := m["key"]
			_, hasMasked := m["masked_key"]
			assert.False(t, hasPlain, "list 不应包含明文 key")
			assert.True(t, hasMasked, "list 应包含 masked_key")
			break
		}
	}
	assert.True(t, found, "list 应包含刚创建的 service key")
}

// ── TDD-6: 扩容申请 ──

func TestTDD6_ExpansionRequest_CreateAndList(t *testing.T) {
	// 场景：管理员发起扩容申请再 list 自己的
	// 注：user_id 必须是合法 UUID（requester_id 是 UUID 列）
	uniqueUID := uuid.New().String()
	token := makeAdminJWT(t, uniqueUID, "tdd6-exp-dept", "s1")

	// 创建
	r1 := doRequest(t, "POST", cpURL()+"/api/v1/expansion-requests", authHeader(token), map[string]any{
		"scope_type":       "dept_daily",
		"scope_id":         "tdd6-exp-dept",
		"requested_amount": 500.0,
		"reason":           "tdd test reason",
	})
	if !assert.Equal(t, http.StatusCreated, r1.Status, "创建扩容申请应 201: %s", r1.Raw) {
		return
	}
	reqID, ok := r1.Body["id"].(string)
	require.True(t, ok, "响应应包含 id")
	assert.Equal(t, "pending", r1.Body["status"])

	// List (requester 视角)
	r2 := doRequest(t, "GET", cpURL()+"/api/v1/expansion-requests?role=requester", authHeader(token), nil)
	assert.Equal(t, http.StatusOK, r2.Status)
	items, ok := r2.Body["items"].([]any)
	require.True(t, ok)

	found := false
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := m["id"].(string); id == reqID {
			found = true
			break
		}
	}
	assert.True(t, found, "list 应包含刚创建的申请")
}

func TestTDD6_ExpansionRequest_Decide(t *testing.T) {
	// 场景：创建 → 决定（approve）→ 状态变更
	uniqueUID := uuid.New().String()
	token := makeAdminJWT(t, uniqueUID, "tdd6-exp-dept-dec", "s1")
	// 创建
	r1 := doRequest(t, "POST", cpURL()+"/api/v1/expansion-requests", authHeader(token), map[string]any{
		"scope_type":       "studio",
		"scope_id":         "s1",
		"requested_amount": 1000.0,
		"reason":           "tdd test approve",
	})
	require.Equal(t, http.StatusCreated, r1.Status)
	reqID := r1.Body["id"].(string)

	// Approve
	r2 := doRequest(t, "POST", fmt.Sprintf("%s/api/v1/expansion-requests/%s/decision", cpURL(), reqID), authHeader(token), map[string]any{
		"action":  "approve",
		"comment": "approved by tdd",
	})
	assert.Equal(t, http.StatusOK, r2.Status, "approve 应成功: %s", r2.Raw)
}

// TestTDD6_ExpansionRequest_ListWithInvalidUserID_ReturnsEmpty 验证防御性校验：
// 当 JWT 中 user_id 不是合法 UUID 时，ListMine 应返回空列表而不是 500 SQL 错误。
// 背景：requester_id 列是 UUID 类型，早期实现在非法 UUID 下会 SQL 报错（PG 22P02），
// 现已在 repo 层加防御性校验。
func TestTDD6_ExpansionRequest_ListWithInvalidUserID_ReturnsEmpty(t *testing.T) {
	// 构造一个 user_id 不是 UUID 的 JWT
	token := makeAdminJWT(t, "not-a-uuid-plain-string", "dept-x", "studio-x")

	r := doRequest(t, "GET", cpURL()+"/api/v1/expansion-requests?role=requester", authHeader(token), nil)
	assert.Equal(t, http.StatusOK, r.Status, "非法 user_id 应返回 200（空列表），而不是 500: %s", r.Raw)
	items, ok := r.Body["items"].([]any)
	require.True(t, ok)
	assert.Empty(t, items, "非法 user_id 应返回空列表")
}
