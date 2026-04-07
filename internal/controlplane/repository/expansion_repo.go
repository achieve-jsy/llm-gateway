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

type PendingExpansionKey struct {
	RequesterID   uuid.UUID
	ScopeType     string
	ScopeID       string
	PeriodType    budgetdomain.PeriodType
	DimensionType budgetdomain.DimensionType
	ModelTierID   *uuid.UUID
	DurationType  budgetdomain.DurationType
}

// ExpansionRepo 处理扩容审批表。
type ExpansionRepo struct {
	db *gorm.DB
}

func NewExpansionRepo(db *gorm.DB) *ExpansionRepo {
	return &ExpansionRepo{db: db}
}

// Create 创建扩容申请。
func (r *ExpansionRepo) Create(req *model.ExpansionRequest) error {
	err := r.db.Create(req).Error
	if isUniqueConstraintError(err) {
		return ErrDuplicatePendingExpansion
	}
	return err
}

func (r *ExpansionRepo) ExistsPending(ctx context.Context, key PendingExpansionKey) (bool, error) {
	dimensionKey := "total"
	if key.DimensionType == budgetdomain.DimensionTier && key.ModelTierID != nil {
		dimensionKey = "tier:" + key.ModelTierID.String()
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&model.ExpansionRequest{}).
		Where("requester_id = ? AND scope_type = ? AND scope_id = ? AND period_type = ? AND dimension_key = ? AND duration_type = ?",
			key.RequesterID, key.ScopeType, key.ScopeID, key.PeriodType, dimensionKey, key.DurationType).
		Where("status IN ?", []string{model.ExpansionStatusPending, model.ExpansionStatusPrecheckRequired}).
		Count(&count).Error
	return count > 0, err
}

func (r *ExpansionRepo) FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*model.ExpansionRequest, error) {
	if tx == nil {
		tx = r.db
	}
	var request model.ExpansionRequest
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&request, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &request, nil
}

func (r *ExpansionRepo) WithLockedRequest(
	ctx context.Context,
	id uuid.UUID,
	fn func(*gorm.DB, *model.ExpansionRequest) error,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		request, err := r.FindByIDForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		return fn(tx, request)
	})
}

func (r *ExpansionRepo) Transaction(ctx context.Context, fn func(*gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

func (r *ExpansionRepo) FindActiveDuplicate(
	requesterID uuid.UUID,
	scopeType, scopeID, periodType, dimensionType string,
	tierID *uuid.UUID,
	durationType string,
) (*model.ExpansionRequest, error) {
	var item model.ExpansionRequest
	query := r.db.Where(
		`requester_id = ? AND scope_type = ? AND scope_id = ? AND period_type = ?
		 AND dimension_type = ? AND duration_type = ? AND status IN ?`,
		requesterID, scopeType, scopeID, periodType, dimensionType, durationType,
		[]string{model.ExpansionStatusPending, model.ExpansionStatusPrecheckRequired},
	)
	if tierID == nil {
		query = query.Where("model_tier_id IS NULL")
	} else {
		query = query.Where("model_tier_id = ?", *tierID)
	}
	if err := query.First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *ExpansionRepo) UpdateRequestFields(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.ExpansionRequest{}).Where("id = ?", id).Updates(updates).Error
}

func (r *ExpansionRepo) MarkPrecheckRequired(
	ctx context.Context,
	tx *gorm.DB,
	id uuid.UUID,
	version string,
	current, used budgetdomain.Amount,
	precheckedAt time.Time,
) error {
	return tx.WithContext(ctx).Model(&model.ExpansionRequest{}).Where("id = ?", id).Updates(map[string]any{
		"status": model.ExpansionStatusPrecheckRequired, "precheck_version": version,
		"current_amount_snapshot": current, "used_amount_snapshot": used, "prechecked_at": precheckedAt,
	}).Error
}

func (r *ExpansionRepo) MarkApplied(
	ctx context.Context,
	tx *gorm.DB,
	request *model.ExpansionRequest,
	targetType string,
	targetID uuid.UUID,
	appliedAt time.Time,
) error {
	return tx.WithContext(ctx).Model(&model.ExpansionRequest{}).
		Where("id = ? AND status = ?", request.ID, model.ExpansionStatusPending).
		Updates(map[string]any{
			"status": model.ExpansionStatusApproved, "applied_at": appliedAt,
			"applied_target_type": targetType, "applied_target_id": targetID,
			"sync_status": "pending",
		}).Error
}

func (r *ExpansionRepo) CreateStepInTx(ctx context.Context, tx *gorm.DB, step *model.ExpansionApprovalStep) error {
	return tx.WithContext(ctx).Create(step).Error
}

func (r *ExpansionRepo) UpdateSyncStatus(ctx context.Context, id uuid.UUID, status string) error {
	return r.db.WithContext(ctx).Model(&model.ExpansionRequest{}).Where("id = ?", id).
		Update("sync_status", status).Error
}

// FindByID 查找扩容申请。
func (r *ExpansionRepo) FindByID(id string) (*model.ExpansionRequest, error) {
	var req model.ExpansionRequest
	err := r.db.Where("id = ?", id).First(&req).Error
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// UpdateStatus 更新申请状态和当前审批级别。
func (r *ExpansionRepo) UpdateStatus(id, status, currentLevel string) error {
	return r.db.Model(&model.ExpansionRequest{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":        status,
			"current_level": currentLevel,
		}).Error
}

// ListByRequester 查询某用户发起的申请。
// 若 requesterID 不是合法 UUID，返回空列表（防御性校验，避免 SQL 500）。
func (r *ExpansionRepo) ListByRequester(requesterID string, status string) ([]model.ExpansionRequest, error) {
	if _, err := uuid.Parse(requesterID); err != nil {
		return []model.ExpansionRequest{}, nil
	}
	var reqs []model.ExpansionRequest
	q := r.db.Where("requester_id = ?", requesterID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	return reqs, q.Order("created_at DESC").Find(&reqs).Error
}

// ListPendingForApprover 查询待某级别审批的申请（工作室负责人/平台 admin 按级别过滤）。
func (r *ExpansionRepo) ListPendingForApprover(level string) ([]model.ExpansionRequest, error) {
	var reqs []model.ExpansionRequest
	err := r.db.Where("status = ? AND current_level = ?", model.ExpansionStatusPending, level).
		Order("created_at ASC").Find(&reqs).Error
	return reqs, err
}

// ListPendingForApproverScoped 查询审批人管辖范围内的待审批申请。
// requesterName 非空时按申请人姓名模糊过滤（JOIN users 表）。
func (r *ExpansionRepo) ListPendingForApproverScoped(level string, deptIDs, studioIDs, memberIDs []string, requesterName string) ([]model.ExpansionRequest, error) {
	var reqs []model.ExpansionRequest
	q := r.db.Table("expansion_requests er").
		Select("er.*").
		Where("er.status = ? AND er.current_level = ?", model.ExpansionStatusPending, level)

	if requesterName != "" {
		q = q.Joins("JOIN users u ON u.id = er.requester_id").
			Where("u.name ILIKE ?", "%"+requesterName+"%")
	}

	switch level {
	case model.ExpansionLevelDept:
		q = q.Where("er.scope_type = ? AND er.scope_id IN ?", model.BudgetScopeMember, memberIDs)
	case model.ExpansionLevelStudio:
		q = q.Where(
			r.db.Where("er.scope_type IN ? AND er.scope_id IN ?", []string{model.BudgetScopeDeptDaily, model.BudgetScopeDeptService}, deptIDs).
				Or("er.scope_type = ? AND er.scope_id IN ?", model.BudgetScopeStudio, studioIDs),
		)
	}
	err := q.Order("er.created_at ASC").Find(&reqs).Error
	return reqs, err
}

// CreateStep 写审批流水。
func (r *ExpansionRepo) CreateStep(step *model.ExpansionApprovalStep) error {
	return r.db.Create(step).Error
}

// ListSteps 查询某扩容申请的所有审批步骤（按时间正序）。
func (r *ExpansionRepo) ListSteps(requestID string) ([]model.ExpansionApprovalStep, error) {
	var steps []model.ExpansionApprovalStep
	err := r.db.Where("request_id = ?", requestID).Order("created_at ASC").Find(&steps).Error
	return steps, err
}
