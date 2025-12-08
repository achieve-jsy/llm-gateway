package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ModelStatusLogRepo 模型状态变更日志数据访问层
type ModelStatusLogRepo struct {
	db *gorm.DB
}

func NewModelStatusLogRepo(db *gorm.DB) *ModelStatusLogRepo {
	return &ModelStatusLogRepo{db: db}
}

func (r *ModelStatusLogRepo) Create(log *model.ModelStatusLog) error {
	return r.db.Create(log).Error
}

func (r *ModelStatusLogRepo) List(
	modelID, providerID *uuid.UUID,
	changeType *string,
	startTime, endTime *time.Time,
	offset, limit int,
) ([]model.ModelStatusLog, int64, error) {
	query := r.db.Model(&model.ModelStatusLog{})

	if modelID != nil {
		query = query.Where("model_id = ?", *modelID)
	}
	if providerID != nil {
		query = query.Where("provider_id = ?", *providerID)
	}
	if changeType != nil && *changeType != "" {
		query = query.Where("change_type = ?", *changeType)
	}

	if startTime != nil {
		query = query.Where("created_at >= ?", *startTime)
	}
	if endTime != nil {
		query = query.Where("created_at <= ?", *endTime)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var logs []model.ModelStatusLog
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&logs).Error
	return logs, total, err
}

func (r *ModelStatusLogRepo) ListByModelID(modelID uuid.UUID, limit int) ([]model.ModelStatusLog, error) {
	var logs []model.ModelStatusLog
	err := r.db.Where("model_id = ?", modelID).Order("created_at DESC").Limit(limit).Find(&logs).Error
	return logs, err
}