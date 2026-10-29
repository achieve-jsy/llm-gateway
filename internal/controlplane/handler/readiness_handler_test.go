package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// 说明：/readyz 是蓝绿切流门禁与 compose healthcheck 的唯一依据，
// 它必须真的检查「能不能读库、能不能连 Redis」，否则就会出现
// 「进程活着 → /healthz 200 → 切流成功 → 所有 api key 验证失败」的事故。
func newReadyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func newReadyRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func createApiKeysTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	ddl := `CREATE TABLE api_keys (
		id TEXT PRIMARY KEY,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		expires_at DATETIME
	)`
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("create api_keys: %v", err)
	}
}

func runReadyz(t *testing.T, h gin.HandlerFunc, path string) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/readyz", h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w.Code, w.Body.String()
}

func TestReadyzHealthyReturns200(t *testing.T) {
	code, body := runReadyz(t, ReadinessHandler(newReadyTestDB(t), newReadyRedis(t)), "/readyz")
	if code != http.StatusOK {
		t.Fatalf("readyz = %d, want 200; body=%s", code, body)
	}
}

func TestReadyzReportsClosedDB(t *testing.T) {
	db := newReadyTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	_ = sqlDB.Close()

	code, body := runReadyz(t, ReadinessHandler(db, newReadyRedis(t)), "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("readyz(db closed) = %d, want 503; body=%s", code, body)
	}
}

func TestReadyzReportsUnreachableRedis(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })

	code, body := runReadyz(t, ReadinessHandler(newReadyTestDB(t), rdb), "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("readyz(redis down) = %d, want 503; body=%s", code, body)
	}
}

// deep 模式：ping 通 ≠ 连的是对的库。空库必须判为 not_ready，
// 否则切流后所有真实 Key 都会 401（verify 会正常返回 key_not_found）。
func TestReadyzDeepFailsWithoutUsableKeys(t *testing.T) {
	db := newReadyTestDB(t)
	createApiKeysTable(t, db)

	code, body := runReadyz(t, ReadinessHandler(db, newReadyRedis(t)), "/readyz?deep=1")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("readyz?deep=1(空库) = %d, want 503; body=%s", code, body)
	}
}

func TestReadyzDeepPassesWithUsableKey(t *testing.T) {
	db := newReadyTestDB(t)
	createApiKeysTable(t, db)
	if err := db.Exec("INSERT INTO api_keys (id, enabled, expires_at) VALUES (?,?,?)", "k1", true, nil).Error; err != nil {
		t.Fatalf("insert key: %v", err)
	}

	code, body := runReadyz(t, ReadinessHandler(db, newReadyRedis(t)), "/readyz?deep=1")
	if code != http.StatusOK {
		t.Fatalf("readyz?deep=1(有可用 Key) = %d, want 200; body=%s", code, body)
	}
}

// 过期/禁用的 Key 不算可用：否则 deep 检查形同虚设
func TestReadyzDeepIgnoresExpiredOrDisabledKeys(t *testing.T) {
	db := newReadyTestDB(t)
	createApiKeysTable(t, db)
	past := time.Now().Add(-time.Hour)
	for _, stmt := range []struct {
		id      string
		enabled bool
		expires any
	}{
		{"k-expired", true, past},
		{"k-disabled", false, nil},
	} {
		if err := db.Exec("INSERT INTO api_keys (id, enabled, expires_at) VALUES (?,?,?)", stmt.id, stmt.enabled, stmt.expires).Error; err != nil {
			t.Fatalf("insert %s: %v", stmt.id, err)
		}
	}

	code, body := runReadyz(t, ReadinessHandler(db, newReadyRedis(t)), "/readyz?deep=1")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("readyz?deep=1(全是过期/禁用) = %d, want 503; body=%s", code, body)
	}
}
