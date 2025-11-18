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

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Discard,
	})
	require.NoError(t, err)

	err = db.AutoMigrate(&obsmodel.CallLog{}, &obsmodel.UsageDaily{})
	require.NoError(t, err)

	return db
}

func insertCallLog(t *testing.T, db *gorm.DB, log obsmodel.CallLog) {
	t.Helper()
	require.NoError(t, db.Create(&log).Error)
}

func TestRollupService_BasicAggregation(t *testing.T) {
	db := setupTestDB(t)
	svc := NewRollupService(db)

	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// Insert 2 call logs for the day
	insertCallLog(t, db, obsmodel.CallLog{
		RequestID:        "req-001",
		TS:               day.Add(time.Hour),
		KeyID:            "key-a",
		UserID:           "user-1",
		DeptID:           "dept-x",
		Model:            "gpt-4o",
		PromptTokens:     1000,
		CompletionTokens: 200,
		Status:           200,
		EstCostUSD:       0.05,
	})
	insertCallLog(t, db, obsmodel.CallLog{
		RequestID:        "req-002",
		TS:               day.Add(2 * time.Hour),
		KeyID:            "key-a",
		UserID:           "user-1",
		DeptID:           "dept-x",
		Model:            "gpt-4o",
		PromptTokens:     500,
		CompletionTokens: 100,
		Status:           200,
		EstCostUSD:       0.025,
	})

	err := svc.Run(context.Background(), day)
	require.NoError(t, err)

	// Verify usage_daily
	var rows []obsmodel.UsageDaily
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)

	row := rows[0]
	assert.Equal(t, 2, row.ReqCount)
	assert.Equal(t, 2, row.SuccessCount)
	assert.Equal(t, int64(1500), row.PromptTokens)
	assert.Equal(t, int64(300), row.CompletionTokens)
	assert.InDelta(t, 0.075, row.EstCostUSD, 1e-9)
	assert.Equal(t, "gpt-4o", row.Model)
	assert.Equal(t, "key-a", row.KeyID)
	assert.Equal(t, "dept-x", row.DeptID)
}

func TestRollupService_Idempotent(t *testing.T) {
	db := setupTestDB(t)
	svc := NewRollupService(db)

	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	insertCallLog(t, db, obsmodel.CallLog{
		RequestID: "req-001", TS: day.Add(time.Hour),
		KeyID: "key-a", Model: "gpt-4o",
		PromptTokens: 1000, CompletionTokens: 200,
		Status: 200, EstCostUSD: 0.05,
	})

	// Run rollup twice - should be idempotent
	require.NoError(t, svc.Run(context.Background(), day))
	require.NoError(t, svc.Run(context.Background(), day))

	var count int64
	db.Model(&obsmodel.UsageDaily{}).Count(&count)
	assert.Equal(t, int64(1), count)

	var row obsmodel.UsageDaily
	require.NoError(t, db.First(&row).Error)
	assert.Equal(t, 1, row.ReqCount)
}

func TestRollupService_EmptyDay(t *testing.T) {
	db := setupTestDB(t)
	svc := NewRollupService(db)

	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	// No call logs
	err := svc.Run(context.Background(), day)
	require.NoError(t, err)

	var count int64
	db.Model(&obsmodel.UsageDaily{}).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestRollupService_MultipleModels(t *testing.T) {
	db := setupTestDB(t)
	svc := NewRollupService(db)

	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	insertCallLog(t, db, obsmodel.CallLog{
		RequestID: "req-001", TS: day.Add(time.Hour),
		KeyID: "key-a", Model: "gpt-4o",
		PromptTokens: 100, EstCostUSD: 0.01, Status: 200,
	})
	insertCallLog(t, db, obsmodel.CallLog{
		RequestID: "req-002", TS: day.Add(2 * time.Hour),
		KeyID: "key-a", Model: "claude-3",
		PromptTokens: 200, EstCostUSD: 0.02, Status: 200,
	})

	require.NoError(t, svc.Run(context.Background(), day))

	var rows []obsmodel.UsageDaily
	require.NoError(t, db.Find(&rows).Error)
	assert.Len(t, rows, 2)
}