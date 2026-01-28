package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type KeyRequestRepo struct {
	db *gorm.DB
}

func NewKeyRequestRepo(db *gorm.DB) *KeyRequestRepo {
	return &KeyRequestRepo{db: db}
}

// Create 创建 Key 申请。
func (r *KeyRequestRepo) Create(req *model.KeyRequest) error {
	return r.db.Create(req).Error
}

// FindByID 查询单条申请。
func (r *KeyRequestRepo) FindByID(id string) (*model.KeyRequest, error) {
	var req model.KeyRequest
	err := r.db.Where("id = ?", id).First(&req).Error
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// UpdateStatus 更新申请状态与审批人/备注/创建的 Key ID。
func (r *KeyRequestRepo) Approve(id string, approverID, createdKeyID uuid.UUID, approverName, comment string) error {
	return r.db.Model(&model.KeyRequest{}).Where("id = ?", id).Updates(map[string]any{
		"status":          model.KeyRequestStatusApproved,
		"approver_id":     approverID,
		"approver_name":   approverName,
		"approve_comment": comment,
		"created_key_id":  createdKeyID,
		"updated_at":      time.Now(),
	}).Error
}

func (r *KeyRequestRepo) Reject(id string, approverID uuid.UUID, approverName, comment string) error {
	return r.db.Model(&model.KeyRequest{}).Where("id = ?", id).Updates(map[string]any{
		"status":          model.KeyRequestStatusRejected,
		"approver_id":     approverID,
		"approver_name":   approverName,
		"approve_comment": comment,
		"updated_at":      time.Now(),
	}).Error
}

func (r *KeyRequestRepo) Cancel(id string) error {
	return r.db.Model(&model.KeyRequest{}).Where("id = ?", id).Updates(map[string]any{
		"status":     model.KeyRequestStatusCancelled,
		"updated_at": time.Now(),
	}).Error
}

// ListByRequester 查询某用户发起的申请，可按状态过滤。
func (r *KeyRequestRepo) ListByRequester(requesterID string, status string) ([]model.KeyRequest, error) {
	items, _, err := r.ListByRequesterPaged(requesterID, status, 1, 0)
	return items, err
}

// ListByRequesterPaged 分页查询某用户发起的申请。
func (r *KeyRequestRepo) ListByRequesterPaged(requesterID, status string, page, pageSize int) ([]model.KeyRequest, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	countQ := r.db.Model(&model.KeyRequest{}).Where("requester_id = ?", requesterID)
	listQ := r.db.Model(&model.KeyRequest{}).Where("requester_id = ?", requesterID)
	if status != "" {
		countQ = countQ.Where("status = ?", status)
		listQ = listQ.Where("status = ?", status)
	}
	var total int64
	if err := countQ.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.KeyRequest{}, 0, nil
	}
	var items []model.KeyRequest
	err := listQ.Order("created_at DESC").
		Offset((page-1)*pageSize).
		Limit(pageSize).
		Find(&items).Error
	return items, total, err
}

// ListPendingPaged 分页查询待审批申请。
func (r *KeyRequestRepo) ListPendingPaged(isGlobal bool, deptIDs []string, requesterName string, hasBudget bool, page, pageSize int) ([]model.KeyRequest, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if !isGlobal && len(deptIDs) == 0 {
		return nil, 0, nil
	}
	countQ := r.db.Model(&model.KeyRequest{}).Where("status = ?", model.KeyRequestStatusPending)
	listQ := r.db.Model(&model.KeyRequest{}).Where("status = ?", model.KeyRequestStatusPending)
	if !isGlobal {
		countQ = countQ.Where("dept_id IN ?", deptIDs)
		listQ = listQ.Where("dept_id IN ?", deptIDs)
	}
	if requesterName != "" {
		like := "%" + requesterName + "%"
		cond := "(requester_name ILIKE ? OR key_name ILIKE ?)"
		countQ = countQ.Where(cond, like, like)
		listQ = listQ.Where(cond, like, like)
	}
	if hasBudget {
		countQ = countQ.Where("COALESCE(budget_monthly, 0) > 0")
		listQ = listQ.Where("COALESCE(budget_monthly, 0) > 0")
	}
	var total int64
	if err := countQ.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.KeyRequest{}, 0, nil
	}
	var items []model.KeyRequest
	err := listQ.Order("created_at ASC").
		Offset((page-1)*pageSize).
		Limit(pageSize).
		Find(&items).Error
	return items, total, err
}

// ListPendingForDept 查询某部门待审批的申请（仅 dept 级审批）。
// requesterName 非空时按申请人姓名模糊过滤。
func (r *KeyRequestRepo) ListPendingForDept(deptIDs []string, requesterName string) ([]model.KeyRequest, error) {
	items, _, err := r.ListPendingPaged(false, deptIDs, requesterName, false, 1, 0)
	return items, err
}

// ListPendingAll 全局 admin 查询所有待审批申请。
func (r *KeyRequestRepo) ListPendingAll(requesterName string) ([]model.KeyRequest, error) {
	items, _, err := r.ListPendingPaged(true, nil, requesterName, false, 1, 0)
	return items, err
}

// CountPendingScoped 统计管辖范围内待审批数量（含预算待办数）。
func (r *KeyRequestRepo) CountPendingScoped(isGlobal bool, deptIDs []string) (total int64, budgetCount int64, err error) {
	if !isGlobal && len(deptIDs) == 0 {
		return 0, 0, nil
	}
	base := r.db.Model(&model.KeyRequest{}).Where("status = ?", model.KeyRequestStatusPending)
	if !isGlobal {
		base = base.Where("dept_id IN ?", deptIDs)
	}
	if err = base.Count(&total).Error; err != nil {
		return 0, 0, err
	}
	budgetQ := r.db.Model(&model.KeyRequest{}).Where("status = ? AND COALESCE(budget_monthly, 0) > 0", model.KeyRequestStatusPending)
	if !isGlobal {
		budgetQ = budgetQ.Where("dept_id IN ?", deptIDs)
	}
	err = budgetQ.Count(&budgetCount).Error
	return total, budgetCount, err
}

// CountByStatus 统计当前用户某种状态的申请数量。
func (r *KeyRequestRepo) CountByStatus(requesterID string, status string) (int64, error) {
	var count int64
	q := r.db.Model(&model.KeyRequest{}).Where("requester_id = ?", requesterID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Count(&count).Error
	return count, err
}

// CountPendingForDept 统计某部门待审批的申请数量。
func (r *KeyRequestRepo) CountPendingForDept(deptIDs []string) (int64, error) {
	var count int64
	err := r.db.Model(&model.KeyRequest{}).Where("status = ? AND dept_id IN ?", model.KeyRequestStatusPending, deptIDs).Count(&count).Error
	return count, err
}

// ListPendingByDept 查询某部门所有待审批的申请（按申请时间升序，先处理的优先审批）。
func (r *KeyRequestRepo) ListPendingByDept(deptID string) ([]model.KeyRequest, error) {
	var items []model.KeyRequest
	err := r.db.Where("status = ? AND dept_id = ?", model.KeyRequestStatusPending, deptID).
		Order("created_at ASC").Find(&items).Error
	return items, err
}

// ListHistoryForApprover 查询当前审批人已处理的申请（status != pending，且当前用户是 approver 或管辖部门的申请）。
func (r *KeyRequestRepo) ListHistoryForApprover(isGlobal bool, deptIDs []string, limit int) ([]model.KeyRequest, error) {
	items, _, err := r.ListHistoryPaged(isGlobal, deptIDs, "", "", 1, limit)
	return items, err
}

// ListHistoryPaged 分页查询审批人已处理的申请（仅已通过/已拒绝）。
func (r *KeyRequestRepo) ListHistoryPaged(isGlobal bool, deptIDs []string, requesterName, status string, page, pageSize int) ([]model.KeyRequest, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if !isGlobal && len(deptIDs) == 0 {
		return []model.KeyRequest{}, 0, nil
	}
	statuses := []string{model.KeyRequestStatusApproved, model.KeyRequestStatusRejected}
	countQ := r.db.Model(&model.KeyRequest{}).Where("status IN ?", statuses)
	listQ := r.db.Model(&model.KeyRequest{}).Where("status IN ?", statuses)
	if !isGlobal {
		countQ = countQ.Where("dept_id IN ?", deptIDs)
		listQ = listQ.Where("dept_id IN ?", deptIDs)
	}
	if requesterName != "" {
		like := "%" + requesterName + "%"
		cond := "(requester_name ILIKE ? OR key_name ILIKE ?)"
		countQ = countQ.Where(cond, like, like)
		listQ = listQ.Where(cond, like, like)
	}
	if status == model.KeyRequestStatusApproved || status == model.KeyRequestStatusRejected {
		countQ = countQ.Where("status = ?", status)
		listQ = listQ.Where("status = ?", status)
	}
	var total int64
	if err := countQ.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.KeyRequest{}, 0, nil
	}
	var items []model.KeyRequest
	err := listQ.Order("updated_at DESC").
		Offset((page-1)*pageSize).
		Limit(pageSize).
		Find(&items).Error
	return items, total, err
}
