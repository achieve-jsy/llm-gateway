package service

import (
	"context"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// acquireDelivery 按 (dedupe_key, channel) 尝试获取一次投递许可。
// 返回 shouldSend=false 表示该 key 已投递成功，应跳过本次发送（避免重复）。
// dedupeKey 为空时总是允许发送（不落投递记录）。
// n 可 nil；非 nil 时将类型/标题/内容/接收人冗余进投递行，供管理端通知历史页展示。
func acquireDelivery(ctx context.Context, db *gorm.DB, dedupeKey, channel string, n *model.Notification) (shouldSend bool, persisted model.NotificationDelivery, err error) {
	if dedupeKey == "" || db == nil {
		return true, model.NotificationDelivery{}, nil
	}

	delivery := model.NotificationDelivery{
		ID:        uuid.New(),
		DedupeKey: dedupeKey,
		Channel:   channel,
		Status:    "pending",
	}
	if n != nil {
		delivery.NotificationType = n.Type
		delivery.Title = n.Title
		delivery.Content = n.Content
		delivery.UserID = n.UserID
	}
	if err := db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedupe_key"}, {Name: "channel"}}, DoNothing: true}).
		Create(&delivery).Error; err != nil {
		return false, model.NotificationDelivery{}, err
	}
	if err := db.WithContext(ctx).
		Where("dedupe_key = ? AND channel = ?", dedupeKey, channel).
		First(&persisted).Error; err != nil {
		return false, model.NotificationDelivery{}, err
	}
	if persisted.Status == "delivered" {
		return false, persisted, nil
	}
	return true, persisted, nil
}

// settleDelivery 记录一次投递结果：成功置 delivered，失败累计 attempts 并记录错误。
// 发送失败必须向上传播：否则 Notify 返回 nil，outbox 观察者会误标 delivered 而永不重试。
func settleDelivery(ctx context.Context, db *gorm.DB, persisted model.NotificationDelivery, sendErr error) error {
	updates := map[string]any{"attempts": gorm.Expr("attempts + 1")}
	if sendErr != nil {
		updates["status"], updates["last_error"] = "failed", sendErr.Error()
	} else {
		now := time.Now().UTC()
		updates["status"], updates["last_error"], updates["delivered_at"] = "delivered", "", now
	}
	if db != nil {
		if err := db.WithContext(ctx).Model(&persisted).Updates(updates).Error; err != nil {
			return err
		}
	}
	return sendErr
}
