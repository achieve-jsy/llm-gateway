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
func (r *KeyRequestRepo) Approve(id string, approverID, createdKeyID uuid.UUID, comment string) error {
	return r.db.Model(&model.KeyRequest{}).Where("id = ?", id).Updates(map[string]any{
		"status":          model.KeyRequestStatusApproved,
		"approver_id":     approverID,
		"approve_comment": comment,
		"created_key_id":  createdKeyID,
		"updated_at":      time.Now(),
	}).Error
}

func (r *KeyRequestRepo) Reject(id string, approverID uuid.UUID, comment string) error {
	return r.db.Model(&model.KeyRequest{}).Where("id = ?", id).Updates(map[string]any{
		"status":          model.KeyRequestStatusRejected,
		"approver_id":     approverID,
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
	var items []model.KeyRequest
	q := r.db.Where("requester_id = ?", requesterID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("created_at DESC").Find(&items).Error
	return items, err
}

// ListPendingForDept 查询某部门待审批的申请（仅 dept 级审批）。
// requesterName 非空时按申请人姓名模糊过滤。
func (r *KeyRequestRepo) ListPendingForDept(deptIDs []string, requesterName string) ([]model.KeyRequest, error) {
	var items []model.KeyRequest
	q := r.db.Where("status = ? AND dept_id IN ?", model.KeyRequestStatusPending, deptIDs)
	if requesterName != "" {
		q = q.Where("requester_name ILIKE ?", "%"+requesterName+"%")
	}
	err := q.Order("created_at ASC").Find(&items).Error
	return items, err
}

// ListPendingAll 全局 admin 查询所有待审批申请。
func (r *KeyRequestRepo) ListPendingAll(requesterName string) ([]model.KeyRequest, error) {
	var items []model.KeyRequest
	q := r.db.Where("status = ?", model.KeyRequestStatusPending)
	if requesterName != "" {
		q = q.Where("requester_name ILIKE ?", "%"+requesterName+"%")
	}
	err := q.Order("created_at ASC").Find(&items).Error
	return items, err
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
	var items []model.KeyRequest
	q := r.db.Where("status != ?", model.KeyRequestStatusPending)
	if !isGlobal {
		if len(deptIDs) == 0 {
			// 无管辖部门，只查自己当过审批人的记录
			q = q.Where("approver_id IS NOT NULL")
		} else {
			q = q.Where("dept_id IN ?", deptIDs)
		}
	}
	if limit <= 0 {
		limit = 50
	}
	err := q.Order("updated_at DESC").Limit(limit).Find(&items).Error
	return items, err
}
