package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

func setupQueryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}, &obsmodel.UsageDaily{}))
	return db
}

func seedUsageDaily(t *testing.T, db *gorm.DB, rows []obsmodel.UsageDaily) {
	t.Helper()
	for _, r := range rows {
		require.NoError(t, db.Create(&r).Error)
	}
}

var testDate = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

func TestQueryService_ManagerSeeAll(t *testing.T) {
	db := setupQueryTestDB(t)
	svc := NewRollupService(db)

	seedUsageDaily(t, db, []obsmodel.UsageDaily{
		{StatDate: testDate, KeyID: "k1", DeptID: "dept-a", Model: "gpt-4o", ReqCount: 5, EstCostUSD: 1.0},
		{StatDate: testDate, KeyID: "k2", DeptID: "dept-b", Model: "gpt-4o", ReqCount: 3, EstCostUSD: 0.5},
	})

	resp, err := svc.Query(context.Background(), UsageDailyQueryParams{
		From:    testDate,
		To:      testDate.Add(24 * time.Hour),
		GroupBy: "day",
		// No scope filter = admin sees all
	})
	require.NoError(t, err)
	require.Len(t, resp.Rows, 1) // grouped by day → 1 row
	assert.Equal(t, int64(8), resp.Total.ReqCount)
}

func TestQueryService_EmployeeScopedByUser(t *testing.T) {
	db := setupQueryTestDB(t)
	svc := NewRollupService(db)

	seedUsageDaily(t, db, []obsmodel.UsageDaily{
		{StatDate: testDate, KeyID: "k1", UserID: "user-1", DeptID: "dept-a", Model: "gpt-4o", ReqCount: 5, EstCostUSD: 1.0},
		{StatDate: testDate, KeyID: "k2", UserID: "user-2", DeptID: "dept-a", Model: "gpt-4o", ReqCount: 3, EstCostUSD: 0.5},
	})

	resp, err := svc.Query(context.Background(), UsageDailyQueryParams{
		From:       testDate,
		To:         testDate.Add(24 * time.Hour),
		GroupBy:    "key",
		UserID:     "user-1",
		ScopeKey:   "user_id = 'user-1'",
	})
	require.NoError(t, err)
	require.Len(t, resp.Rows, 1)
	assert.Equal(t, int64(5), resp.Total.ReqCount)
}

func TestQueryService_DeptHead(t *testing.T) {
	db := setupQueryTestDB(t)
	svc := NewRollupService(db)

	seedUsageDaily(t, db, []obsmodel.UsageDaily{
		{StatDate: testDate, KeyID: "k1", UserID: "u1", DeptID: "dept-a", Model: "gpt-4o", ReqCount: 5, EstCostUSD: 1.0},
		{StatDate: testDate, KeyID: "k2", UserID: "u2", DeptID: "dept-b", Model: "gpt-4o", ReqCount: 3, EstCostUSD: 0.5},
	})

	resp, err := svc.Query(context.Background(), UsageDailyQueryParams{
		From:     testDate,
		To:       testDate.Add(24 * time.Hour),
		GroupBy:  "dept",
		ScopeKey: "dept_id = 'dept-a'",
	})
	require.NoError(t, err)
	require.Len(t, resp.Rows, 1)
	assert.Equal(t, int64(5), resp.Total.ReqCount)
}

func TestQueryService_EmptyResult(t *testing.T) {
	db := setupQueryTestDB(t)
	svc := NewRollupService(db)

	resp, err := svc.Query(context.Background(), UsageDailyQueryParams{
		From:    testDate,
		To:      testDate.Add(24 * time.Hour),
		GroupBy: "day",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Rows)
	assert.Nil(t, resp.Total)
}

func TestQueryService_FilterByModel(t *testing.T) {
	db := setupQueryTestDB(t)
	svc := NewRollupService(db)

	seedUsageDaily(t, db, []obsmodel.UsageDaily{
		{StatDate: testDate, KeyID: "k1", DeptID: "dept-a", Model: "gpt-4o", ReqCount: 5, EstCostUSD: 1.0},
		{StatDate: testDate, KeyID: "k2", DeptID: "dept-a", Model: "claude-3", ReqCount: 3, EstCostUSD: 0.5},
	})

	resp, err := svc.Query(context.Background(), UsageDailyQueryParams{
		From:    testDate,
		To:      testDate.Add(24 * time.Hour),
		GroupBy: "model",
		Model:   "gpt-4o",
	})
	require.NoError(t, err)
	require.Len(t, resp.Rows, 1)
	assert.Equal(t, int64(5), resp.Total.ReqCount)
}