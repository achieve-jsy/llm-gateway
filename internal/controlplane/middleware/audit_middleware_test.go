package middleware

import (
	"net/http"
	"net/http/httptest"
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
