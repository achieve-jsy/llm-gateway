package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NotificationWebhookTargetRepo webhook 端点子表 CRUD。一个通知类型可有多个端点,告警 fan-out 到全部启用端点。
type NotificationWebhookTargetRepo struct {
	db *gorm.DB
}

func NewNotificationWebhookTargetRepo(db *gorm.DB) *NotificationWebhookTargetRepo {
	return &NotificationWebhookTargetRepo{db: db}
}

// ListAllTargets 全量列表,按类型 + sort_order 排序(矩阵状态点与对话框初始化共用)。
func (r *NotificationWebhookTargetRepo) ListAllTargets() ([]model.NotificationWebhookTarget, error) {
	var targets []model.NotificationWebhookTarget
	err := r.db.Order("notification_type, sort_order, created_at").Find(&targets).Error
	return targets, err
}

// ListEnabledByType 某类型下启用的端点(dispatchWebhook fan-out 用)。
func (r *NotificationWebhookTargetRepo) ListEnabledByType(notificationType string) ([]model.NotificationWebhookTarget, error) {
	var targets []model.NotificationWebhookTarget
	err := r.db.Where("notification_type = ? AND enabled = ?", notificationType, true).
		Order("sort_order, created_at").Find(&targets).Error
	return targets, err
}

// Get 获取单个端点。
func (r *NotificationWebhookTargetRepo) Get(id uuid.UUID) (*model.NotificationWebhookTarget, error) {
	var t model.NotificationWebhookTarget
	err := r.db.First(&t, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Create 创建端点。调用方需在 ID 为空时自行生成 uuid。
func (r *NotificationWebhookTargetRepo) Create(t *model.NotificationWebhookTarget) error {
	return r.db.Create(t).Error
}

// Update 更新端点(Save 全量覆写,含 url/secret)。
func (r *NotificationWebhookTargetRepo) Update(t *model.NotificationWebhookTarget) error {
	return r.db.Save(t).Error
}

// Delete 删除端点。
func (r *NotificationWebhookTargetRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&model.NotificationWebhookTarget{}, "id = ?", id).Error
}
