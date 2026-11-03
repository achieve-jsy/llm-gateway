package service

import (
	"context"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type LifecycleOwnerResolver interface {
	ResolveOwnerRecipients(context.Context, uuid.UUID) ([]uuid.UUID, error)
}

type BudgetLifecycleNotificationObserver struct {
	db         *gorm.DB
	expansions *repository.ExpansionRepo
	owners     LifecycleOwnerResolver
	notifier   Notifier
	leaseOwner string
}

var _ BudgetLifecycleObserver = (*BudgetLifecycleNotificationObserver)(nil)

// maxOutboxAttempts 单条通知的最大投递尝试次数。达到后标记为 deadletter，
// 不再重试——飞书 webhook 配置错误等持续失败的行不会无限占用消费队列
// （每分钟重试 + attempts 无限增长），在弱一致语义下放弃即可。
const maxOutboxAttempts = 5

func (o *BudgetLifecycleNotificationObserver) RetryPending(ctx context.Context, limit int) error {
	if o.notifier == nil {
		return nil
	}
	now := time.Now().UTC()
	var rows []model.NotificationOutbox
	deadlettered := make(map[uuid.UUID]struct{})
	err := o.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.NotificationOutbox{}).
			Where("status = ? AND lease_expires_at > 0 AND lease_expires_at < ?", "processing", now.UnixMicro()).
			Updates(map[string]any{"status": "failed", "next_attempt": now, "locked_at": nil, "lease_owner": "", "lease_expires_at": 0, "last_error": "processing lease expired"}).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status IN ? AND next_attempt <= ?", []string{"pending", "failed"}, now).
			Order("next_attempt").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.Attempts >= maxOutboxAttempts {
				deadlettered[row.ID] = struct{}{}
				if err := tx.Model(&model.NotificationOutbox{}).Where("id = ?", row.ID).
					Updates(map[string]any{"status": "deadletter", "last_error": "max attempts exceeded", "locked_at": nil, "lease_owner": "", "lease_expires_at": 0}).Error; err != nil {
					return err
				}
				continue
			}
			if err := tx.Model(&model.NotificationOutbox{}).Where("id = ?", row.ID).
				Updates(map[string]any{"status": "processing", "attempts": gorm.Expr("attempts + 1"), "locked_at": now, "lease_owner": o.leaseOwner, "lease_expires_at": now.Add(5 * time.Minute).UnixMicro()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, ok := deadlettered[row.ID]; ok {
			continue
		}
		notification := &model.Notification{
			UserID: row.UserID, Type: row.Type, Level: row.Level, Title: row.Title,
			Content: row.Content, RefType: row.RefType, RefID: row.RefID,
			Source: "outbox", DedupeKey: row.DedupeKey,
			TemplateData: row.TemplateData,
		}
		if err := o.notifier.Notify(ctx, notification); err != nil {
			_ = o.db.WithContext(ctx).Model(&model.NotificationOutbox{}).Where("id = ? AND lease_owner = ?", row.ID, o.leaseOwner).
				Updates(map[string]any{"status": "failed", "last_error": err.Error(), "next_attempt": time.Now().UTC().Add(time.Minute), "locked_at": nil, "lease_owner": "", "lease_expires_at": 0}).Error
			continue
		}
		now := time.Now().UTC()
		if err := o.db.WithContext(ctx).Model(&model.NotificationOutbox{}).Where("id = ? AND lease_owner = ?", row.ID, o.leaseOwner).
			Updates(map[string]any{"status": "delivered", "delivered_at": now, "last_error": "", "locked_at": nil, "lease_owner": "", "lease_expires_at": 0}).Error; err != nil {
			return err
		}
	}
	return nil
}

func NewBudgetLifecycleNotificationObserver(
	db *gorm.DB,
	expansions *repository.ExpansionRepo,
	owners LifecycleOwnerResolver,
	notifier Notifier,
) *BudgetLifecycleNotificationObserver {
	return &BudgetLifecycleNotificationObserver{
		db: db, expansions: expansions, owners: owners, notifier: notifier, leaseOwner: uuid.NewString(),
	}
}

func (o *BudgetLifecycleNotificationObserver) OnOverrideExpired(
	ctx context.Context,
	item ExpiredBudgetOverride,
) error {
	recipients := make([]uuid.UUID, 0, 4)
	if item.ExpansionRequestID != nil {
		request, err := o.expansions.FindByID(item.ExpansionRequestID.String())
		if err != nil {
			return err
		}
		recipients = append(recipients, request.RequesterID)
	}
	owners, err := o.owners.ResolveOwnerRecipients(ctx, item.BudgetAllocationID)
	if err != nil {
		return err
	}
	recipients = append(recipients, owners...)
	return o.notifyRecipients(ctx, "override-expired:"+item.ID.String(), recipients, &model.Notification{
		Type: model.NotificationTypeExpansionRestored, Level: model.NotificationLevelInfo,
		Title: "临时扩容已到期",
		RefType: "budget_override", RefID: item.ID.String(),
		TemplateData: map[string]interface{}{"ExpireTime": item.ExpiredAt.Format(time.RFC3339)},
	})
}

func (o *BudgetLifecycleNotificationObserver) OnScheduleApplied(
	ctx context.Context,
	item AppliedBudgetSchedule,
) error {
	return o.notifySchedule(ctx, item.ID, item.BudgetAllocationID, model.NotificationTypeBudgetScheduleApplied, "", item.AppliedAt)
}

func (o *BudgetLifecycleNotificationObserver) OnScheduleFailed(
	ctx context.Context,
	item FailedBudgetSchedule,
) error {
	return o.notifySchedule(ctx, item.ID, item.BudgetAllocationID, model.NotificationTypeBudgetScheduleFailed, item.FailureReason, item.FailedAt)
}

func (o *BudgetLifecycleNotificationObserver) notifySchedule(
	ctx context.Context,
	scheduleID, allocationID uuid.UUID,
	notificationType, failureReason string,
	at time.Time,
) error {
	var schedule model.BudgetLimitSchedule
	if err := o.db.WithContext(ctx).Select("created_by", "dimension_type", "period_type").
		First(&schedule, "id = ?", scheduleID).Error; err != nil {
		return err
	}
	recipients := []uuid.UUID{schedule.CreatedBy}
	owners, err := o.owners.ResolveOwnerRecipients(ctx, allocationID)
	if err != nil {
		return err
	}
	recipients = append(recipients, owners...)
	title := "预算计划已生效"
	level := model.NotificationLevelInfo
	data := map[string]interface{}{
		"PeriodType": schedule.PeriodType,
		"AppliedAt":  at.Format(time.RFC3339),
	}
	if notificationType == model.NotificationTypeBudgetScheduleFailed {
		title, level = "预算计划生效失败", model.NotificationLevelCritical
		data["Reason"] = failureReason
	}
	return o.notifyRecipients(ctx, "schedule:"+notificationType+":"+scheduleID.String(), recipients, &model.Notification{
		Type: notificationType, Level: level, Title: title,
		RefType: "budget_schedule", RefID: scheduleID.String(),
		TemplateData: data,
	})
}

func (o *BudgetLifecycleNotificationObserver) notifyRecipients(
	ctx context.Context,
	keyPrefix string,
	recipients []uuid.UUID,
	template *model.Notification,
) error {
	if o.notifier == nil {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(recipients))
	for _, recipient := range recipients {
		if recipient == uuid.Nil {
			continue
		}
		if _, ok := seen[recipient]; ok {
			continue
		}
		seen[recipient] = struct{}{}
		key := keyPrefix + ":" + recipient.String()
		notification := *template
		notification.UserID = recipient
		outbox := &model.NotificationOutbox{
			ID: uuid.New(), DedupeKey: key, UserID: recipient, Type: notification.Type,
			Level: notification.Level, Title: notification.Title, Content: notification.Content,
			RefType: notification.RefType, RefID: notification.RefID,
			TemplateData: notification.TemplateData,
			Status:       "pending", NextAttempt: time.Now().UTC(),
		}
		if err := o.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedupe_key"}}, DoNothing: true}).Create(outbox).Error; err != nil {
			return err
		}
	}
	// 只入队，不在此同步投递。后台 worker（router.go outbox 消费 goroutine）
	// 周期调用 RetryPending 完成真实分发，避免生命周期 RunOnce 阻塞在飞书 HTTP / SMTP 上。
	return nil
}
