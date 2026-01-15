package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NotificationRepo 站内通知数据访问层。
type NotificationRepo struct {
	db *gorm.DB
}

func NewNotificationRepo(db *gorm.DB) *NotificationRepo {
	return &NotificationRepo{db: db}
}

// Create 写入一条通知。
func (r *NotificationRepo) Create(n *model.Notification) error {
	return r.db.Create(n).Error
}

// GetUnread 查询指定用户的未读通知列表，按创建时间倒序。
func (r *NotificationRepo) GetUnread(userID uuid.UUID) ([]model.Notification, error) {
	var items []model.Notification
	err := r.db.Where("user_id = ? AND is_read = false", userID).
		Order("created_at DESC").
		Limit(50).
		Find(&items).Error
	return items, err
}

// GetUnreadCount 查询指定用户的未读通知数量。
func (r *NotificationRepo) GetUnreadCount(userID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.Model(&model.Notification{}).
		Where("user_id = ? AND is_read = false", userID).
		Count(&count).Error
	return count, err
}

// MarkRead 将指定通知标记为已读。
func (r *NotificationRepo) MarkRead(userID, notificationID uuid.UUID) error {
	now := time.Now()
	return r.db.Model(&model.Notification{}).
		Where("id = ? AND user_id = ?", notificationID, userID).
		Updates(map[string]interface{}{"is_read": true, "read_at": now}).Error
}

// MarkAllRead 将指定用户所有未读通知标记为已读。
func (r *NotificationRepo) MarkAllRead(userID uuid.UUID) error {
	now := time.Now()
	return r.db.Model(&model.Notification{}).
		Where("user_id = ? AND is_read = false", userID).
		Updates(map[string]interface{}{"is_read": true, "read_at": now}).Error
}

// GetHistory 查询指定用户的已读通知，最近 30 条。
func (r *NotificationRepo) GetHistory(userID uuid.UUID) ([]model.Notification, error) {
	var items []model.Notification
	err := r.db.Where("user_id = ? AND is_read = true", userID).
		Order("read_at DESC").
		Limit(30).
		Find(&items).Error
	return items, err
}
