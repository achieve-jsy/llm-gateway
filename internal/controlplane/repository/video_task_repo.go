package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

type VideoTaskRepo struct {
	db *gorm.DB
}

func NewVideoTaskRepo(db *gorm.DB) *VideoTaskRepo {
	return &VideoTaskRepo{db: db}
}

// List 分页查询
func (r *VideoTaskRepo) List(page, pageSize int, providerName, modelName, status string) ([]model.VideoTask, int64, error) {
	var tasks []model.VideoTask
	var total int64

	query := r.db.Model(&model.VideoTask{})

	if providerName != "" {
		query = query.Where("provider_name = ?", providerName)
	}
	if modelName != "" {
		query = query.Where("model = ?", modelName)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// GetByID 按 task_id 查询
func (r *VideoTaskRepo) GetByID(taskID string) (*model.VideoTask, error) {
	var task model.VideoTask
	if err := r.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// Create 创建记录
func (r *VideoTaskRepo) Create(task *model.VideoTask) error {
	return r.db.Create(task).Error
}

// UpdateStatus 更新状态
func (r *VideoTaskRepo) UpdateStatus(taskID, status string) error {
	return r.db.Model(&model.VideoTask{}).Where("task_id = ?", taskID).Update("status", status).Error
}

// Stats 统计
func (r *VideoTaskRepo) Stats() (map[string]interface{}, error) {
	var total, submitted, running, succeeded, failed int64
	var avgDurationSeconds float64

	r.db.Model(&model.VideoTask{}).Count(&total)
	r.db.Model(&model.VideoTask{}).Where("status = ?", "submitted").Count(&submitted)
	r.db.Model(&model.VideoTask{}).Where("status = ?", "running").Count(&running)
	r.db.Model(&model.VideoTask{}).Where("status = ?", "succeeded").Count(&succeeded)
	r.db.Model(&model.VideoTask{}).Where("status = ?", "failed").Count(&failed)

	// 计算已成功任务的平均耗时（秒）
	r.db.Model(&model.VideoTask{}).
		Where("status = ? AND updated_at > created_at", "succeeded").
		Select("COALESCE(AVG(EXTRACT(EPOCH FROM (updated_at - created_at))), 0)").
		Scan(&avgDurationSeconds)

	return map[string]interface{}{
		"total":               total,
		"submitted":           submitted,
		"running":             running,
		"succeeded":           succeeded,
		"failed":              failed,
		"avg_duration_seconds": avgDurationSeconds,
	}, nil
}
