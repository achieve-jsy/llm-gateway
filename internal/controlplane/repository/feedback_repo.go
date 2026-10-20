package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FeedbackRepo struct {
	db *gorm.DB
}

func NewFeedbackRepo(db *gorm.DB) *FeedbackRepo {
	return &FeedbackRepo{db: db}
}

// FeedbackListFilter 反馈列表查询条件，空字符串表示不按该字段过滤。
type FeedbackListFilter struct {
	Status   string
	Type     string
	Page     int
	PageSize int
}

func (r *FeedbackRepo) Create(f *model.Feedback) error {
	return r.db.Create(f).Error
}

// DB 暴露底层 gorm.DB，供 service 在同一连接上构造附件仓储
// （feedbacks 与 feedback_attachments 必须同库，事务才能覆盖两者）。
func (r *FeedbackRepo) DB() *gorm.DB {
	return r.db
}

// Transaction 在单个数据库事务内执行 fn，供 service 保证
// 「创建反馈 + 绑定附件」要么全部生效、要么全部回滚。
func (r *FeedbackRepo) Transaction(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

// CreateInTx 在调用方事务内创建反馈（配合 Transaction 使用）。
func (r *FeedbackRepo) CreateInTx(tx *gorm.DB, f *model.Feedback) error {
	return tx.Create(f).Error
}

func (r *FeedbackRepo) List(filter FeedbackListFilter) ([]model.Feedback, int64, error) {
	feedbacks := make([]model.Feedback, 0)
	var total int64

	query := r.db.Model(&model.Feedback{})
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&feedbacks).Error

	return feedbacks, total, err
}

func (r *FeedbackRepo) GetByID(id uuid.UUID) (*model.Feedback, error) {
	var f model.Feedback
	if err := r.db.First(&f, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// UpdateStatus 更新反馈状态与处理备注；状态流转到已解决/已关闭时一并写入处理时间与处理人。
func (r *FeedbackRepo) UpdateStatus(id uuid.UUID, status string, handledBy uuid.UUID, note string, handledAt time.Time) error {
	updates := map[string]interface{}{
		"status":      status,
		"handle_note": note,
	}
	if status == "resolved" || status == "closed" {
		updates["handled_at"] = handledAt
		updates["handled_by"] = handledBy
	}
	return r.db.Model(&model.Feedback{}).Where("id = ?", id).Updates(updates).Error
}
