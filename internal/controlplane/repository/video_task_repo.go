package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VideoTaskRepo struct {
	db *gorm.DB
}

type TrendRow struct {
	Date      string `json:"date"`
	Tasks     int64  `json:"tasks"`
	Succeeded int64  `json:"succeeded"`
	Failed    int64  `json:"failed"`
	Submitted int64  `json:"submitted"`
	Running   int64  `json:"running"`
}

type ProviderDistributionRow struct {
	ProviderName string `json:"provider_name"`
	Count        int64  `json:"count"`
}

type StatusDistributionRow struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type ModelDistributionRow struct {
	Model     string `json:"model"`
	Count     int64  `json:"count"`
	Succeeded int64  `json:"succeeded"`
	Failed    int64  `json:"failed"`
}

type DistributionRows struct {
	Providers []ProviderDistributionRow `json:"providers"`
	Statuses  []StatusDistributionRow   `json:"statuses"`
	Models    []ModelDistributionRow    `json:"models"`
}

func NewVideoTaskRepo(db *gorm.DB) *VideoTaskRepo {
	return &VideoTaskRepo{db: db}
}

type VideoTaskUpdateFields struct {
	Status       string
	VideoURL     string
	ResponseBody string
	ErrorMessage string
}

// List 分页查询
func (r *VideoTaskRepo) List(page, pageSize int, providerName, modelName, status, userID string) ([]model.VideoTask, int64, error) {
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
	if userID != "" {
		query = query.Where("user_id = ?", userID)
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
	if err := r.db.
		Preload("Events", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC").Order("id ASC")
		}).
		Where("task_id = ?", taskID).
		First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// Create 创建记录（task_id 冲突时忽略，保证幂等）
func (r *VideoTaskRepo) Create(task *model.VideoTask) error {
	result := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(task)
	if result.Error != nil || result.RowsAffected == 0 {
		return result.Error
	}
	return r.createEvent(task.TaskID, task.Status, "任务已提交")
}

// UpdateStatus 更新状态并刷新 updated_at（用于平均生成耗时统计）
func (r *VideoTaskRepo) UpdateStatus(taskID, status string) error {
	return r.UpdateFields(taskID, VideoTaskUpdateFields{Status: status})
}

// UpdateFields 按字段更新任务详情，并在状态变化时写入事件。
func (r *VideoTaskRepo) UpdateFields(taskID string, fields VideoTaskUpdateFields) error {
	updates := map[string]interface{}{}
	if fields.Status != "" {
		updates["status"] = fields.Status
	}
	if fields.VideoURL != "" {
		updates["video_url"] = fields.VideoURL
	}
	if fields.ResponseBody != "" {
		updates["response_body"] = fields.ResponseBody
	}
	if fields.ErrorMessage != "" {
		updates["error_message"] = fields.ErrorMessage
	}
	if len(updates) == 0 {
		return nil
	}

	var previousStatus string
	if fields.Status != "" {
		_ = r.db.Model(&model.VideoTask{}).
			Select("status").
			Where("task_id = ?", taskID).
			Scan(&previousStatus).Error
	}

	// updated_at 用于生成耗时统计；同一状态下轮询只刷新响应体，不延长耗时。
	if fields.Status == "" || fields.Status != previousStatus {
		updates["updated_at"] = time.Now().UTC()
	}
	result := r.db.Model(&model.VideoTask{}).
		Where("task_id = ?", taskID).
		Updates(updates)
	if result.Error != nil || result.RowsAffected == 0 {
		return result.Error
	}
	if fields.Status != "" && fields.Status != previousStatus {
		return r.createEvent(taskID, fields.Status, "状态更新为 "+fields.Status)
	}
	return nil
}

func (r *VideoTaskRepo) createEvent(taskID, eventType, eventData string) error {
	if eventType == "" {
		eventType = "updated"
	}
	return r.db.Create(&model.VideoTaskEvent{
		TaskID:    taskID,
		EventType: eventType,
		EventData: eventData,
		CreatedAt: time.Now().UTC(),
	}).Error
}

// GetProviderType 按 task_id 查 provider_type（供 gateway 查询路由）
func (r *VideoTaskRepo) GetProviderType(taskID string) (string, error) {
	var task model.VideoTask
	if err := r.db.Select("provider_type").Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return "", err
	}
	return task.ProviderType, nil
}

func (r *VideoTaskRepo) statsQuery(start, end, userID, providerName, modelName string) *gorm.DB {
	query := r.db.Model(&model.VideoTask{})
	if start != "" {
		query = query.Where("created_at >= ?", start)
	}
	if end != "" {
		query = query.Where("created_at < (?::date + INTERVAL '1 day')", end)
	}
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if providerName != "" {
		query = query.Where("provider_name = ?", providerName)
	}
	if modelName != "" {
		query = query.Where("model = ?", modelName)
	}
	return query
}

// Stats 统计
func (r *VideoTaskRepo) Stats(start, end, userID, providerName, modelName string) (map[string]interface{}, error) {
	var total, submitted, running, succeeded, failed int64
	var avgDurationSeconds float64

	base := r.statsQuery(start, end, userID, providerName, modelName)
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, err
	}
	if err := base.Session(&gorm.Session{}).Where("status = ?", "submitted").Count(&submitted).Error; err != nil {
		return nil, err
	}
	if err := base.Session(&gorm.Session{}).Where("status = ?", "running").Count(&running).Error; err != nil {
		return nil, err
	}
	if err := base.Session(&gorm.Session{}).Where("status = ?", "succeeded").Count(&succeeded).Error; err != nil {
		return nil, err
	}
	if err := base.Session(&gorm.Session{}).Where("status = ?", "failed").Count(&failed).Error; err != nil {
		return nil, err
	}

	// 计算已成功任务的平均耗时（秒）
	if err := base.Session(&gorm.Session{}).
		Where("status = ? AND updated_at > created_at", "succeeded").
		Select("COALESCE(AVG(EXTRACT(EPOCH FROM (updated_at - created_at))), 0)").
		Scan(&avgDurationSeconds).Error; err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total":                total,
		"submitted":            submitted,
		"running":              running,
		"succeeded":            succeeded,
		"failed":               failed,
		"avg_duration_seconds": avgDurationSeconds,
	}, nil
}

// StatsTrend 按天统计任务趋势。
func (r *VideoTaskRepo) StatsTrend(start, end, userID, providerName, modelName string) ([]TrendRow, error) {
	var rows []TrendRow
	err := r.statsQuery(start, end, userID, providerName, modelName).
		Select(`
			TO_CHAR(created_at::date, 'YYYY-MM-DD') AS date,
			COUNT(*) AS tasks,
			COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed,
			COUNT(*) FILTER (WHERE status = 'submitted') AS submitted,
			COUNT(*) FILTER (WHERE status = 'running') AS running
		`).
		Group("created_at::date").
		Order("created_at::date ASC").
		Scan(&rows).Error
	return rows, err
}

// StatsDistribution 统计渠道、状态、模型分布。
func (r *VideoTaskRepo) StatsDistribution(start, end, userID, providerName, modelName string) (DistributionRows, error) {
	var result DistributionRows

	if err := r.statsQuery(start, end, userID, providerName, modelName).
		Select("provider_name, COUNT(*) AS count").
		Group("provider_name").
		Order("count DESC").
		Scan(&result.Providers).Error; err != nil {
		return result, err
	}
	if err := r.statsQuery(start, end, userID, providerName, modelName).
		Select("status, COUNT(*) AS count").
		Group("status").
		Order("count DESC").
		Scan(&result.Statuses).Error; err != nil {
		return result, err
	}
	if err := r.statsQuery(start, end, userID, providerName, modelName).
		Select(`
			model,
			COUNT(*) AS count,
			COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed
		`).
		Group("model").
		Order("count DESC").
		Limit(20).
		Scan(&result.Models).Error; err != nil {
		return result, err
	}
	return result, nil
}
