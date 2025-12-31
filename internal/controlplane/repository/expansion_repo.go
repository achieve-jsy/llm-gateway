package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ExpansionRepo 处理扩容审批表。
type ExpansionRepo struct {
	db *gorm.DB
}

func NewExpansionRepo(db *gorm.DB) *ExpansionRepo {
	return &ExpansionRepo{db: db}
}

// Create 创建扩容申请。
func (r *ExpansionRepo) Create(req *model.ExpansionRequest) error {
	return r.db.Create(req).Error
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

// CreateStep 写审批流水。
func (r *ExpansionRepo) CreateStep(step *model.ExpansionApprovalStep) error {
	return r.db.Create(step).Error
}
