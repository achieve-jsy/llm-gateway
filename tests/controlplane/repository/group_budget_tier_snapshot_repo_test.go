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

func TestGroupBudgetTierSnapshotKeepsArchivedTierMappingDisabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:group_tier_snapshot?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE model_tiers (
		id text PRIMARY KEY, name text NOT NULL, enabled boolean NOT NULL, archived_at datetime NULL
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE model_groups (
		id integer PRIMARY KEY, name text NOT NULL, budget_tier_id text NULL, deleted_at datetime NULL
	)`).Error)

	tierID := uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO model_tiers(id,name,enabled,archived_at) VALUES (?,?,?,?)`,
		tierID.String(), "历史高级", true, time.Now()).Error)
	require.NoError(t, db.Exec(`INSERT INTO model_groups(id,name,budget_tier_id) VALUES (?,?,?)`,
		1, "legacy-opus", tierID.String()).Error)

	rows, err := repository.NewGroupRepo(db).ListBudgetTierGroupSnapshots(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "legacy-opus", rows[0].GroupName)
	require.Equal(t, tierID, rows[0].TierID)
	require.False(t, rows[0].TierEnabled)
}
