package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openModelTierTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TABLE model_tiers (
			id text PRIMARY KEY,
			name text NOT NULL,
			description text NOT NULL DEFAULT '',
			color text NOT NULL,
			sort_order integer NOT NULL DEFAULT 0,
			enabled boolean NOT NULL DEFAULT true,
			archived_at datetime NULL,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL
		)
	`).Error)
	return db
}

func TestModelTierRepositoryCreatesAndReadsTier(t *testing.T) {
	repo := repository.NewModelTierRepo(openModelTierTestDB(t))
	tier := &model.ModelTier{
		ID: uuid.New(), Name: "高级", Color: "#7C3AED", Enabled: true,
	}
	require.NoError(t, repo.Create(context.Background(), tier))

	got, err := repo.FindByID(context.Background(), tier.ID)
	require.NoError(t, err)
	require.Equal(t, "高级", got.Name)
	require.True(t, got.Enabled)
}

func openModelTierTestDBWithAllTables(t *testing.T) *gorm.DB {
	t.Helper()
	db := openModelTierTestDB(t)
	// Create all related tables for ReferenceCounts tests
	require.NoError(t, db.Exec(`
		CREATE TABLE model_groups (
			id integer PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			deleted_at datetime NULL,
			name text NOT NULL,
			description text,
			model_type text NOT NULL DEFAULT 'chat',
			strategy text NOT NULL DEFAULT 'round_robin',
			enabled boolean NOT NULL DEFAULT true,
			max_switches integer NOT NULL DEFAULT 2,
			same_model_retries integer NOT NULL DEFAULT 1,
			failover_cooldown_seconds integer NOT NULL DEFAULT 60,
			timeout_seconds integer NOT NULL DEFAULT 0,
			open_mode text NOT NULL DEFAULT 'public',
			is_capability_override boolean NOT NULL DEFAULT false,
			override_supports_vision boolean NOT NULL DEFAULT false,
			override_supports_function_calling boolean NOT NULL DEFAULT false,
			override_supports_tool_choice boolean NOT NULL DEFAULT false,
			quota_rpm integer,
			quota_max_concurrency integer,
			budget_tier_id text
		)
	`).Error)
	require.NoError(t, db.Exec(`
		CREATE TABLE budget_tier_limits (
			id text PRIMARY KEY,
			budget_allocation_id text NOT NULL,
			model_tier_id text NOT NULL,
			period_type text NOT NULL,
			base_limit_usd text NOT NULL,
			enabled boolean NOT NULL DEFAULT true,
			effective_at datetime,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL
		)
	`).Error)
	require.NoError(t, db.Exec(`
		CREATE TABLE budget_temporary_overrides (
			id text PRIMARY KEY,
			budget_tier_limit_id text NOT NULL,
			model_tier_id text NOT NULL,
			target_usd text NOT NULL,
			effective_at datetime NOT NULL,
			expires_at datetime NOT NULL,
			created_at datetime NOT NULL
		)
	`).Error)
	require.NoError(t, db.Exec(`
		CREATE TABLE budget_limit_schedules (
			id text PRIMARY KEY,
			budget_tier_limit_id text NOT NULL,
			model_tier_id text NOT NULL,
			target_usd text NOT NULL,
			effective_at datetime NOT NULL,
			created_at datetime NOT NULL
		)
	`).Error)
	require.NoError(t, db.Exec(`
		CREATE TABLE budget_alert_records (
			id text PRIMARY KEY,
			model_tier_id text NOT NULL,
			alert_type text NOT NULL,
			severity text NOT NULL,
			created_at datetime NOT NULL
		)
	`).Error)
	require.NoError(t, db.Exec(`
		CREATE TABLE expansion_requests (
			id text PRIMARY KEY,
			model_tier_id text NOT NULL,
			requester_id text NOT NULL,
			amount_usd text NOT NULL,
			status text NOT NULL,
			created_at datetime NOT NULL
		)
	`).Error)
	require.NoError(t, db.Exec(`
		CREATE TABLE call_logs (
			id text PRIMARY KEY,
			budget_tier_id text,
			model_name text NOT NULL,
			tokens_used integer NOT NULL,
			cost_usd text NOT NULL,
			created_at datetime NOT NULL
		)
	`).Error)
	return db
}

func TestModelTierRepoReferenceCountsReturnsCorrectCountsForModelGroups(t *testing.T) {
	db := openModelTierTestDBWithAllTables(t)
	repo := repository.NewModelTierRepo(db)
	tierID := uuid.New()

	// Insert 3 model groups referencing this tier via budget_tier_id
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Exec(
			"INSERT INTO model_groups (id, created_at, updated_at, name, budget_tier_id) VALUES (?, ?, ?, ?, ?)",
			i+1, time.Now(), time.Now(), "group-"+string(rune('A'+i)), tierID,
		).Error)
	}

	// Insert 2 model groups referencing a different tier (should not count)
	otherTierID := uuid.New()
	require.NoError(t, db.Exec(
		"INSERT INTO model_groups (id, created_at, updated_at, name, budget_tier_id) VALUES (?, ?, ?, ?, ?)",
		100, time.Now(), time.Now(), "other-group", otherTierID,
	).Error)

	counts, err := repo.ReferenceCounts(context.Background(), tierID)
	require.NoError(t, err)
	require.Equal(t, int64(3), counts.Groups, "should count model_groups rows where budget_tier_id matches")
	require.Equal(t, int64(0), counts.Limits)
	require.Equal(t, int64(0), counts.Overrides)
	require.Equal(t, int64(0), counts.Schedules)
	require.Equal(t, int64(0), counts.Alerts)
	require.Equal(t, int64(0), counts.Expansions)
	require.Equal(t, int64(0), counts.CallLogs)
	require.Equal(t, int64(3), counts.Total())
}

func TestModelTierRepoReferenceCountsReturnsCorrectCountsForCallLogs(t *testing.T) {
	db := openModelTierTestDBWithAllTables(t)
	repo := repository.NewModelTierRepo(db)
	tierID := uuid.New()

	// Insert 5 call logs referencing this tier via budget_tier_id
	for i := 0; i < 5; i++ {
		require.NoError(t, db.Exec(
			"INSERT INTO call_logs (id, budget_tier_id, model_name, tokens_used, cost_usd, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			uuid.New().String(), tierID, "gpt-4", 100+i, "0.01", time.Now(),
		).Error)
	}

	// Insert call logs with NULL budget_tier_id (should not count)
	require.NoError(t, db.Exec(
		"INSERT INTO call_logs (id, budget_tier_id, model_name, tokens_used, cost_usd, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		uuid.New().String(), nil, "gpt-3.5", 50, "0.001", time.Now(),
	).Error)

	counts, err := repo.ReferenceCounts(context.Background(), tierID)
	require.NoError(t, err)
	require.Equal(t, int64(0), counts.Groups)
	require.Equal(t, int64(0), counts.Limits)
	require.Equal(t, int64(0), counts.Overrides)
	require.Equal(t, int64(0), counts.Schedules)
	require.Equal(t, int64(0), counts.Alerts)
	require.Equal(t, int64(0), counts.Expansions)
	require.Equal(t, int64(5), counts.CallLogs, "should count call_logs rows where budget_tier_id matches")
	require.Equal(t, int64(5), counts.Total())
}

func TestModelTierRepoReferenceCountsDoesNotErrorOnAnyOfThe7Tables(t *testing.T) {
	db := openModelTierTestDBWithAllTables(t)
	repo := repository.NewModelTierRepo(db)
	tierID := uuid.New()

	counts, err := repo.ReferenceCounts(context.Background(), tierID)
	require.NoError(t, err, "ReferenceCounts should not error even when all tables are empty")
	require.Equal(t, int64(0), counts.Total(), "all counts should be zero for a tier with no references")
}

func TestModelTierRepoReferenceCountsAggregatesAcrossAllTables(t *testing.T) {
	db := openModelTierTestDBWithAllTables(t)
	repo := repository.NewModelTierRepo(db)
	tierID := uuid.New()

	// 2 model_groups
	require.NoError(t, db.Exec(
		"INSERT INTO model_groups (id, created_at, updated_at, name, budget_tier_id) VALUES (?, ?, ?, ?, ?)",
		1, time.Now(), time.Now(), "group-1", tierID,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO model_groups (id, created_at, updated_at, name, budget_tier_id) VALUES (?, ?, ?, ?, ?)",
		2, time.Now(), time.Now(), "group-2", tierID,
	).Error)

	// 1 budget_tier_limit
	require.NoError(t, db.Exec(
		"INSERT INTO budget_tier_limits (id, budget_allocation_id, model_tier_id, period_type, base_limit_usd, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		uuid.New().String(), uuid.New().String(), tierID, "monthly", "100.000000", true, time.Now(), time.Now(),
	).Error)

	// 1 budget_temporary_override
	require.NoError(t, db.Exec(
		"INSERT INTO budget_temporary_overrides (id, budget_tier_limit_id, model_tier_id, target_usd, effective_at, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		uuid.New().String(), uuid.New().String(), tierID, "150.000000", time.Now(), time.Now().Add(time.Hour*24), time.Now(),
	).Error)

	// 1 budget_limit_schedule
	require.NoError(t, db.Exec(
		"INSERT INTO budget_limit_schedules (id, budget_tier_limit_id, model_tier_id, target_usd, effective_at, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		uuid.New().String(), uuid.New().String(), tierID, "120.000000", time.Now().Add(time.Hour*24), time.Now(),
	).Error)

	// 2 budget_alert_records
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Exec(
			"INSERT INTO budget_alert_records (id, model_tier_id, alert_type, severity, created_at) VALUES (?, ?, ?, ?, ?)",
			uuid.New().String(), tierID, "threshold", "warning", time.Now(),
		).Error)
	}

	// 1 expansion_request
	require.NoError(t, db.Exec(
		"INSERT INTO expansion_requests (id, model_tier_id, requester_id, amount_usd, status, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		uuid.New().String(), tierID, "user-1", "50.000000", "pending", time.Now(),
	).Error)

	// 3 call_logs
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Exec(
			"INSERT INTO call_logs (id, budget_tier_id, model_name, tokens_used, cost_usd, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			uuid.New().String(), tierID, "gpt-4", 100, "0.01", time.Now(),
		).Error)
	}

	counts, err := repo.ReferenceCounts(context.Background(), tierID)
	require.NoError(t, err)
	require.Equal(t, int64(2), counts.Groups, "model_groups count")
	require.Equal(t, int64(1), counts.Limits, "budget_tier_limits count")
	require.Equal(t, int64(1), counts.Overrides, "budget_temporary_overrides count")
	require.Equal(t, int64(1), counts.Schedules, "budget_limit_schedules count")
	require.Equal(t, int64(2), counts.Alerts, "budget_alert_records count")
	require.Equal(t, int64(1), counts.Expansions, "expansion_requests count")
	require.Equal(t, int64(3), counts.CallLogs, "call_logs count")
	require.Equal(t, int64(11), counts.Total(), "total reference count")
}

func TestModelTierRepoListFlowWorksWithReferenceCounts(t *testing.T) {
	db := openModelTierTestDBWithAllTables(t)
	repo := repository.NewModelTierRepo(db)

	// Create two tiers
	tier1 := &model.ModelTier{ID: uuid.New(), Name: "免费", Color: "#10B981", Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	tier2 := &model.ModelTier{ID: uuid.New(), Name: "高级", Color: "#7C3AED", Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, repo.Create(context.Background(), tier1))
	require.NoError(t, repo.Create(context.Background(), tier2))

	// tier1 has 2 model_groups
	require.NoError(t, db.Exec(
		"INSERT INTO model_groups (id, created_at, updated_at, name, budget_tier_id) VALUES (?, ?, ?, ?, ?)",
		1, time.Now(), time.Now(), "group-1", tier1.ID,
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO model_groups (id, created_at, updated_at, name, budget_tier_id) VALUES (?, ?, ?, ?, ?)",
		2, time.Now(), time.Now(), "group-2", tier1.ID,
	).Error)

	// tier2 has 1 call_log
	require.NoError(t, db.Exec(
		"INSERT INTO call_logs (id, budget_tier_id, model_name, tokens_used, cost_usd, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		uuid.New().String(), tier2.ID, "gpt-4", 100, "0.01", time.Now(),
	).Error)

	// List tiers and verify reference counts are populated
	tiers, err := repo.List(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, tiers, 2)

	// Verify tier1 counts
	counts1, err := repo.ReferenceCounts(context.Background(), tier1.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), counts1.Groups)
	require.Equal(t, int64(2), counts1.Total())

	// Verify tier2 counts
	counts2, err := repo.ReferenceCounts(context.Background(), tier2.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), counts2.CallLogs)
	require.Equal(t, int64(1), counts2.Total())
}
