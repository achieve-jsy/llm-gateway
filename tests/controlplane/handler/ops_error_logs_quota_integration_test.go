//go:build integration

package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	obsrepo "github.com/example/llm-gateway/internal/gateway/observability/repository"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 真实 Postgres 集成测试：验证"增强错误日志"端点能把配额拒绝事实直接呈现给运维
// —— 不搜不猜即可区分并发闸/ RPM 闸，并看到该次判定的限额。
//
// 依赖：TEST_DATABASE_URL 指向已执行过 atlas 迁移的库（call_logs 含 quota_* 三列），
// 例如 postgres://user@127.0.0.1:5432/<scratch>?sslmode=disable。
func TestListErrorLogsExposesQuotaRejectionFacts(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)

	// 用写入端（gateway 侧 call_logs 批量落库链路）真实写入两行配额拒绝。
	writer := obsrepo.NewCallLogRepository(db)
	// 用 UTC：call_logs.ts 是 timestamptz，PG 侧 NOW() 按服务器时区求值；
	// 用本地 time.Now() 在 UTC+8 机器上会写成"未来时间"，导致 ts >= NOW() - INTERVAL '1h' 匹配不到。
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`DELETE FROM call_logs WHERE request_id LIKE 'errlog-quota-%'`).Error)
	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM call_logs WHERE request_id LIKE 'errlog-quota-%'`).Error
	})

	limitConcurrency, limitRPM := 20, 60
	rows := []obsmodel.CallLog{
		{
			RequestID: "errlog-quota-conc", TS: now, KeyType: obsmodel.KeyTypePersonal,
			UserID: "u-errlog", UserName: "配额错误日志测试", Model: "gpt-4o", Status: http.StatusTooManyRequests,
			QuotaGate: "concurrency", QuotaLayer: "subject", QuotaLimit: &limitConcurrency,
		},
		{
			RequestID: "errlog-quota-rpm", TS: now, KeyType: obsmodel.KeyTypePersonal,
			UserID: "u-errlog", UserName: "配额错误日志测试", Model: "gpt-4o", Status: http.StatusTooManyRequests,
			QuotaGate: "rpm", QuotaLayer: "subject", QuotaLimit: &limitRPM,
		},
	}
	require.NoError(t, writer.BatchInsert(t.Context(), rows))

	ops := handler.NewOpsHandler(db, nil, nil)
	gin.SetMode(gin.TestMode)

	type entry struct {
		RequestID    string  `json:"request_id"`
		ErrorMessage string  `json:"error_message"`
		QuotaGate    *string `json:"quota_gate"`
		QuotaLayer   *string `json:"quota_layer"`
		QuotaLimit   *int    `json:"quota_limit"`
	}
	listErrorLogs := func(t *testing.T, query string) ([]entry, int) {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/ops/error-logs?"+query, nil)
		ops.ListErrorLogs(c)
		require.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Items []entry `json:"items"`
			Total int     `json:"total"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return resp.Items, resp.Total
	}

	// 1) 精确匹配 request_id 前缀，两行应直接带着 gate / layer / limit 返回（无需反推流量）。
	items, total := listErrorLogs(t, "time_range=1h&page_size=200&search=errlog-quota-conc")
	require.Equal(t, 1, total)
	require.Len(t, items, 1)

	conc := items[0]
	require.Equal(t, "errlog-quota-conc", conc.RequestID)
	require.NotNil(t, conc.QuotaGate)
	require.Equal(t, "concurrency", *conc.QuotaGate)
	require.NotNil(t, conc.QuotaLayer)
	require.Equal(t, "subject", *conc.QuotaLayer)
	require.NotNil(t, conc.QuotaLimit)
	require.Equal(t, 20, *conc.QuotaLimit)
	require.Equal(t, "配额拒绝: concurrency / subject (limit=20)", conc.ErrorMessage)

	items, total = listErrorLogs(t, "time_range=1h&page_size=200&search=errlog-quota-rpm")
	require.Equal(t, 1, total)
	require.Len(t, items, 1)
	rpm := items[0]
	require.NotNil(t, rpm.QuotaGate)
	require.Equal(t, "rpm", *rpm.QuotaGate)
	require.NotNil(t, rpm.QuotaLimit)
	require.Equal(t, 60, *rpm.QuotaLimit)
	require.Equal(t, "配额拒绝: rpm / subject (limit=60)", rpm.ErrorMessage)

	// 2) 按闸门搜索（运维入口）：quota_gate 参与 search 匹配。
	_, gateTotal := listErrorLogs(t, "time_range=1h&page_size=200&search=concurrency")
	require.GreaterOrEqual(t, gateTotal, 1, "按 gate 关键字搜索应能命中并发闸行")

	// 3) 两个闸门的行可区分：并发闸搜索不应把 RPM 闸的行算进来（该行 request_id 不含 concurrency）。
	gateItems, _ := listErrorLogs(t, "time_range=1h&page_size=200&search=concurrency")
	for _, item := range gateItems {
		require.NotEqual(t, "errlog-quota-rpm", item.RequestID)
	}
}
