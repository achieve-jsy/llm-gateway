package repository_test

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAlertDedupSeparatesPeriodTypeAndTier(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:alert_dimension?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text PRIMARY KEY, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)
	repo := repository.NewBudgetAlertRepo(db)
	require.NoError(t, repo.Create(&model.BudgetAlertRecord{
		ScopeType: "member", ScopeID: "member-a", PeriodType: "monthly", Period: "2026-07",
		Threshold: 80, DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
	}))
	tierID := uuid.New()
	exists, err := repo.ExistsForPeriod(context.Background(), repository.AlertDimension{
		ScopeType: "member", ScopeID: "member-a", PeriodType: budgetdomain.PeriodDaily,
		Period: "2026-07-11", Threshold: 80, DimensionType: budgetdomain.DimensionTotal,
	})
	require.NoError(t, err)
	require.False(t, exists)
	exists, err = repo.ExistsForPeriod(context.Background(), repository.AlertDimension{
		ScopeType: "member", ScopeID: "member-a", PeriodType: budgetdomain.PeriodMonthly,
		Period: "2026-07", Threshold: 80, DimensionType: budgetdomain.DimensionTier, ModelTierID: &tierID,
	})
	require.NoError(t, err)
	require.False(t, exists)
}
