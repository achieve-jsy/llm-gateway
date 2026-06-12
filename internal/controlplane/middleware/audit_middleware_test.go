package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// testUUID 固定的测试 UUID，避免每个测试重复构造
var testUUID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

// ── mockRecorder 用于验证 AuditMiddleware 是否调用了 Record ──

type mockRecorder struct {
	calls []audit.Input
}

func (r *mockRecorder) Record(in audit.Input) {
	r.calls = append(r.calls, in)
}

func newTestRouter(rec audit.Recorder) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuditMiddleware(rec))

	// 模拟 adminGroup 路由
	r.GET("/api/v1/providers", func(c *gin.Context) { c.String(200, "list") })
	r.POST("/api/v1/providers", func(c *gin.Context) { c.String(201, "create") })
	r.PUT("/api/v1/providers/:id", func(c *gin.Context) { c.String(200, "update") })
	r.PATCH("/api/v1/providers/:id/status", func(c *gin.Context) { c.String(200, "toggle") })
	r.DELETE("/api/v1/providers/:id", func(c *gin.Context) { c.String(200, "delete") })

	// 手动埋点的路由
	r.POST("/api/v1/service-keys", func(c *gin.Context) {
		rec.Record(audit.Input{
			Action:       audit.ActionServiceKeyCreate,
			ResourceType: audit.ResourceServiceKey,
			ResourceID:   "key-1",
			UserID:       testUUID,
		})
		audit.MarkRecorded(c)
		c.String(201, "create")
	})

	return r
}

func perform(router *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, nil)
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	router.ServeHTTP(w, req)
	return w
}

func TestAuditMiddleware_RecordsWriteMethods(t *testing.T) {
	rec := &mockRecorder{}
	router := newTestRouter(rec)

	methods := []struct {
		method string
		path   string
	}{
		{"POST", "/api/v1/providers"},
		{"PUT", "/api/v1/providers/p-1"},
		{"PATCH", "/api/v1/providers/p-1/status"},
		{"DELETE", "/api/v1/providers/p-1"},
	}

	for _, tc := range methods {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			before := len(rec.calls)
			w := perform(router, tc.method, tc.path)
			if w.Code != http.StatusOK && w.Code != http.StatusCreated {
				t.Fatalf("unexpected status %d", w.Code)
			}
			if len(rec.calls) != before+1 {
				t.Fatalf("expected 1 Record call, got %d (total calls: %d)", len(rec.calls)-before, len(rec.calls))
			}
			last := rec.calls[len(rec.calls)-1]
			if last.Action == "" {
				t.Error("Action should not be empty")
			}
			if last.ResourceType == "" {
				t.Error("ResourceType should not be empty")
			}
			if last.ClientIP == "" {
				t.Error("ClientIP should be extracted from X-Forwarded-For")
			}
		})
	}
}

func TestAuditMiddleware_SkipsReadMethods(t *testing.T) {
	rec := &mockRecorder{}
	router := newTestRouter(rec)

	w := perform(router, "GET", "/api/v1/providers")
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", w.Code)
	}
	if len(rec.calls) != 0 {
		t.Errorf("GET should not be audited, got %d calls", len(rec.calls))
	}
}

func TestAuditMiddleware_RespectsMarkRecorded(t *testing.T) {
	rec := &mockRecorder{}
	router := newTestRouter(rec)

	// POST /api/v1/service-keys 是手动埋点的路由，应只记录 1 次（不是 2 次）
	perform(router, "POST", "/api/v1/service-keys")
	if len(rec.calls) != 1 {
		t.Fatalf("expected exactly 1 Record call for manually-logged route, got %d", len(rec.calls))
	}
	if rec.calls[0].Action != audit.ActionServiceKeyCreate {
		t.Errorf("expected service_key.create, got %s", rec.calls[0].Action)
	}
}

func TestAuditMiddleware_CapturesStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := &mockRecorder{}
	r := gin.New()
	r.Use(AuditMiddleware(rec))
	r.POST("/api/v1/providers", func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	})

	perform(r, "POST", "/api/v1/providers")
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(rec.calls))
	}
	if rec.calls[0].StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.calls[0].StatusCode)
	}
}

func TestAuditMiddleware_NilRecorderSafe(t *testing.T) {
	// Wrap(nil) 应降为 Nop，不 panic
	rec := audit.Wrap(nil)
	router := newTestRouter(rec)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic with nil recorder: %v", r)
		}
	}()
	perform(router, "POST", "/api/v1/providers")
}

func TestAuditMiddleware_DetailsContainContext(t *testing.T) {
	rec := &mockRecorder{}
	router := newTestRouter(rec)

	perform(router, "POST", "/api/v1/providers")
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 call")
	}
	det, ok := rec.calls[0].Details.(map[string]any)
	if !ok {
		t.Fatalf("Details should be map[string]any, got %T", rec.calls[0].Details)
	}
	if det["method"] != "POST" {
		t.Errorf("method = %v", det["method"])
	}
	if det["path"] != "/api/v1/providers" {
		t.Errorf("path = %v", det["path"])
	}
}

// performWithBody 发送带请求体的请求，供脱敏相关测试复用。
func performWithBody(router *gin.Engine, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	router.ServeHTTP(w, req)
	return w
}

// TestAuditMiddleware_RedactsBeforeAfter 验证 handler 注入的 before/after 也经过脱敏。
func TestAuditMiddleware_RedactsBeforeAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := &mockRecorder{}
	r := gin.New()
	r.Use(AuditMiddleware(rec))
	r.POST("/api/v1/budgets", func(c *gin.Context) {
		audit.SetBeforeAfter(c,
			map[string]any{"api_key": "sk-old-secret", "amount": 100},
			map[string]any{"api_key": "sk-new-secret", "amount": 200},
		)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	perform(r, "POST", "/api/v1/budgets")
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(rec.calls))
	}
	det, ok := rec.calls[0].Details.(map[string]any)
	if !ok {
		t.Fatalf("Details should be map[string]any, got %T", rec.calls[0].Details)
	}
	before, ok := det["before"].(map[string]any)
	if !ok {
		t.Fatalf("before should be map[string]any, got %T", det["before"])
	}
	if before["api_key"] != "***" {
		t.Errorf("before.api_key should be redacted, got %v", before["api_key"])
	}
	if before["amount"] != float64(100) {
		t.Errorf("before.amount should be preserved as 100, got %v (%T)", before["amount"], before["amount"])
	}
	after, ok := det["after"].(map[string]any)
	if !ok {
		t.Fatalf("after should be map[string]any, got %T", det["after"])
	}
	if after["api_key"] != "***" {
		t.Errorf("after.api_key should be redacted, got %v", after["api_key"])
	}
}

// TestAuditMiddleware_BeforeAfterNilOmitted 验证 nil 的 before/after 不会写入 null。
func TestAuditMiddleware_BeforeAfterNilOmitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := &mockRecorder{}
	r := gin.New()
	r.Use(AuditMiddleware(rec))
	r.POST("/api/v1/budgets", func(c *gin.Context) {
		audit.SetBeforeAfter(c, nil, map[string]any{"name": "x"})
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	perform(r, "POST", "/api/v1/budgets")
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(rec.calls))
	}
	det := rec.calls[0].Details.(map[string]any)
	if _, exists := det["before"]; exists {
		t.Errorf("before should be omitted when nil, got %v", det["before"])
	}
	if _, exists := det["after"]; !exists {
		t.Errorf("after should be present when non-nil")
	}
}

// TestRedactValue_StringifiedJSONArray 验证字符串化的 JSON 数组也会递归脱敏。
func TestRedactValue_StringifiedJSONArray(t *testing.T) {
	in := `[{"api_key":"sk-abc","name":"keep"},{"token":"tok-123"}]`
	got := redactValue(in)
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected string output, got %T", got)
	}
	if strings.Contains(s, "sk-abc") || strings.Contains(s, "tok-123") {
		t.Errorf("stringified JSON array secrets should be masked, got %s", s)
	}
	if !strings.Contains(s, `"api_key":"***"`) {
		t.Errorf("api_key should be ***, got %s", s)
	}
	if !strings.Contains(s, `"token":"***"`) {
		t.Errorf("token should be ***, got %s", s)
	}
}

// TestLooksLikeSecret_DetectsJWTAndAWS 验证 JWT / AWS 密钥识别。
func TestLooksLikeSecret_DetectsJWTAndAWS(t *testing.T) {
	cases := map[string]bool{
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig": true,
		"AKIAIOSFODNN7EXAMPLE":                     true,
		"ASIAXXXXXXXXXXXX":                         true,
		"sk-abc123":                                true,
		"bearer abc.def":                           true,
		"https://example.com/api":                  false,
		"normal-name":                              false,
	}
	for in, want := range cases {
		if got := looksLikeSecret(in); got != want {
			t.Errorf("looksLikeSecret(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestMaskURLCredentials 验证 URL 内嵌凭证（user:pass@ 与敏感查询参数）掩码。
func TestMaskURLCredentials(t *testing.T) {
	in := "https://user:pass@example.com/api?token=abc&sig=xyz&foo=bar"
	got, changed := maskURLCredentials(in)
	if !changed {
		t.Fatalf("expected change, got none")
	}
	if strings.Contains(got, "pass") && strings.Contains(got, "user:pass@") {
		t.Errorf("password in userinfo should be masked, got %s", got)
	}
	if strings.Contains(got, "token=abc") || strings.Contains(got, "sig=xyz") {
		t.Errorf("sensitive query params should be masked, got %s", got)
	}
	if !strings.Contains(got, "user:***@") {
		t.Errorf("userinfo password should be ***, got %s", got)
	}
	if !strings.Contains(got, "foo=bar") {
		t.Errorf("non-sensitive query param should be preserved, got %s", got)
	}
	// 幂等：已掩码内容再次掩码不应变化
	got2, changed2 := maskURLCredentials(got)
	if changed2 {
		t.Errorf("maskURLCredentials should be idempotent, but changed: %s -> %s", got, got2)
	}
}

// TestSanitizeBody_FormEncoded 验证 form-encoded 体的密码脱敏。
func TestSanitizeBody_FormEncoded(t *testing.T) {
	in := []byte("password=secret123&name=alice")
	got := sanitizeBody(in, "application/x-www-form-urlencoded")
	if strings.Contains(got, "secret123") {
		t.Errorf("form password should be masked, got %s", got)
	}
	if !strings.Contains(got, "password=%2A%2A%2A") {
		t.Errorf("password value should encode to ***, got %s", got)
	}
	if !strings.Contains(got, "name=alice") {
		t.Errorf("non-sensitive form field should be preserved, got %s", got)
	}
}

// TestSanitizeBody_MultipartPlaceholder 验证 multipart 只存大小占位。
func TestSanitizeBody_MultipartPlaceholder(t *testing.T) {
	in := []byte("--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.txt\"\r\n\r\nSECRET_FILE_CONTENT\r\n--boundary--")
	got := sanitizeBody(in, "multipart/form-data; boundary=boundary")
	if strings.Contains(got, "SECRET_FILE_CONTENT") {
		t.Errorf("multipart body must not leak raw content, got %s", got)
	}
	if !strings.Contains(got, "<multipart body, ") || !strings.Contains(got, "bytes>") {
		t.Errorf("multipart should store size placeholder, got %s", got)
	}
}

// TestSanitizeBody_NonJSONPlaceholder 验证其它非 JSON 体只存占位。
func TestSanitizeBody_NonJSONPlaceholder(t *testing.T) {
	in := []byte("some plain text with a password=secret123")
	got := sanitizeBody(in, "text/plain")
	if strings.Contains(got, "secret123") {
		t.Errorf("non-json body must not leak raw content, got %s", got)
	}
	if !strings.Contains(got, "<non-json body, ") || !strings.Contains(got, "bytes>") {
		t.Errorf("non-json body should store size placeholder, got %s", got)
	}
}

// TestSanitizeBody_JSONURLCredentials 验证 JSON 体中的 URL 内嵌凭证被掩码。
func TestSanitizeBody_JSONURLCredentials(t *testing.T) {
	in := []byte(`{"webhook_url":"https://user:pass@example.com/hook?token=abc","name":"keep"}`)
	got := sanitizeBody(in, "application/json")
	if strings.Contains(got, "pass") && strings.Contains(got, "user:pass@") {
		t.Errorf("URL userinfo password should be masked, got %s", got)
	}
	if strings.Contains(got, "token=abc") {
		t.Errorf("URL token should be masked, got %s", got)
	}
	if !strings.Contains(got, "keep") {
		t.Errorf("non-sensitive field should be preserved, got %s", got)
	}
}

// TestAuditUserCacheBounded 验证缓存计数越阈后整体清空、计数归零。
func TestAuditUserCacheBounded(t *testing.T) {
	// 重置共享状态，避免污染其它测试
	auditUserCache.Range(func(k, _ any) bool { auditUserCache.Delete(k); return true })
	auditUserCacheCount.Store(0)
	defer func() {
		auditUserCache.Range(func(k, _ any) bool { auditUserCache.Delete(k); return true })
		auditUserCacheCount.Store(0)
	}()

	for i := 0; i < auditUserCacheMaxEntries; i++ {
		auditUserCache.Store(strconv.Itoa(i), &userIdentity{Name: "x"})
		bumpAuditUserCache()
	}
	if got := auditUserCacheCount.Load(); got != auditUserCacheMaxEntries {
		t.Fatalf("expected count %d before overflow, got %d", auditUserCacheMaxEntries, got)
	}

	// 再存一条触发越阈清空
	auditUserCache.Store("overflow", &userIdentity{Name: "x"})
	bumpAuditUserCache()
	if got := auditUserCacheCount.Load(); got != 0 {
		t.Fatalf("expected count reset to 0 after clear, got %d", got)
	}
	cnt := 0
	auditUserCache.Range(func(k, _ any) bool { cnt++; return true })
	if cnt != 0 {
		t.Errorf("expected cache empty after clear, got %d entries", cnt)
	}
}
