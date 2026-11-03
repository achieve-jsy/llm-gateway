package service

import (
	"context"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// outboxNotifier 是 Notifier 的"入队"实现：Notify 只把通知写入 notification_outbox
// （一次 DB insert，~1ms）即返回，不直接发送。后台 worker（router.go 的 outbox
// 消费 goroutine）从队列取出后调用真实 Notify 完成 inapp/email/webhook 投递。
// 请求线程内的消费者注入此实现，避免同步等待飞书 HTTP / SMTP 拖慢响应。
type outboxNotifier struct {
	db *gorm.DB
}

// NewOutboxNotifier 创建只写 outbox 的 Notifier（消费者侧入队语义）。
func NewOutboxNotifier(db *gorm.DB) Notifier {
	return &outboxNotifier{db: db}
}

func (o *outboxNotifier) Notify(ctx context.Context, n *model.Notification) error {
	if o.db == nil || n == nil {
		return nil
	}
	key := n.DedupeKey
	if key == "" {
		// 无去重键的通知每次入队生成唯一键：同一条通知只入队一次，
		// 不同通知各自独立投递，与"不去重"语义一致。
		key = "outbox:" + uuid.NewString()
	}
	row := &model.NotificationOutbox{
		ID: uuid.New(), DedupeKey: key,
		UserID: n.UserID, Type: n.Type, Level: n.Level,
		Title: n.Title, Content: n.Content,
		RefType: n.RefType, RefID: n.RefID,
		TemplateData: n.TemplateData,
		Status:       "pending", NextAttempt: time.Now().UTC(),
	}
	return o.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedupe_key"}}, DoNothing: true}).
		Create(row).Error
}
