package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBudgetRiskRepoAggregatesP1BudgetBlockFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, created_at datetime
	)`).Error)
	tierID := uuid.New()
	for _, at := range []time.Time{
		time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC),
	} {
		require.NoError(t, db.Exec(`INSERT INTO call_logs VALUES (402, ?, ?, ?, ?, ?, ?, ?)`,
			tierID.String(), "tier_budget_exceeded", "tier", "dept_daily", "d-1", "monthly", at).Error)
	}

	rows, err := repository.NewBudgetRiskRepo(db).AggregateBlockedRequests(context.Background(), repository.BlockedRequestQuery{
		From:       time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		ScopeTypes: []string{"dept_daily"}, ScopeIDs: []string{"d-1"},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(2), rows[0].Count)
	require.Equal(t, "tier_budget_exceeded", rows[0].Code)
	require.Equal(t, tierID, *rows[0].ModelTierID)
}

func TestBudgetRiskRepoListsAlertRecordsWithTierNames(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE model_tiers (
		id text primary key, name text, description text, color text, sort_order integer,
		enabled boolean, archived_at datetime, created_at datetime, updated_at datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)
	tierID, alertID := uuid.New(), uuid.New()
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	require.NoError(t, db.Exec(`INSERT INTO model_tiers
		(id,name,description,color,sort_order,enabled,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, tierID, "高级", "", "#000000", 1, true, now, now).Error)
	require.NoError(t, db.Exec(`INSERT INTO budget_alert_records VALUES (?,?,?,?,?,?,?,?,?,?)`,
		alertID, "dept_daily", "d-1", "monthly", "2026-07", 80,
		"tier:"+tierID.String(), "tier", tierID, now).Error)

	items, err := repository.NewBudgetRiskRepo(db).ListAlertRecords(
		context.Background(), []string{"dept_daily"}, []string{"d-1"}, []string{"monthly"},
	)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].ModelTier)
	require.Equal(t, "高级", items[0].ModelTier.Name)
}
