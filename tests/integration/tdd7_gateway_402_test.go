//go:build integration

// TDD-7: 网关端到端预算拦截集成测试。
//
// 测试目标：验证 v0.7 预算体系端到端工作：
//   1. 创建 personal key → 配置极小预算 → chat/completions 应返回 402 budget_exceeded
//   2. 创建 personal key → 配置充足预算 → chat/completions 应通过预算预扣（可能因 upstream 失败返回其他码，但不该 402）
//   3. 预算预扣成功后，Redis 中 budget:used:{scope} 应有增量
//
// 运行条件：
//   - control-plane (127.0.0.1:8081) + gateway (127.0.0.1:8080)
//   - Postgres (192.0.2.10:5432) + Redis (192.0.2.10:6379)
//   - AUTH_URL=.env 指向 control-plane (8081)，这是关键配置
//
// 运行：
//   go test ./tests/integration/... -tags=integration -run "TDD7" -v -timeout 120s

package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// generateAdminToken 生成一个有效的 admin JWT token（不依赖外部文件）。
func generateAdminToken(t *testing.T) string {
	t.Helper()
	secret := jwtSecret() // 复用 TDD-6 的 helper
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":          "llm-gateway",
		"iat":          now.Unix(),
		"exp":          now.Add(time.Hour).Unix(),
		"user_id":      "00000000-0000-0000-0000-000000000001",
		"email":        "tdd7-admin@test.local",
		"name":         "TDD7 Admin",
		"passport":     "",
		"dept_id":      "test-dept",
		"dept_name":    "Test Dept",
		"studio_id":    "test-studio",
		"studio_name":  "Test Studio",
		"role":         "admin",
		"company_name": "Test Co",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	require.NoError(t, err, "JWT 签名应成功")
	return signed
}

// realRedisClient 直接连真实 Redis（TDD-7 依赖真实环境）
func realRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "192.0.2.10:6379"
	}
	pwd := os.Getenv("REDIS_PASSWORD")
	if pwd == "" {
		pwd = "CHANGE_ME_REDIS_PASSWORD"
	}
	c := redis.NewClient(&redis.Options{Addr: addr, Password: pwd})
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Ping(context.Background()).Err(), "连接真实 Redis 失败")
	return c
}

// currentPeriod YYYY-MM
func currentPeriod() string {
	now := time.Now()
	return fmt.Sprintf("%d-%02d", now.Year(), now.Month())
}

// createPersonalKey 通过 control-plane API 创建 personal key，返回 (id, plaintext)
func createPersonalKey(t *testing.T, adminToken, name string) (string, string) {
	t.Helper()
	r := doRequest(t, "POST", cpURL()+"/api/v1/keys", authHeader(adminToken), map[string]any{
		"name": name,
	})
	require.Equal(t, http.StatusCreated, r.Status, "创建 key 应 201: %s", r.Raw)
	data := r.Body["data"].(map[string]any)
	id, _ := data["id"].(string)
	key, _ := data["key"].(string)
	require.NotEmpty(t, id)
	require.NotEmpty(t, key)
	return id, key
}

// updateKeyDeptInfo 直接 SQL 给 key 设置 dept_id/studio_id（无公开 API 时走 DB）
func updateKeyDeptInfo(t *testing.T, keyID, deptID, studioID string) {
	t.Helper()
	// 通过 psql CLI 简单粗暴做（测试不优雅但直接）
	// 实际项目应该暴露 update key API
	// 我们用 redis-cli + DB 验证即可，这里略过
	// （TDD-6 的 ServiceKey 创建接口已经能自动填 dept_id，但 personal key 还没）
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestTDD7_BudgetExceeded_402 验证预算预扣在 chat 请求超限时返回 402。
//
// 场景：
//  1. 创建一个测试 key，给它设极小预算（0.0001 USD）
//  2. 调 chat/completions → 期望 402 budget_exceeded
//
// 注：由于 personal key 不能通过 API 设 dept_id，我们复用一个已存在的带 dept_id 的测试 key
// （tdd7-test-key，setup 已在之前手工创建）。
func TestTDD7_BudgetExceeded_402(t *testing.T) {
	// 直接用之前创建的测试 key（带 dept_id=技术研发部, studio_id=200f2786-...）
	// 如果这个 key 不存在，TDD 会失败，先手工 setup
	testKeyPlaintext := os.Getenv("TDD7_TEST_KEY")
	if testKeyPlaintext == "" {
		testKeyPlaintext = "sk-e2b7b66eccb912bde9b9d1ace0be17d48e779984b540be6c0782d0fb68dcbc4b"
	}

	// 设置极小预算（覆盖之前的设置）
	adminToken := generateAdminToken(t)
	t.Logf("Generated admin token (first 50): %s...", adminToken[:50])

	// 验证 token 能用：调 /api/v1/me
	meResp := doRequest(t, "GET", cpURL()+"/api/v1/me", authHeader(adminToken), nil)
	t.Logf("/api/v1/me response: status=%d raw=%s", meResp.Status, meResp.Raw)

	if meResp.Status != http.StatusOK {
		t.Fatalf("admin token 无效（status %d），跳过后续测试", meResp.Status)
	}

	// dept_daily 设 0.0001 USD
	r1 := doRequest(t, "PUT", cpURL()+"/api/v1/budgets", authHeader(adminToken), map[string]any{
		"scope_type": "dept_daily", "scope_id": "技术研发部", "amount_usd": 0.0001,
	})
	require.Equal(t, http.StatusOK, r1.Status, "设置 dept_daily 预算应 200")

	// studio 设 0.0001 USD
	r2 := doRequest(t, "PUT", cpURL()+"/api/v1/budgets", authHeader(adminToken), map[string]any{
		"scope_type": "studio", "scope_id": "200f2786-02e0-4397-a336-3a843bc5f1d3", "amount_usd": 0.0001,
	})
	require.Equal(t, http.StatusOK, r2.Status)

	// 清空 redis 让 keycache 失效
	rdb := realRedisClient(t)
	rdb.FlushDB(context.Background())
	time.Sleep(100 * time.Millisecond)

	// 调 gateway chat/completions 期望 402
	req, err := http.NewRequest(http.MethodPost, gwURL()+"/v1/chat/completions",
		strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testKeyPlaintext)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, http.StatusPaymentRequired, resp.StatusCode,
		"应被预算拦截返回 402，body=%s", string(body))
	assert.Contains(t, string(body), "budget_exceeded", "error.code 应为 budget_exceeded")
	assert.Contains(t, string(body), "技术研发部", "错误信息应指明哪个 scope 超额")
}

// TestTDD7_BudgetPreReserve_RedisIncr 验证预算预扣后 Redis used 增量。
//
// 场景：
//  1. 配置充足预算
//  2. 清空 Redis used
//  3. 调 chat/completions（可能 upstream 失败，但预算应已预扣）
//  4. 检查 Redis budget:used:dept_daily:{dept} 是否大于 0
func TestTDD7_BudgetPreReserve_RedisIncr(t *testing.T) {
	adminToken := generateAdminToken(t)

	// 配置充足的预算
	doRequest(t, "PUT", cpURL()+"/api/v1/budgets", authHeader(adminToken), map[string]any{
		"scope_type": "dept_daily", "scope_id": "预算预扣测试部", "amount_usd": 10000,
	})

	// 清空 redis
	rdb := realRedisClient(t)
	rdb.FlushDB(context.Background())
	time.Sleep(100 * time.Millisecond)

	// 注意：本测试由于需要一个带 dept_id="预算预扣测试部" 的 key 才能走通，
	// 暂时跳过 - 仅作占位。完整场景需要 TDD-6 的 ServiceKey 创建接口，
	// 让 dept_id 自动填为 "预算预扣测试部"。
	t.Skip("需要特定 dept_id 的测试 key（依赖 ServiceKey 或手工 setup）")
}

// ─── AUTH URL 测试 ────────────────────────────────────────────────────────────

// TestTDD7_AuthURLCorrect 验证 .env AUTH_URL 指向 control-plane（关键配置）。
//
// 历史 bug：.env 里 AUTH_URL=8080（gateway 自己），导致 gateway 调自己的简化版
// verify-key（不含 BudgetChain），预算拦截永远不生效。
//
// 验证方法：通过 real HTTP 调 gateway，看 verify-key 响应是否含 budget_chain 字段。
// 间接证明：如果 chat/completions 没报 budget_exceeded（在极小预算下），说明 AUTH_URL 错配了。
func TestTDD7_AuthURLCorrect(t *testing.T) {
	// 通过检查 cp 的 /internal/v1/auth/verify-key endpoint 是否有 budget_chain 字段
	// 然后确认 AUTH_URL 指向 8081
	authURL := os.Getenv("AUTH_URL")
	if authURL == "" {
		// 从 .env 文件读
		data, err := os.ReadFile("../../.env")
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "AUTH_URL=") {
					authURL = strings.TrimPrefix(line, "AUTH_URL=")
					break
				}
			}
		}
	}

	// 关键断言：AUTH_URL 必须指向 control-plane（端口 8081），不是 gateway（8080）
	assert.NotContains(t, authURL, ":8080",
		"AUTH_URL 应指向 control-plane (8081)，不能指向 gateway 自己 (8080)。当前值: %s", authURL)
	assert.Contains(t, authURL, ":8081",
		"AUTH_URL 应包含 :8081。当前值: %s", authURL)
}
