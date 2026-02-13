package handler_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupOpsTestDB 创建 SQLite 内存 DB 并建表
func setupOpsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.AlertRule{},
		&model.AlertEvent{},
		&model.AlertSilence{},
		&model.SystemLog{},
		&model.AlertJobHeartbeat{},
		&model.SystemConfig{},
	); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}
	return db
}

// setupOpsMiniredis 创建 miniredis 实例，返回 redis.Client 和关闭函数
func setupOpsMiniredis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, func() {
		rdb.Close()
		mr.Close()
	}
}

// setupOpsHandler 创建完整的 OpsHandler 和 Gin Engine
func setupOpsHandler(t *testing.T) (*gin.Engine, *gorm.DB, *redis.Client) {
	t.Helper()
	db := setupOpsTestDB(t)
	rdb, cleanup := setupOpsMiniredis(t)
	t.Cleanup(cleanup)

	mc := service.NewSystemMetricsCollector(db, rdb)
	h := handler.NewOpsHandler(db, rdb, mc)

	r := gin.New()
	// 告警规则
	r.GET("/ops/alert-rules", h.ListAlertRules)
	r.POST("/ops/alert-rules", h.CreateAlertRule)
	r.PUT("/ops/alert-rules/:id", h.UpdateAlertRule)
	r.DELETE("/ops/alert-rules/:id", h.DeleteAlertRule)
	// 告警事件
	r.GET("/ops/alert-events", h.ListAlertEvents)
	r.GET("/ops/alert-events/:id", h.GetAlertEvent)
	r.PUT("/ops/alert-events/:id/status", h.UpdateAlertEventStatus)
	r.POST("/ops/alert-silences", h.CreateAlertSilence)
	// 系统日志
	r.GET("/ops/system-logs", h.ListSystemLogs)
	r.GET("/ops/system-logs/health", h.GetSystemLogSinkHealth)
	r.POST("/ops/system-logs/cleanup", h.CleanupSystemLogs)
	// 设置
	r.GET("/ops/settings/metric-thresholds", h.GetMetricThresholds)
	r.PUT("/ops/settings/metric-thresholds", h.UpdateMetricThresholds)
	r.GET("/ops/settings/dashboard", h.GetDashboardSettings)
	r.PUT("/ops/settings/dashboard", h.UpdateDashboardSettings)
	// 运行时日志配置
	r.GET("/ops/runtime/logging", h.GetRuntimeLogConfig)
	r.PUT("/ops/runtime/logging", h.UpdateRuntimeLogConfig)
	r.POST("/ops/runtime/logging/reset", h.ResetRuntimeLogConfig)
	// Jobs
	r.GET("/ops/jobs", h.ListJobs)
	// Token 统计（需要 call_logs，此处不注册）
	// 并发（需要 call_logs，此处不注册）

	return r, db, rdb
}

// doRequest 发送 HTTP 请求并返回 recorder
func doRequest(r *gin.Engine, method, path string, body ...string) *httptest.ResponseRecorder {
	var reqBody string
	if len(body) > 0 {
		reqBody = body[0]
	}
	req := httptest.NewRequest(method, path, strings.NewReader(reqBody))
	if reqBody != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Errorf("status = %d, want %d, body = %s", w.Code, want, w.Body.String())
	}
}
