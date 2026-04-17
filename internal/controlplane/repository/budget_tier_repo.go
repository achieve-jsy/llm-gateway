package repository

import (
	"context"
	"errors"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BudgetTierRepo struct {
	db *gorm.DB
}

type UpsertTierLimitParams struct {
	BudgetAllocationID uuid.UUID
	ModelTierID        uuid.UUID
	PeriodType         budgetdomain.PeriodType
	LimitUSD           budgetdomain.Amount
	EffectiveAt        time.Time
	Enabled            bool
}

func NewBudgetTierRepo(db *gorm.DB) *BudgetTierRepo {
	return &BudgetTierRepo{db: db}
}

func (r *BudgetTierRepo) ListTierLimits(ctx context.Context, allocationID uuid.UUID) ([]model.BudgetTierLimit, error) {
	var limits []model.BudgetTierLimit
	err := r.db.WithContext(ctx).Preload("ModelTier").
		Where("budget_allocation_id = ?", allocationID).
		Order("model_tier_id, period_type").Find(&limits).Error
	return limits, err
}

func (r *BudgetTierRepo) FindTierLimit(ctx context.Context, allocationID, tierID uuid.UUID, period budgetdomain.PeriodType) (*model.BudgetTierLimit, error) {
	var limit model.BudgetTierLimit
	err := r.db.WithContext(ctx).
		Where("budget_allocation_id = ? AND model_tier_id = ? AND period_type = ?", allocationID, tierID, period).
		First(&limit).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &limit, nil
}

func (r *BudgetTierRepo) FindTierLimitForUpdate(ctx context.Context, tx *gorm.DB, allocationID, tierID uuid.UUID, period budgetdomain.PeriodType) (*model.BudgetTierLimit, error) {
	var limit model.BudgetTierLimit
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("budget_allocation_id = ? AND model_tier_id = ? AND period_type = ?", allocationID, tierID, period).
		First(&limit).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &limit, nil
}

func (r *BudgetTierRepo) UpsertTierLimit(ctx context.Context, params UpsertTierLimitParams) (*model.BudgetTierLimit, error) {
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return UpsertTierLimitInTx(ctx, tx, params)
	}); err != nil {
		return nil, err
	}
	return r.FindTierLimit(ctx, params.BudgetAllocationID, params.ModelTierID, params.PeriodType)
}

func (r *BudgetTierRepo) ListActiveOverrides(ctx context.Context, allocationID uuid.UUID, now time.Time) ([]model.BudgetTemporaryOverride, error) {
	var overrides []model.BudgetTemporaryOverride
	err := r.db.WithContext(ctx).
		Where("budget_allocation_id = ? AND status = ? AND starts_at <= ? AND expires_at > ?",
			allocationID, budgetdomain.OverrideActive, now, now).
		Find(&overrides).Error
	return overrides, err
}

func (r *BudgetTierRepo) CreateOverride(ctx context.Context, override *model.BudgetTemporaryOverride) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockBudgetAllocation(ctx, tx, override.BudgetAllocationID); err != nil {
			return err
		}
		return tx.WithContext(ctx).Create(override).Error
	})
}

func (r *BudgetTierRepo) ListDueOverrideIDs(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Model(&model.BudgetTemporaryOverride{}).
		Where("status = ? AND expires_at <= ?", budgetdomain.OverrideActive, now).
		Order("expires_at ASC").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}

func (r *BudgetTierRepo) GetOverrideAllocationID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (uuid.UUID, error) {
	var row struct {
		BudgetAllocationID uuid.UUID
	}
	err := tx.WithContext(ctx).Model(&model.BudgetTemporaryOverride{}).
		Select("budget_allocation_id").Where("id = ?", id).Take(&row).Error
	return row.BudgetAllocationID, err
}

func (r *BudgetTierRepo) LockOverrideForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*model.BudgetTemporaryOverride, error) {
	var override model.BudgetTemporaryOverride
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		First(&override, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &override, nil
}

func (r *BudgetTierRepo) MarkOverrideExpired(ctx context.Context, tx *gorm.DB, id uuid.UUID, expiredAt time.Time) error {
	result := tx.WithContext(ctx).Model(&model.BudgetTemporaryOverride{}).
		Where("id = ? AND status = ? AND expires_at <= ?", id, budgetdomain.OverrideActive, expiredAt).
		Updates(map[string]any{"status": budgetdomain.OverrideExpired, "updated_at": expiredAt, "failure_reason": nil})
	return requireSingleLifecycleUpdate(result)
}

func (r *BudgetTierRepo) MarkOverrideFailed(ctx context.Context, tx *gorm.DB, id uuid.UUID, reason string) error {
	result := tx.WithContext(ctx).Model(&model.BudgetTemporaryOverride{}).
		Where("id = ? AND status = ?", id, budgetdomain.OverrideActive).
		Updates(map[string]any{"status": budgetdomain.OverrideFailed, "failure_reason": reason})
	return requireSingleLifecycleUpdate(result)
}

func (r *BudgetTierRepo) ReplacePendingSchedule(ctx context.Context, schedule *model.BudgetLimitSchedule) (*model.BudgetLimitSchedule, error) {
	return r.PutPendingSchedule(ctx, schedule, true)
}

func (r *BudgetTierRepo) PutPendingSchedule(ctx context.Context, schedule *model.BudgetLimitSchedule, confirmReplace bool) (*model.BudgetLimitSchedule, error) {
	for attempt := 0; attempt < 2; attempt++ {
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := lockBudgetAllocation(ctx, tx, schedule.BudgetAllocationID); err != nil {
				return err
			}
			return r.PutPendingScheduleInTx(ctx, tx, schedule, confirmReplace)
		})
		if !isUniqueConstraintError(err) {
			return schedule, err
		}
		if !confirmReplace {
			return nil, ErrPendingScheduleExists
		}
	}
	return nil, ErrPendingScheduleExists
}

func lockBudgetAllocation(ctx context.Context, tx *gorm.DB, allocationID uuid.UUID) error {
	var row struct {
		ID uuid.UUID
	}
	return tx.WithContext(ctx).Model(&model.BudgetAllocation{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").Where("id = ?", allocationID).Take(&row).Error
}

func (r *BudgetTierRepo) LockBudgetAllocation(ctx context.Context, tx *gorm.DB, allocationID uuid.UUID) error {
	return lockBudgetAllocation(ctx, tx, allocationID)
}

func (r *BudgetTierRepo) PutPendingScheduleInTx(ctx context.Context, tx *gorm.DB, schedule *model.BudgetLimitSchedule, confirmReplace bool) error {
	var pending model.BudgetLimitSchedule
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("budget_allocation_id = ? AND dimension_key = ? AND period_type = ? AND status = ?",
			schedule.BudgetAllocationID, schedule.DimensionKey, schedule.PeriodType, budgetdomain.SchedulePending).
		First(&pending).Error
	switch {
	case err == nil:
		if !confirmReplace {
			return ErrPendingScheduleExists
		}
		result := tx.WithContext(ctx).Model(&model.BudgetLimitSchedule{}).
			Where("id = ? AND status = ?", pending.ID, budgetdomain.SchedulePending).
			Update("status", budgetdomain.ScheduleReplaced)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrPendingScheduleExists
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Partial unique index serializes the no-row race at insert time.
	default:
		return err
	}
	err = tx.WithContext(ctx).Create(schedule).Error
	if isUniqueConstraintError(err) {
		return ErrPendingScheduleExists
	}
	return err
}

func (r *BudgetTierRepo) FindPendingSchedule(ctx context.Context, allocationID uuid.UUID, dimensionKey string, period budgetdomain.PeriodType) (*model.BudgetLimitSchedule, error) {
	var schedule model.BudgetLimitSchedule
	err := r.db.WithContext(ctx).
		Where("budget_allocation_id = ? AND dimension_key = ? AND period_type = ? AND status = ?",
			allocationID, dimensionKey, period, budgetdomain.SchedulePending).
		First(&schedule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (r *BudgetTierRepo) ListPendingSchedules(ctx context.Context, allocationID uuid.UUID) ([]model.BudgetLimitSchedule, error) {
	var schedules []model.BudgetLimitSchedule
	err := r.db.WithContext(ctx).
		Where("budget_allocation_id = ? AND dimension_type = ? AND status = ?",
			allocationID, budgetdomain.DimensionTier, budgetdomain.SchedulePending).
		Order("dimension_key, period_type").Find(&schedules).Error
	return schedules, err
}

func (r *BudgetTierRepo) ListDueScheduleIDs(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Model(&model.BudgetLimitSchedule{}).
		Where("status = ? AND effective_at <= ?", budgetdomain.SchedulePending, now).
		Order("effective_at ASC").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}

func (r *BudgetTierRepo) GetScheduleAllocationID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (uuid.UUID, error) {
	var row struct {
		BudgetAllocationID uuid.UUID
	}
	err := tx.WithContext(ctx).Model(&model.BudgetLimitSchedule{}).
		Select("budget_allocation_id").Where("id = ?", id).Take(&row).Error
	return row.BudgetAllocationID, err
}

func (r *BudgetTierRepo) LockScheduleForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*model.BudgetLimitSchedule, error) {
	var schedule model.BudgetLimitSchedule
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		First(&schedule, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (r *BudgetTierRepo) MarkSchedule(ctx context.Context, tx *gorm.DB, id uuid.UUID, status budgetdomain.ScheduleStatus, reason string) error {
	updates := map[string]any{"status": status}
	if reason != "" {
		updates["failure_reason"] = reason
	} else {
		updates["failure_reason"] = nil
	}
	result := tx.WithContext(ctx).Model(&model.BudgetLimitSchedule{}).
		Where("id = ? AND status = ?", id, budgetdomain.SchedulePending).Updates(updates)
	return requireSingleLifecycleUpdate(result)
}

func requireSingleLifecycleUpdate(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *BudgetTierRepo) Transaction(ctx context.Context, fn func(*gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

func UpsertTierLimitInTx(ctx context.Context, tx *gorm.DB, params UpsertTierLimitParams) error {
	if err := lockBudgetAllocation(ctx, tx, params.BudgetAllocationID); err != nil {
		return err
	}
	limit := model.BudgetTierLimit{
		ID: uuid.New(), BudgetAllocationID: params.BudgetAllocationID,
		ModelTierID: params.ModelTierID, PeriodType: params.PeriodType,
		LimitUSD: params.LimitUSD, EffectiveAt: params.EffectiveAt, Enabled: params.Enabled,
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "budget_allocation_id"}, {Name: "model_tier_id"}, {Name: "period_type"}},
		DoUpdates: clause.Assignments(map[string]any{
			"limit_usd": params.LimitUSD, "enabled": params.Enabled, "updated_at": time.Now().UTC(),
		}),
	}).Create(&limit).Error
}

func (r *BudgetTierRepo) UpsertTierLimitInTx(ctx context.Context, tx *gorm.DB, params UpsertTierLimitParams) error {
	return UpsertTierLimitInTx(ctx, tx, params)
}

func (r *BudgetTierRepo) ApplyTierLimitsBatch(ctx context.Context, items []UpsertTierLimitParams) error {
	return r.Transaction(ctx, func(tx *gorm.DB) error {
		for _, item := range items {
			if err := UpsertTierLimitInTx(ctx, tx, item); err != nil {
				return err
			}
		}
		return nil
	})
}
