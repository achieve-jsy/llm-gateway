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

func TestTierUsageCountsOnlyEligibleRowsForAccountingUsage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tier_usage?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		request_id text primary key, ts datetime, budget_tier_id text, est_cost_usd numeric,
		budget_tier_accounting_eligible boolean, status integer, studio_id text, dept_id text,
		user_id text, key_type text, group_name text
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE model_groups (id integer primary key, name text)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO model_groups(id,name) VALUES (42,'gpt-5')`).Error)
	tierID := uuid.New()
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`INSERT INTO call_logs VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		"tier-1", now, tierID.String(), 1.25, true, 200, "", "dept-a", "member-a", "personal", "gpt-5").Error)
	require.NoError(t, db.Exec(`INSERT INTO call_logs VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		"tier-2", now, tierID.String(), 9, false, 200, "", "dept-a", "member-a", "personal", "gpt-5").Error)
	require.NoError(t, db.Exec(`INSERT INTO call_logs VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		"tier-failed", now, tierID.String(), 100, true, 500, "", "dept-a", "member-a", "personal", "gpt-5").Error)
	groupID := uint(42)
	got, err := repository.NewCallLogRepo(db).SumTierUsage(context.Background(), repository.TierUsageQuery{
		ScopeType: "dept_daily", ScopeID: "dept-a", TierID: &tierID, GroupID: &groupID,
		StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "10.250000", got[0].ReportedCostUSD.String())
	require.Equal(t, "1.250000", got[0].AccountingCostUSD.String())
	require.Equal(t, groupID, *got[0].GroupID)
}

func TestTierUsageKeepsReportedCostWhenNoRowsAreAccountingEligible(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tier_usage_reported_only?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		request_id text primary key, ts datetime, budget_tier_id text, est_cost_usd numeric,
		budget_tier_accounting_eligible boolean, status integer, studio_id text, dept_id text,
		user_id text, key_type text, group_name text
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE model_groups (id integer primary key, name text)`).Error)
	tierID := uuid.New()
	now := time.Now().UTC()
	for _, row := range []struct {
		id   string
		cost float64
	}{
		{"reported-a", 3.25},
		{"reported-b", 4.75},
	} {
		require.NoError(t, db.Exec(`INSERT INTO call_logs VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			row.id, now, tierID.String(), row.cost, false, 200, "", "dept-a", "member-a", "personal", "").Error)
	}

	got, err := repository.NewCallLogRepo(db).SumTierUsage(context.Background(), repository.TierUsageQuery{
		ScopeType: "dept_daily", ScopeID: "dept-a", TierID: &tierID,
		StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "8.000000", got[0].ReportedCostUSD.String())
	require.Equal(t, "0.000000", got[0].AccountingCostUSD.String())
	require.Equal(t, int64(2), got[0].RequestCount)
}

func TestTierUsageSeparatesPersonalAndServiceDepartmentPools(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tier_usage_key_type?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		request_id text primary key, ts datetime, budget_tier_id text, est_cost_usd numeric,
		budget_tier_accounting_eligible boolean, status integer, studio_id text, dept_id text,
		user_id text, key_type text, group_name text
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE model_groups (id integer primary key, name text)`).Error)
	tierID, now := uuid.New(), time.Now().UTC()
	for _, row := range []struct{ id, keyType string }{{"personal", "personal"}, {"service", "service"}} {
		require.NoError(t, db.Exec(`INSERT INTO call_logs VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			row.id, now, tierID.String(), 2, true, 200, "", "dept-a", "member-a", row.keyType, "").Error)
	}
	repo := repository.NewCallLogRepo(db)
	personal, err := repo.SumTierUsage(context.Background(), repository.TierUsageQuery{
		ScopeType: "dept_daily", ScopeID: "dept-a", TierID: &tierID, StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), personal[0].RequestCount)
	serviceRows, err := repo.SumTierUsage(context.Background(), repository.TierUsageQuery{
		ScopeType: "dept_service", ScopeID: "dept-a", TierID: &tierID, StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), serviceRows[0].RequestCount)
}

func TestTotalUsageIncludesSuccessfulRowsRegardlessOfTierEligibility(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:total_usage_eligibility?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		request_id text primary key, ts datetime, budget_tier_id text, est_cost_usd numeric,
		budget_tier_accounting_eligible boolean, status integer, studio_id text, dept_id text,
		user_id text, key_type text, group_name text
	)`).Error)
	now, tierID := time.Now().UTC(), uuid.New()
	for _, row := range []struct {
		id       string
		cost     float64
		eligible bool
		status   int
	}{
		{"eligible", 1.25, true, 200},
		{"not-eligible", 9, false, 200},
		{"failed", 100, false, 500},
	} {
		require.NoError(t, db.Exec(`INSERT INTO call_logs VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			row.id, now, tierID.String(), row.cost, row.eligible, row.status,
			"", "dept-a", "member-a", "personal", "").Error)
	}

	got, err := repository.NewCallLogRepo(db).SumTierUsage(context.Background(), repository.TierUsageQuery{
		ScopeType: "dept_daily", ScopeID: "dept-a", Total: true,
		StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "10.250000", got[0].AccountingCostUSD.String())
	require.Equal(t, int64(2), got[0].RequestCount)
}
