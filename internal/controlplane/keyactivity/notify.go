package keyactivity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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
	// ts 来自 call_logs（数据库连接时区为 UTC），必须转北京时间再展示，
	// 否则邮件正文会比实际发生时间早 8 小时（与邮件尾行的本地时间不一致）。
	t := ts.In(shanghaiTz).Format("2006-01-02 15:04:05")
	// Key 名称缺失（Key 已删除或未命名）时给出可读占位，避免出现「你的 Key〔〕」。
	keyName = keyDisplayName(keyName)
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
		// 基线缺失（mean<=0）时不展示「基线约 0.0」这种无意义信息。
		baseline := ""
		if mean > 0 {
			baseline = fmt.Sprintf("（基线约 %.1f 次/小时）", mean)
		}
		return "Key 用量异常提醒",
			fmt.Sprintf("你的 Key〔%s〕用量异常：最近 1 小时调用 %d 次，明显高于你的日常水平%s。", keyName, count, baseline)
	}
	return "Key 活动提醒", fmt.Sprintf("Key〔%s〕检测到异常活动。", keyName)
}

// keyDisplayName 返回 Key 的可读展示名；名称为空时给出占位，
// 避免用户收到「你的 Key〔〕…」这种残缺文案。
func keyDisplayName(keyName string) string {
	if trimmed := strings.TrimSpace(keyName); trimmed != "" {
		return trimmed
	}
	return "（未命名 Key）"
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

// WakeupDedupKey 生成 E3 沉睡唤醒提醒的「每天一次」去重键（key_activity_alert.dedup_key）。
// t 通常来自 call_logs.ts（数据库连接时区为 UTC），必须转北京时间再取日期：
// 若按 UTC 日切分，北京时间早上 8 点就换日，会导致同一天重复提醒、或跨日少报。
func WakeupDedupKey(t time.Time) string {
	return t.In(shanghaiTz).Format("2006-01-02")
}
