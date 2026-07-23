package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	corequota "github.com/example/llm-gateway/internal/core/quota"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsrepo "github.com/example/llm-gateway/internal/gateway/observability/repository"
	"github.com/example/llm-gateway/internal/gateway/quota"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 本文件覆盖 t2「配额拒绝的 gate/layer/limit 落库到 call_logs」的观测链路：
// quota.WriteQuotaRejected 写 gin context → UsageCollector 采集 → Collector 批写 call_logs，
// 断言落库行能区分并发闸/ RPM 闸、区分配额层、并带上该次判定的限额。
//
// 说明：
//   - 这里直接调用 WriteQuotaRejected（真实的 429 写入入口），而不是从 HTTP 请求触发并发拒绝，
//     因为"并发闸是否真的会拒绝"属于 t1 的语义，由 t3 独立验证；本文件只负责"拒绝事实能被观测到"。
//   - SQLite 仅替代 PG 承载"列名 + 取值"断言；列类型（varchar(32)/bigint）一致性由
//     atlas-migrations 迁移与真实 PG 的 information_schema 核对覆盖（见测试报告）。

// quotaTelemetryCollector 构造一个写 SQLite 的采集器，用于断言落库行。
func quotaTelemetryCollector(t *testing.T, dbName string) (*gwobs.Collector, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+dbName+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))

	cfg := gwobs.DefaultCollectorConfig()
	cfg.BufferSize = 16
	cfg.FlushN = 1
	cfg.FlushInterval = 5 * time.Millisecond
	cfg.DBRetryCount = 1
	cfg.DBRetryBackoff = time.Millisecond

	collector := gwobs.NewCollector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	collector.StartWriter(ctx, obsrepo.NewCallLogRepository(db))

	return collector, db
}

// newRejectionContext 构造一个"已被配额闸门拒绝"的 gin 上下文：先由 WriteQuotaRejected 写状态码与
// 配额事实，再由 UsageCollector 采集，最终落库。
func newRejectionContext(t *testing.T, collector *gwobs.Collector, db *gorm.DB, violation corequota.QuotaViolation, requestID, model string) obsmodel.CallLog {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("request_id", requestID)
	c.Set("key_name", "gw-test-key")
	c.Set("key_type", "personal")
	c.Set("user_id", "u-quota-obs")
	c.Set("user_name", "配额观测测试用户")
	c.Set("model", model)
	c.Set("identity", &struct{}{})

	quota.WriteQuotaRejected(c, violation)

	event := collector.NewUsageEvent(c)
	event.Status = w.Code
	collector.Emit(event)

	// 异步落库，等批写完成后取回该行。
	return waitCallLog(t, db, requestID)
}

func waitCallLog(t *testing.T, db *gorm.DB, requestID string) obsmodel.CallLog {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var row obsmodel.CallLog
		err := db.Where("request_id = ?", requestID).First(&row).Error
		if err == nil {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("call_logs 未在 5s 内写入 request_id=%s: %v", requestID, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestQuotaRejectionTelemetry_ConcurrencyGate 并发闸拒绝的落库事实。
func TestQuotaRejectionTelemetry_ConcurrencyGate(t *testing.T) {
	collector, db := quotaTelemetryCollector(t, "quota_telemetry_conc")
	row := newRejectionContext(t, collector, db, corequota.QuotaViolation{
		SubjectType: "user", SubjectID: "u-quota-obs",
		Layer: "subject", Gate: "concurrency", Limit: 20,
	}, "req-conc", "gpt-4o")

	require.Equal(t, http.StatusTooManyRequests, row.Status)
	require.Equal(t, "concurrency", row.QuotaGate, "应能从数据库区分出并发闸")
	require.Equal(t, "subject", row.QuotaLayer, "应记录生效的配额层")
	require.NotNil(t, row.QuotaLimit, "应记录该次判定的限额")
	require.Equal(t, 20, *row.QuotaLimit)
}

// TestQuotaRejectionTelemetry_RPMGate RPM 闸拒绝的落库事实（与并发闸可区分）。
func TestQuotaRejectionTelemetry_RPMGate(t *testing.T) {
	collector, db := quotaTelemetryCollector(t, "quota_telemetry_rpm")
	row := newRejectionContext(t, collector, db, corequota.QuotaViolation{
		SubjectType: "user", SubjectID: "u-quota-obs",
		Layer: "subject", Gate: "rpm", Limit: 60,
	}, "req-rpm", "gpt-4o")

	require.Equal(t, http.StatusTooManyRequests, row.Status)
	require.Equal(t, "rpm", row.QuotaGate, "应能从数据库区分出 RPM 闸")
	require.Equal(t, "subject", row.QuotaLayer)
	require.NotNil(t, row.QuotaLimit)
	require.Equal(t, 60, *row.QuotaLimit)
}

// TestQuotaRejectionTelemetry_GroupAndModelLayers 组层/模型层拒绝分别落到 quota_layer。
func TestQuotaRejectionTelemetry_GroupAndModelLayers(t *testing.T) {
	cases := []struct {
		name      string
		violation corequota.QuotaViolation
	}{
		{"group-concurrency", corequota.QuotaViolation{Layer: "group", Gate: "concurrency", Limit: 3}},
		{"model-rpm", corequota.QuotaViolation{Layer: "model", Gate: "rpm", Limit: 10}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			collector, db := quotaTelemetryCollector(t, "quota_telemetry_layer_"+tc.name)
			violation := tc.violation
			violation.SubjectType = "user"
			violation.SubjectID = "u-quota-obs"
			row := newRejectionContext(t, collector, db, violation, "req-"+tc.name, "gpt-4o")

			require.Equal(t, tc.violation.Gate, row.QuotaGate)
			require.Equal(t, tc.violation.Layer, row.QuotaLayer)
			require.NotNil(t, row.QuotaLimit)
			require.Equal(t, tc.violation.Limit, *row.QuotaLimit)
		})
	}
}

// TestQuotaRejectionTelemetry_NonRejectedRowHasNoQuotaFacts 未发生配额拒绝的行不应写入配额事实
// （quota_limit 必须是 NULL 而不是 0，避免把"没有配额判定"误读成"限额为 0"）。
func TestQuotaRejectionTelemetry_NonRejectedRowHasNoQuotaFacts(t *testing.T) {
	collector, db := quotaTelemetryCollector(t, "quota_telemetry_plain")

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("request_id", "req-ok")
	c.Set("key_type", "personal")
	c.Set("model", "gpt-4o")
	c.Status(http.StatusOK)

	event := collector.NewUsageEvent(c)
	event.Status = http.StatusOK
	collector.Emit(event)

	row := waitCallLog(t, db, "req-ok")
	require.Empty(t, row.QuotaGate)
	require.Empty(t, row.QuotaLayer)
	require.Nil(t, row.QuotaLimit)
}

// TestQuotaRejectionTelemetry_CollectorReadsFactsFromGinContext 采集器字段解析的独立校验：
// 只以 quota_gate 是否存在为准，显式 0 的限额（该层不限）也必须原样保留。
func TestQuotaRejectionTelemetry_CollectorReadsFactsFromGinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	collector := gwobs.NewCollector(gwobs.DefaultCollectorConfig(), nil)

	rejected, _ := gin.CreateTestContext(httptest.NewRecorder())
	rejected.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rejected.Set("quota_gate", "rpm")
	rejected.Set("quota_layer", "group")
	rejected.Set("quota_limit", 0)
	event := collector.NewUsageEvent(rejected)
	require.Equal(t, "rpm", event.QuotaGate)
	require.Equal(t, "group", event.QuotaLayer)
	require.NotNil(t, event.QuotaLimit)
	require.Equal(t, 0, *event.QuotaLimit)

	plain, _ := gin.CreateTestContext(httptest.NewRecorder())
	plain.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	plainEvent := collector.NewUsageEvent(plain)
	require.Empty(t, plainEvent.QuotaGate)
	require.Empty(t, plainEvent.QuotaLayer)
	require.Nil(t, plainEvent.QuotaLimit)

	callLog, err := event.ToCallLog()
	require.NoError(t, err)
	require.Equal(t, "rpm", callLog.QuotaGate)
	require.Equal(t, "group", callLog.QuotaLayer)
	require.NotNil(t, callLog.QuotaLimit)
	require.Equal(t, 0, *callLog.QuotaLimit)
}

// TestQuotaRejectionTelemetry_RejectedResponseUnchanged 纯观测：429 状态码与响应体字段不变。
func TestQuotaRejectionTelemetry_RejectedResponseUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	quota.WriteQuotaRejected(c, corequota.QuotaViolation{
		SubjectType: "user", SubjectID: "u-quota-obs",
		Layer: "subject", Gate: "concurrency", Limit: 20,
	})

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Equal(t, "1", w.Header().Get("Retry-After"))
	require.Equal(t, "20", w.Header().Get("X-RateLimit-Limit"))
	require.Equal(t, "0", w.Header().Get("X-RateLimit-Remaining"))
	require.True(t, c.IsAborted(), "配额拒绝必须终止 handler 链")
	require.Contains(t, w.Body.String(), "quota_concurrency_exceeded")
	require.Contains(t, w.Body.String(), `"gate":"concurrency"`)
	require.Contains(t, w.Body.String(), `"layer":"subject"`)
	require.Contains(t, w.Body.String(), `"limit":20`)
}

// TestQuotaRejectionTelemetry_FactsComeFromViolation 关键约束：三个落库值必须取自本次判定的
// QuotaViolation（v.Gate / v.Layer / v.Limit），不能从响应体/HTTP 头/事后推断取值。
//
// 手法：故意让限额与其他来源可能给出的值互不相同（Limit=7，而 layer 字面量 "group" 不是数字、
// Retry-After 只会是 1 或 60），若实现改成从响应头/响应体反推，断言必然失败。
func TestQuotaRejectionTelemetry_FactsComeFromViolation(t *testing.T) {
	const limit = 7
	violation := corequota.QuotaViolation{
		SubjectType: "service_key", SubjectID: "key-obs",
		Layer: "group", Gate: "rpm", Limit: limit,
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("request_id", "req-source")
	c.Set("key_type", "service")

	quota.WriteQuotaRejected(c, violation)

	// gin context 里的三值必须逐字等于 violation 字段。
	require.Equal(t, violation.Gate, c.GetString("quota_gate"))
	require.Equal(t, violation.Layer, c.GetString("quota_layer"))
	limitVal, exists := c.Get("quota_limit")
	require.True(t, exists)
	require.Equal(t, limit, limitVal)

	// 与其它来源可区分：limit 既不等于 Retry-After（rpm 闸为 60），也不等于 X-RateLimit-Remaining。
	require.NotEqual(t, w.Header().Get("Retry-After"), "7")
	require.Equal(t, "60", w.Header().Get("Retry-After"), "rpm 闸的 Retry-After 为 60")
	require.NotEqual(t, w.Header().Get("X-RateLimit-Remaining"), "7")

	// 端到端：经采集器落到 call_logs 的仍是 violation 的值。
	collector, db := quotaTelemetryCollector(t, "quota_telemetry_source")
	event := collector.NewUsageEvent(c)
	event.Status = w.Code
	collector.Emit(event)
	row := waitCallLog(t, db, "req-source")
	require.Equal(t, http.StatusTooManyRequests, row.Status)
	require.Equal(t, "rpm", row.QuotaGate)
	require.Equal(t, "group", row.QuotaLayer)
	require.NotNil(t, row.QuotaLimit)
	require.Equal(t, limit, *row.QuotaLimit)
}
