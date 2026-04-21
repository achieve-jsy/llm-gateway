package service_test

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGroupBudgetTierBatchAssignmentIsTransactional(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE model_groups (
		id integer PRIMARY KEY, budget_tier_id text NULL, deleted_at datetime NULL
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO model_groups(id) VALUES (1)`).Error)
	repo := repository.NewGroupRepo(db)
	tierID := uuid.New()

	err = repo.SetBudgetTierBatch(context.Background(), []uint{1, 999}, &tierID)
	require.ErrorIs(t, err, repository.ErrGroupNotFound)
	var got *string
	require.NoError(t, db.Raw(`SELECT budget_tier_id FROM model_groups WHERE id = 1`).Scan(&got).Error)
	require.Nil(t, got)
}

func TestGroupServiceRejectsDisabledBudgetTier(t *testing.T) {
	tierRepo := newFakeModelTierRepo()
	tierID := uuid.New()
	tierRepo.tiers[tierID] = &model.ModelTier{ID: tierID, Enabled: false}
	groupRepo := repository.NewGroupRepo(nil)
	svc := service.NewGroupService(groupRepo, nil).WithModelTierRepo(tierRepo)

	err := svc.SetBudgetTier(context.Background(), 42, &tierID, "test")
	require.ErrorIs(t, err, service.ErrModelTierDisabled)
}

func TestGroupBudgetTierWriteRequiresReason(t *testing.T) {
	svc := service.NewGroupService(repository.NewGroupRepo(nil), nil)
	err := svc.SetBudgetTier(context.Background(), 42, nil, " ")
	require.ErrorIs(t, err, service.ErrReasonRequired)
}
