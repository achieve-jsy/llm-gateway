package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
)

func setupRebuildCallLogsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	err = db.Exec(`CREATE TABLE call_logs (
		request_id   TEXT PRIMARY KEY,
		ts           DATETIME NOT NULL,
		key_id       TEXT,
		user_id      TEXT,
		dept_id      TEXT,
		studio_id    TEXT,
		key_type     TEXT NOT NULL DEFAULT 'personal',
		est_cost_usd REAL NOT NULL DEFAULT 0,
		status       INTEGER NOT NULL DEFAULT 200
	)`).Error
	require.NoError(t, err)
	return db
}

func insertCallLog(t *testing.T, db *gorm.DB, reqID, deptID, userID, keyType string, cost float64) {
	t.Helper()
	err := db.Exec(
		`INSERT INTO call_logs (request_id, ts, dept_id, user_id, key_type, est_cost_usd, status)
		 VALUES (?, ?, ?, ?, ?, ?, 200)`,
		reqID, time.Now(), deptID, userID, keyType, cost,
	).Error
	require.NoError(t, err)
}

// TestBudgetService_RebuildCounters_KeyTypeScopes 验证预算重建按 call_logs.key_type 分流：
// personal → dept_daily / member；service → dept_service，且不计入 member。
func TestBudgetService_RebuildCounters_KeyTypeScopes(t *testing.T) {
	db := setupRebuildCallLogsDB(t)
	insertCallLog(t, db, "p1", "dept-1", "user-personal", "personal", 10)
	insertCallLog(t, db, "s1", "dept-1", "user-service", "service", 20)

	rdb, mr := newTestMiniredis(t)
	svc := service.NewBudgetService(newFakeAllocRepo(), newFakeAlertRepo())
	svc.SetRedis(rdb)

	ctx := context.Background()
	result, err := svc.RebuildCounters(ctx, db)
	require.NoError(t, err)
	require.NotNil(t, result)

	month := time.Now().Format("2006-01")

	deptDaily, err := mr.Get(fmt.Sprintf("budget:used:%s:%s:%s", model.BudgetScopeDeptDaily, "dept-1", month))
	require.NoError(t, err)
	assert.Equal(t, "10", deptDaily)

	deptService, err := mr.Get(fmt.Sprintf("budget:used:%s:%s:%s", model.BudgetScopeDeptService, "dept-1", month))
	require.NoError(t, err)
	assert.Equal(t, "20", deptService)

	memberPersonal, err := mr.Get(fmt.Sprintf("budget:used:%s:%s:%s", model.BudgetScopeMember, "user-personal", month))
	require.NoError(t, err)
	assert.Equal(t, "10", memberPersonal)

	_, err = mr.Get(fmt.Sprintf("budget:used:%s:%s:%s", model.BudgetScopeMember, "user-service", month))
	assert.Error(t, err, "service key 用量不应写入 member scope")
}
