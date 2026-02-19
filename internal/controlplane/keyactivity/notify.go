package keyactivity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

var shanghaiTz, _ = time.LoadLocation("Asia/Shanghai")

// emitAlert 写提醒记录 + 按需发送通知。
// InsertAlert（唯一键去重）→ 若 RowsAffected=1 → 若 enabled → 发通知
func (s *Scanner) emitAlert(ctx context.Context, eventType string, row CallLogRow, fp, dedupKey, keyName string, detail string) {
	alert := &KeyActivityAlert{
		KeyID:      row.KeyID,
		UserID:     row.UserID,
		EventType:  eventType,
		DedupKey:   dedupKey,
		UARaw:      row.UserAgent,
		IP:         row.IPAddress,
		Detail:     detail,
		NotifiedAt: time.Now(),
	}

	isNew, err := s.kaRepo.InsertAlert(alert)
	if err != nil {
		slog.Warn("key_activity: insert_alert_failed", "key_id", row.KeyID, "event", eventType, "error", err)
		return
	}
	if !isNew {
		return
	}

	slog.Info("key_activity_alert", "key_id", row.KeyID, "event", eventType, "dedup", dedupKey)

	if !s.IsEnabled(ctx) {
		return
	}

	userUUID, err := uuid.Parse(row.UserID)
	if err != nil {
		slog.Warn("key_activity: invalid user_id, skip notification", "user_id", row.UserID, "key_id", row.KeyID)
		return
	}

	title, body := FormatNotification(eventType, keyName, row.UserAgent, row.IPAddress, row.TS, detail)
	level := EventLevel(eventType)
	if err := s.notifier.Notify(userUUID, title, body, level, "api_key", row.KeyID); err != nil {
		slog.Warn("key_activity: notify failed", "user_id", row.UserID, "event", eventType, "error", err)
	}
}

// FormatNotification 生成通知标题与正文（导出，供测试）
func FormatNotification(eventType, keyName, ua, ip string, ts time.Time, detail string) (string, string) {
	t := ts.Format("2006-01-02 15:04:05")
	switch eventType {
	case EventTypeNewSource:
		return "Key 新设备使用提醒",
			fmt.Sprintf("你的 Key〔%s〕检测到新设备使用：客户端 %s，IP %s，时间 %s。若非本人操作，请尽快重置 Key。", keyName, ua, ip, t)
	case EventTypeFirstUse:
		return "Key 首次使用",
			fmt.Sprintf("你的 Key〔%s〕首次被使用：客户端 %s，IP %s，时间 %s。", keyName, ua, ip, t)
	case EventTypeWakeup:
		return "Key 重新启用提醒",
			fmt.Sprintf("你的 Key〔%s〕沉睡 %d 天后重新被使用：客户端 %s，IP %s，时间 %s。", keyName, ParseDormantDays(detail), ua, ip, t)
	case EventTypeSpike:
		count, mean := ParseSpikeDetail(detail)
		return "Key 用量异常提醒",
			fmt.Sprintf("你的 Key〔%s〕用量异常：最近 1 小时调用 %d 次，明显高于你的日常水平（基线约 %.1f）。", keyName, count, mean)
	}
	return "Key 活动提醒", fmt.Sprintf("Key〔%s〕检测到异常活动。", keyName)
}

// EventLevel E1/E3/E4 → warning；E2 → info
func EventLevel(eventType string) string {
	if eventType == EventTypeFirstUse {
		return "info"
	}
	return "warning"
}

// ParseDormantDays 从 detail JSON 提取 dormant_days 字段（导出供测试）
func ParseDormantDays(detail string) int {
	if detail == "" {
		return 0
	}
	var m map[string]interface{}
	if json.Unmarshal([]byte(detail), &m) != nil {
		return 0
	}
	if v, ok := m["dormant_days"]; ok {
		if n, ok := v.(float64); ok {
			return int(n)
		}
	}
	return 0
}

// ParseSpikeDetail 从 detail JSON 提取 count 和 mean 字段（导出供测试）
func ParseSpikeDetail(detail string) (int, float64) {
	if detail == "" {
		return 0, 0
	}
	var m map[string]interface{}
	if json.Unmarshal([]byte(detail), &m) != nil {
		return 0, 0
	}
	var count int
	var mean float64
	if v, ok := m["count"]; ok {
		if n, ok := v.(float64); ok {
			count = int(n)
		}
	}
	if v, ok := m["mean"]; ok {
		if n, ok := v.(float64); ok {
			mean = n
		}
	}
	return count, mean
}

func hourBucketKey(t time.Time) string {
	sh := t.In(shanghaiTz)
	return fmt.Sprintf("%04d-%02d-%02d-%02d", sh.Year(), sh.Month(), sh.Day(), sh.Hour())
}

func hourBucketTime(t time.Time) time.Time {
	sh := t.In(shanghaiTz)
	return time.Date(sh.Year(), sh.Month(), sh.Day(), sh.Hour(), 0, 0, 0, shanghaiTz)
}
