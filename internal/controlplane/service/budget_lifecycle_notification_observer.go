package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/notification"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// formatNotificationTime 渲染通知正文里的时间：业务时区、分钟精度。
// 实现放在 internal/notification，站内信、邮件、报表共用同一套渲染。
func formatNotificationTime(t time.Time) string {
	return notification.FormatLocalTime(t)
}

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

func (o *BudgetLifecycleNotificationObserver) RetryPending(ctx context.Context, limit int) error {
	if o.notifier == nil {
		return nil
	}
	now := time.Now().UTC()
	var rows []model.NotificationOutbox
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
		notification := &model.Notification{
			UserID: row.UserID, Type: row.Type, Level: row.Level, Title: row.Title,
			Content: row.Content, RefType: row.RefType, RefID: row.RefID,
			Source: "outbox", DedupeKey: row.DedupeKey,
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
		Type: "expansion_restored", Level: model.NotificationLevelInfo,
		Title: "临时扩容已到期", Content: fmt.Sprintf(
			"临时额度已于 %s 到期，当前额度恢复为届时生效的长期基础额度。",
			formatNotificationTime(item.ExpiredAt),
		),
		RefType: "budget_override", RefID: item.ID.String(),
	})
}

func (o *BudgetLifecycleNotificationObserver) OnScheduleApplied(
	ctx context.Context,
	item AppliedBudgetSchedule,
) error {
	return o.notifySchedule(ctx, item.ID, item.BudgetAllocationID, "budget_schedule_applied", "", item.AppliedAt)
}

func (o *BudgetLifecycleNotificationObserver) OnScheduleFailed(
	ctx context.Context,
	item FailedBudgetSchedule,
) error {
	return o.notifySchedule(ctx, item.ID, item.BudgetAllocationID, "budget_schedule_failed", item.FailureReason, item.FailedAt)
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
	content := fmt.Sprintf("%s预算计划已于 %s 生效。",
		budgetAlertPeriodWord(schedule.PeriodType), formatNotificationTime(at))
	level := model.NotificationLevelInfo
	if notificationType == "budget_schedule_failed" {
		title, level = "预算计划生效失败", model.NotificationLevelCritical
		// failureReason 来自执行器，是原始 Go/gorm 错误串（英文，可能含 UUID 或 SQL 片段），
		// 因此只写日志、不进通知正文；管理面仍可从预算计划详情查看失败原因。
		slog.Warn("budget_schedule: apply failed",
			"schedule_id", scheduleID.String(),
			"period_type", string(schedule.PeriodType),
			"reason", strings.TrimSpace(failureReason))
		content = fmt.Sprintf("%s预算计划于 %s 生效失败，请进入预算管理检查配置后重新提交。",
			budgetAlertPeriodWord(schedule.PeriodType), formatNotificationTime(at))
	}
	return o.notifyRecipients(ctx, "schedule:"+notificationType+":"+scheduleID.String(), recipients, &model.Notification{
		Type: notificationType, Level: level, Title: title, Content: content,
		RefType: "budget_schedule", RefID: scheduleID.String(),
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
			Status: "pending", NextAttempt: time.Now().UTC(),
		}
		if err := o.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedupe_key"}}, DoNothing: true}).Create(outbox).Error; err != nil {
			return err
		}
	}
	return o.RetryPending(ctx, len(seen))
}
