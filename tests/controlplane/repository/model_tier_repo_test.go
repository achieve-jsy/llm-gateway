package repository_test

import (
	"context"
	"testing"

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
