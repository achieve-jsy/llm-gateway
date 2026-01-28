package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AuditLogRepo struct {
	db *gorm.DB
}

func NewAuditLogRepo(db *gorm.DB) *AuditLogRepo {
	return &AuditLogRepo{db: db}
}

// Create 写入一条审计日志（best-effort，由上层决定失败是否阻断）
func (r *AuditLogRepo) Create(log *model.AuditLog) error {
	return r.db.Create(log).Error
}

// CreateAsync 异步写审计，写失败仅打 warn 日志
func (r *AuditLogRepo) CreateAsync(log *model.AuditLog) {
	go func() {
		if err := r.Create(log); err != nil {
			// 审计失败不阻断业务，仅打日志
			_ = err
		}
	}()
}

// List 分页查询审计日志
func (r *AuditLogRepo) List(offset, limit int, userID *uuid.UUID, action, resourceType, deptID string, deptIDs []string, from, to *time.Time) ([]model.AuditLog, int64, error) {
	var logs []model.AuditLog
	var total int64

	query := r.db.Model(&model.AuditLog{})
	if userID != nil {
		query = query.Where("user_id = ?", *userID)
	}
	if action != "" {
		query = query.Where("action = ?", action)
	}
	if resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	}
	if deptID != "" {
		query = query.Where("dept_id = ?", deptID)
	} else if len(deptIDs) > 0 {
		query = query.Where("dept_id IN ?", deptIDs)
	}
	if from != nil {
		query = query.Where("created_at >= ?", *from)
	}
	if to != nil {
		query = query.Where("created_at <= ?", *to)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// DeleteBefore 清理历史日志（GDPR/容量保护）
func (r *AuditLogRepo) DeleteBefore(before time.Time) (int64, error) {
	res := r.db.Where("created_at < ?", before).Delete(&model.AuditLog{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
