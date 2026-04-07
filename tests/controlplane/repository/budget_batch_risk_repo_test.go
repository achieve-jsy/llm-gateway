package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBatchTierRiskUsesParentTierLimitAndAllSiblingLimits(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:batch_tier_risk?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE budget_allocations (
		id text PRIMARY KEY, scope_type text, scope_id text, amount_usd numeric,
		daily_amount_usd numeric, weekly_amount_usd numeric, enabled boolean, updated_at datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_tier_limits (
		id text PRIMARY KEY, budget_allocation_id text, model_tier_id text, period_type text,
		limit_usd numeric, effective_at datetime, enabled boolean, created_at datetime, updated_at datetime
	)`).Error)

	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	companyID, studioAID, studioBID, tierID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, allocation := range []struct {
		id        uuid.UUID
		scopeType string
		scopeID   string
	}{
		{companyID, model.BudgetScopeCompany, model.BudgetScopeGlobal},
		{studioAID, model.BudgetScopeStudio, "studio-a"},
		{studioBID, model.BudgetScopeStudio, "studio-b"},
	} {
		require.NoError(t, db.Exec(`INSERT INTO budget_allocations VALUES (?,?,?,?,?,?,?,?)`,
			allocation.id.String(), allocation.scopeType, allocation.scopeID, 1000, -1, -1, true, now).Error)
	}
	for _, limit := range []struct {
		allocationID uuid.UUID
		amount       int
	}{
		{companyID, 100},
		{studioAID, 40},
		{studioBID, 50},
	} {
		require.NoError(t, db.Exec(`INSERT INTO budget_tier_limits VALUES (?,?,?,?,?,?,?,?,?)`,
			uuid.NewString(), limit.allocationID.String(), tierID.String(), budgetdomain.PeriodMonthly,
			limit.amount, now.Add(-time.Hour), true, now, now).Error)
	}

	got, err := repository.NewBudgetRepo(db).GetBatchTierRiskSnapshot(context.Background(), model.BudgetAllocation{
		ID: studioAID, ScopeType: model.BudgetScopeStudio, ScopeID: "studio-a",
	}, tierID, budgetdomain.PeriodMonthly)
	require.NoError(t, err)
	require.NotNil(t, got.ParentLimitUSD)
	require.Equal(t, "100.000000", got.ParentLimitUSD.String())
	require.Equal(t, "90.000000", got.SiblingLimitTotalUSD.String())
	require.NotEmpty(t, got.ParentVersion)
}
