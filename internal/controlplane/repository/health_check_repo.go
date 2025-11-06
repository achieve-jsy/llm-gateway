package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// HealthCheckRepo 健康检测数据访问层
type HealthCheckRepo struct {
	db *gorm.DB
}

// NewHealthCheckRepo 创建 HealthCheckRepo 实例
func NewHealthCheckRepo(db *gorm.DB) *HealthCheckRepo {
	return &HealthCheckRepo{db: db}
}

// GetConfig 获取健康检测配置（单例）
func (r *HealthCheckRepo) GetConfig() (*model.HealthCheckConfig, error) {
	var config model.HealthCheckConfig
	err := r.db.First(&config).Error
	return &config, err
}

// UpdateConfig 更新配置
func (r *HealthCheckRepo) UpdateConfig(config *model.HealthCheckConfig) error {
	return r.db.Save(config).Error
}

// CreateLog 创建检测记录
func (r *HealthCheckRepo) CreateLog(log *model.HealthCheckLog) error {
	return r.db.Create(log).Error
}

// GetRecentLogs 获取最近的检测记录
func (r *HealthCheckRepo) GetRecentLogs(limit int) ([]model.HealthCheckLog, error) {
	var logs []model.HealthCheckLog
	err := r.db.Order("checked_at DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// GetTargetLogs 获取某目标的最近记录
func (r *HealthCheckRepo) GetTargetLogs(targetType string, targetID uuid.UUID, limit int) ([]model.HealthCheckLog, error) {
	var logs []model.HealthCheckLog
	err := r.db.Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("checked_at DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// CountConsecutiveFailures 统计连续失败次数
func (r *HealthCheckRepo) CountConsecutiveFailures(targetType string, targetID uuid.UUID) (int, error) {
	var logs []model.HealthCheckLog
	err := r.db.Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("checked_at DESC").Limit(10).Find(&logs).Error
	if err != nil {
		return 0, err
	}
	count := 0
	for _, log := range logs {
		if log.Success {
			break
		}
		count++
	}
	return count, nil
}

// CleanOldLogs 清理超过指定天数的日志
func (r *HealthCheckRepo) CleanOldLogs(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)
	return r.db.Where("checked_at < ?", cutoff).Delete(&model.HealthCheckLog{}).Error
}
