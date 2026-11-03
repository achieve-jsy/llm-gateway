//go:build integration

package observability_test

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsrepo "github.com/example/llm-gateway/internal/gateway/observability/repository"
)

// TestCallTypePersistedToRealPostgres
//
// 测什么：call_type 在**真实 schema**（已执行过 atlas 迁移的库）上确实能整行写入并读回，
// 尤其证明「未识别端点落空串而不是 NULL」这一语义在 Postgres 的 varchar(64) 上成立。
// 怎么测：TEST_DATABASE_URL 指向已迁移的库（本测试**不建表**，用真表）→ 采集侧构造
// 已知端点与未识别端点两个事件 → 落库 → 用裸 SQL 读回 call_type 与 `call_type IS NULL`。
// 期望：已知端点 = 约定取值；未识别端点 = ”（IS NULL=false）。
//
// 运行：TEST_DATABASE_URL=postgres://… go test -tags=integration ./tests/gateway/observability/ -run TestCallTypePersisted
func TestCallTypePersistedToRealPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	// 直接使用真实 schema（已在目标库执行过 atlas 迁移）；不在此建表。
	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM call_logs WHERE request_id LIKE 'a11-call-type-%'`).Error
	})

	cfg := gwobs.DefaultCollectorConfig()
	cfg.BufferSize = 8
	cfg.FlushN = 1
	cfg.FlushInterval = 5 * time.Millisecond
	collector := gwobs.NewCollector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector.StartWriter(ctx, obsrepo.NewCallLogRepository(db))
	time.Sleep(20 * time.Millisecond)

	cases := []struct {
		requestID   string
		method      string
		path        string
		wantCallTyp string
	}{
		{"a11-call-type-chat", "POST", "/v1/chat/completions", "chat"},
		{"a11-call-type-video-create", "POST", "/v1/contents/generations/tasks", "video.create"},
		{"a11-call-type-asset", "POST", "/v1/assets/CreateAssetGroup", "video.asset.CreateAssetGroup"},
		{"a11-call-type-unknown", "POST", "/v1/not-registered", ""},
	}
	for _, tc := range cases {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(tc.method, tc.path, nil)
		c.Set("request_id", tc.requestID)
		c.Set("key_type", "personal")
		c.Set("model", "a11-call-type-group")
		collector.Emit(collector.NewUsageEvent(c))
	}

	for _, tc := range cases {
		require.Eventually(t, func() bool {
			var count int64
			_ = db.Table("call_logs").Where("request_id = ?", tc.requestID).Count(&count).Error
			return count == 1
		}, 3*time.Second, 20*time.Millisecond, "call_logs 未写入 %s", tc.requestID)

		var got struct {
			CallType string
			IsNull   int
		}
		res := db.Raw(
			`SELECT COALESCE(call_type, '') AS call_type, CASE WHEN call_type IS NULL THEN 1 ELSE 0 END AS is_null
			   FROM call_logs WHERE request_id = ?`, tc.requestID,
		).Scan(&got)
		require.NoError(t, res.Error)
		require.Equal(t, int64(1), res.RowsAffected)
		require.Equal(t, tc.wantCallTyp, got.CallType, "request_id=%s path=%s", tc.requestID, tc.path)
		require.Equal(t, 0, got.IsNull, "call_type 必须落 '' 而不是 NULL（request_id=%s）", tc.requestID)
	}
}
