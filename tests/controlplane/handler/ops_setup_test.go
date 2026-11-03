package handler_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
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
		// call_logs：GetRequestErrorDetail / GetUpstreamErrorDetail / ListErrorLogs 等
		// 查询此表，测试用 INSERT INTO call_logs 直接 seed，需先建表。
		// 注意：handler 中含 Postgres 方言（PERCENTILE_CONT/INTERVAL/::jsonb）的端点
		// 在 SQLite 上不可行，相关测试用例已规避或用真 Postgres；此处仅建表供
		// 标准 SQL 端点的 detail/error-log 测试使用。
		&obsmodel.CallLog{},
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
	// 阈值 & 看板设置 & 高级设置
	r.GET("/ops/settings/metric-thresholds", h.GetMetricThresholds)
	r.PUT("/ops/settings/metric-thresholds", h.UpdateMetricThresholds)
	r.GET("/ops/settings/dashboard", h.GetDashboardSettings)
	r.PUT("/ops/settings/dashboard", h.UpdateDashboardSettings)
	r.GET("/ops/settings/advanced", h.GetAdvancedSettings)
	r.PUT("/ops/settings/advanced", h.UpdateAdvancedSettings)
	// 运行时日志配置
	r.GET("/ops/runtime/logging", h.GetRuntimeLogConfig)
	r.PUT("/ops/runtime/logging", h.UpdateRuntimeLogConfig)
	r.POST("/ops/runtime/logging/reset", h.ResetRuntimeLogConfig)
	// 账户可用性（纯读 Redis，SQLite 可跑）
	r.GET("/ops/account-availability", h.GetAccountAvailability)
	// 请求错误 / 上游错误详情（标准 SQL 查 call_logs，SQLite 可跑）
	r.GET("/ops/request-errors/:id", h.GetRequestErrorDetail)
	r.GET("/ops/upstream-errors/:id", h.GetUpstreamErrorDetail)
	// 错误日志增强视图（查 system_logs，SQLite 可跑；resolve 用 ::jsonb 方言，仅注册路由，
	// 新增的 resolve 测试在真 Postgres 或 dialect-aware 后补，见 WP3/WP11）
	r.GET("/ops/error-logs", h.ListErrorLogs)
	r.PUT("/ops/error-logs/:id/resolve", h.ResolveErrorLog)
	r.PUT("/ops/request-errors/:id/resolve", h.ResolveRequestError)
	r.PUT("/ops/upstream-errors/:id/resolve", h.ResolveUpstreamError)
	// Jobs
	r.GET("/ops/jobs", h.ListJobs)
	// Token 统计（需要 call_logs 且 handler 用 Postgres 方言 percentile，此处不注册）
	// 并发（需要 call_logs 且 handler 用 Postgres 方言，此处不注册）

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
