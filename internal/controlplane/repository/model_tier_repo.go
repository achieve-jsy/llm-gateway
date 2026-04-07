package repository

import (
	"context"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ModelTierRepo struct {
	db *gorm.DB
}

type ModelTierReferenceCounts struct {
	Groups     int64
	Limits     int64
	Overrides  int64
	Schedules  int64
	Alerts     int64
	Expansions int64
	CallLogs   int64
}

func (c ModelTierReferenceCounts) Total() int64 {
	return c.Groups + c.Limits + c.Overrides + c.Schedules + c.Alerts + c.Expansions + c.CallLogs
}

func NewModelTierRepo(db *gorm.DB) *ModelTierRepo {
	return &ModelTierRepo{db: db}
}

func (r *ModelTierRepo) Create(ctx context.Context, tier *model.ModelTier) error {
	err := r.db.WithContext(ctx).Create(tier).Error
	if isUniqueConstraintError(err) {
		return ErrUniqueConflict
	}
	return err
}

func (r *ModelTierRepo) FindByID(ctx context.Context, id uuid.UUID) (*model.ModelTier, error) {
	var tier model.ModelTier
	if err := r.db.WithContext(ctx).First(&tier, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &tier, nil
}

func (r *ModelTierRepo) List(ctx context.Context, includeArchived bool) ([]model.ModelTier, error) {
	var tiers []model.ModelTier
	query := r.db.WithContext(ctx)
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	err := query.Order("sort_order ASC, created_at ASC").Find(&tiers).Error
	return tiers, err
}

func (r *ModelTierRepo) Update(ctx context.Context, tier *model.ModelTier) error {
	err := r.db.WithContext(ctx).Save(tier).Error
	if isUniqueConstraintError(err) {
		return ErrUniqueConflict
	}
	return err
}

func (r *ModelTierRepo) Reorder(ctx context.Context, ids []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index, id := range ids {
			result := tx.Model(&model.ModelTier{}).
				Where("id = ? AND archived_at IS NULL", id).
				Update("sort_order", index)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		return nil
	})
}

func (r *ModelTierRepo) ReferenceCounts(ctx context.Context, id uuid.UUID) (ModelTierReferenceCounts, error) {
	var counts ModelTierReferenceCounts
	targets := []struct {
		table string
		out   *int64
	}{
		{"model_groups", &counts.Groups},
		{"budget_tier_limits", &counts.Limits},
		{"budget_temporary_overrides", &counts.Overrides},
		{"budget_limit_schedules", &counts.Schedules},
		{"budget_alert_records", &counts.Alerts},
		{"expansion_requests", &counts.Expansions},
		{"call_logs", &counts.CallLogs},
	}
	for _, target := range targets {
		if err := r.db.WithContext(ctx).Table(target.table).Where("model_tier_id = ?", id).Count(target.out).Error; err != nil {
			return ModelTierReferenceCounts{}, err
		}
	}
	return counts, nil
}

func (r *ModelTierRepo) Delete(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Delete(&model.ModelTier{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
