package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

// OpsAlertSender 系统级运维告警发送器：ops_* 告警统一用 UserID=uuid.Nil（系统通知），
// email 对 uuid.Nil 早退、inapp 写入不可见行，webhook 是唯一实际送达渠道。
// 去重键 `ops:<kind>:<scopeKey>:<YYYYMMDDHH>` 小时级冷却，依赖 outbox OnConflict(dedupe_key)
// 去重（webhook 本身不去重）。
type OpsAlertSender struct {
	notifier Notifier
}

func NewOpsAlertSender(notifier Notifier) *OpsAlertSender {
	return &OpsAlertSender{notifier: notifier}
}

// NotifyChannelDown 渠道/模型探活失败告警（critical）。
// source: provider_health（health check 连续失败自动禁用）| channel_monitor（探活失败/错误）。
func (s *OpsAlertSender) NotifyChannelDown(ctx context.Context, source, providerName string, providerID uuid.UUID, monitorName string, monitorID int64, failures, threshold int, errMsg string) {
	if s == nil || s.notifier == nil {
		return
	}
	scopeKey := fmt.Sprintf("%d", monitorID)
	if monitorID == 0 {
		scopeKey = providerID.String()
	}
	dedupeKey := fmt.Sprintf("ops:channel_down:%s:%s:%s", source, scopeKey, time.Now().Format("2006-01-02-15"))

	data := map[string]interface{}{
		"Source":       source,
		"ProviderName": providerName,
		"MonitorName":  monitorName,
		"Failures":     failures,
		"Threshold":    threshold,
	}
	if providerID != uuid.Nil {
		data["ProviderID"] = providerID.String()
	}
	if monitorID > 0 {
		data["MonitorID"] = monitorID
	}
	if errMsg != "" {
		data["Error"] = errMsg
	}
	s.send(ctx, model.NotificationTypeOpsChannelDown, model.NotificationLevelCritical, data, dedupeKey)
}

// NotifyChannelRecovered 渠道自动恢复通知（info）。
func (s *OpsAlertSender) NotifyChannelRecovered(ctx context.Context, source, providerName string, providerID uuid.UUID, monitorName string, monitorID int64) {
	if s == nil || s.notifier == nil {
		return
	}
	scopeKey := fmt.Sprintf("%d", monitorID)
	if monitorID == 0 {
		scopeKey = providerID.String()
	}
	dedupeKey := fmt.Sprintf("ops:channel_recovered:%s:%s:%s", source, scopeKey, time.Now().Format("2006-01-02-15"))

	data := map[string]interface{}{
		"Source":       source,
		"ProviderName": providerName,
		"MonitorName":  monitorName,
	}
	if providerID != uuid.Nil {
		data["ProviderID"] = providerID.String()
	}
	if monitorID > 0 {
		data["MonitorID"] = monitorID
	}
	s.send(ctx, model.NotificationTypeOpsChannelRecovered, model.NotificationLevelInfo, data, dedupeKey)
}

// NotifyDeliveryFailed 通知投递死信告警（critical）：outbox 重试耗尽转 deadletter 时触发。
// 按被死信行的 Type 聚合去重，同小时递归最多 1 层即被 OnConflict(dedupe_key) 拦截。
func (s *OpsAlertSender) NotifyDeliveryFailed(ctx context.Context, notifType, title, reason string, attempts int) {
	if s == nil || s.notifier == nil {
		return
	}
	dedupeKey := fmt.Sprintf("ops:delivery_failed:%s:%s", notifType, time.Now().Format("2006-01-02-15"))

	data := map[string]interface{}{
		"Type":     notifType,
		"Title":    title,
		"Reason":   reason,
		"Attempts": attempts,
	}
	s.send(ctx, model.NotificationTypeOpsDeliveryFailed, model.NotificationLevelCritical, data, dedupeKey)
}

func (s *OpsAlertSender) send(ctx context.Context, notifType, level string, data map[string]interface{}, dedupeKey string) {
	notification := &model.Notification{
		UserID:    uuid.Nil, // 系统通知
		Type:      notifType,
		Level:     level,
		DedupeKey: dedupeKey,
		CreatedAt: time.Now(),
		TemplateData: data,
	}
	if err := s.notifier.Notify(ctx, notification); err != nil {
		slog.Error("ops_alert_sender: send failed", "type", notifType, "dedupe_key", dedupeKey, "error", err)
	} else {
		slog.Debug("ops_alert_sender: sent", "type", notifType, "dedupe_key", dedupeKey)
	}
}
