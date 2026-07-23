//go:build integration

package observability_test

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	corequota "github.com/example/llm-gateway/internal/core/quota"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsrepo "github.com/example/llm-gateway/internal/gateway/observability/repository"
	"github.com/example/llm-gateway/internal/gateway/quota"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 真实 Postgres 端到端：配额拒绝 → call_logs.quota_gate/quota_layer/quota_limit 落库并可直接查询。
func TestQuotaRejectionTelemetryRealPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	// 直接使用真实 schema（已在目标库执行过 atlas 迁移）；不在此建表。
	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM call_logs WHERE request_id LIKE 'e2e-quota-%'`).Error
	})

	cfg := gwobs.DefaultCollectorConfig()
	cfg.BufferSize = 8
	cfg.FlushN = 1
	cfg.FlushInterval = 5 * time.Millisecond
	collector := gwobs.NewCollector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector.StartWriter(ctx, obsrepo.NewCallLogRepository(db))

	cases := []struct {
		requestID string
		gate      string
		layer     string
		limit     int
	}{
		{"e2e-quota-conc", "concurrency", "subject", 20},
		{"e2e-quota-rpm", "rpm", "subject", 60},
		{"e2e-quota-model", "concurrency", "model", 4},
	}
	for _, tc := range cases {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		c.Set("request_id", tc.requestID)
		c.Set("key_type", "personal")
		c.Set("model", "gpt-4o")
		quota.WriteQuotaRejected(c, corequota.QuotaViolation{
			SubjectType: "user", SubjectID: "u-e2e", Layer: tc.layer, Gate: tc.gate, Limit: tc.limit,
		})
		event := collector.NewUsageEvent(c)
		event.Status = w.Code
		collector.Emit(event)
	}

	time.Sleep(400 * time.Millisecond)

	type row struct {
		RequestID  string `gorm:"column:request_id"`
		Status     int    `gorm:"column:status"`
		QuotaGate  *string `gorm:"column:quota_gate"`
		QuotaLayer *string `gorm:"column:quota_layer"`
		QuotaLimit *int    `gorm:"column:quota_limit"`
	}
	var rows []row
	require.NoError(t, db.Raw(`SELECT request_id, status, quota_gate, quota_layer, quota_limit
		FROM call_logs WHERE request_id LIKE 'e2e-quota-%' ORDER BY request_id`).Scan(&rows).Error)
	require.Len(t, rows, 3)
	byID := make(map[string]row, len(rows))
	for _, r := range rows {
		byID[r.RequestID] = r
		t.Logf("DB row: %s status=%d gate=%v layer=%v limit=%v", r.RequestID, r.Status, *r.QuotaGate, *r.QuotaLayer, *r.QuotaLimit)
	}
	for _, tc := range cases {
		r, ok := byID[tc.requestID]
		require.True(t, ok, "缺少行 %s", tc.requestID)
		require.Equal(t, 429, r.Status)
		require.Equal(t, tc.gate, *r.QuotaGate)
		require.Equal(t, tc.layer, *r.QuotaLayer)
		require.Equal(t, tc.limit, *r.QuotaLimit)
	}

	// 确认"未发生配额拒绝"的行为独立于本次改造：同一表里不存在 quota_gate 为空的干扰事实
	var nulls int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM call_logs WHERE request_id LIKE 'e2e-quota-%' AND quota_gate IS NULL`).Scan(&nulls).Error)
	require.Zero(t, nulls)

	_ = obsmodel.CallLog{}
}
