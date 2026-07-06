package main

// anomaly_feedback.go —— 方案 X（真反馈闭环）的 report 侧装配/读回接线。
//
// 在 derive.Derive + 两阶段/成员级归因完成之后调用 applyAnomalyFeedback：
//  1. 上报：把每条命中异常（Hit && !NotComputed）收成 (rule, window_start, member_id) 批量
//     POST /usage/report/anomalies（upsert，默认「待确认」，服务端不覆盖用户反馈态）。
//  2. 读回：GET /usage/report/anomaly-status?scope_level=&scope_id=&from=&to=，按
//     (rule, window_start, member_id) 三元组（分钟精度 + +08:00 时区）覆盖 AnomalyHit.Status，
//     使 CountAnomalyStatus 归桶出真实 5 态。
//
// 纪律：接口未建/不可用/加载失败时【静默降级】为「待确认」，绝不中断报告、绝不 panic。
// 窗口时间序列化统一用带显式 +08:00 的 RFC3339、且按【分钟】精度截断，避免秒/时区漂移导致匹配不上
// （沿用 t11 教训：裸 "2006-01-02 15:04:05" 会被服务端误判为 UTC 漂移 8 小时）。

import (
	"context"
	"log"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/tools/usage-report/client"
)

// anomalyMinuteTS 把命中窗口起点格式化为分钟精度的 +08:00 RFC3339（上报/读回匹配的统一口径）。
// 截断到分钟：秒/毫秒丢弃，避免服务端存储精度（timestamptz 秒级）与客户端序列化不一致导致匹配不上。
func anomalyMinuteTS(t time.Time) string {
	return t.In(shLoc).Truncate(time.Minute).Format(time.RFC3339)
}

// applyAnomalyFeedback 方案 X：上报命中异常 + 读回真实状态覆盖 AnomalyHit.Status。
// 任何一步失败都静默降级（上报跳过、读回保持当前 Status 即「待确认」），保证报告不因反馈接口异常中断。
func applyAnomalyFeedback(ctx context.Context, cli *client.Client, cfg Config, scope report.Scope, rep *report.Report) {
	if rep == nil || cli == nil {
		return
	}
	hits := rep.Judgment.Anomalies
	if len(hits) == 0 {
		return
	}

	// 1) 收集合规命中（Hit && !NotComputed），按分钟精度窗口起点收集上报载荷。
	upserts := make([]client.AnomalyUpsert, 0, len(hits))
	for i := range hits {
		a := &hits[i]
		if !a.Hit || a.NotComputed {
			continue
		}
		memberID := ""
		if a.Dimensions != nil {
			memberID = a.Dimensions["user_id"]
		}
		// 窗口可能为空（非按窗规则如 A3–A5）：仍上报，服务端按 scope+rule+member 维度登记。
		ws := ""
		if !a.WindowStart.IsZero() {
			ws = anomalyMinuteTS(a.WindowStart)
		}
		upserts = append(upserts, client.AnomalyUpsert{
			ScopeLevel:  cfg.Level,
			ScopeID:     scope.ID,
			Rule:        a.Rule,
			RuleVersion: a.RuleVersion,
			WindowStart: ws,
			MemberID:    memberID,
		})
	}
	if len(upserts) == 0 {
		return
	}
	if _, err := cli.PostReportAnomalies(ctx, upserts); err != nil {
		// 上报失败：尽力而为，不阻断；接口未建时自然降级（下次读回仍为默认态）。
		if cfg.Debug {
			log.Printf("anomaly-feedback upsert: %v", err)
		}
	}
	// 2) 读回真实状态，按 (rule, window_start, member_id) 覆盖 AnomalyHit.Status。
	//    读回失败（未建接口/网络）→ 静默降级：保持当前 Status（默认「待确认」）。
	rows, err := cli.FetchAnomalyStatus(ctx, cfg.Level, scope.ID, cfg.From, cfg.To)
	if err != nil {
		if cfg.Debug {
			log.Printf("anomaly-feedback readback: %v", err)
		}
		return
	}
	if len(rows) == 0 {
		return
	}
	// 三元组索引：(rule|window_start分钟|member_id) → status。window_start 同样按分钟精度归一。
	idx := make(map[string]string, len(rows))
	for _, r := range rows {
		ws := ""
		if t, err := time.Parse(time.RFC3339, r.WindowStart); err == nil {
			ws = anomalyMinuteTS(t)
		} else {
			ws = r.WindowStart // 归一失败则用原始串（与服务端回显一致）
		}
		idx[anomalyKey(r.Rule, ws, r.MemberID)] = r.Status
	}
	for i := range hits {
		a := &hits[i]
		if !a.Hit || a.NotComputed {
			continue
		}
		memberID := ""
		if a.Dimensions != nil {
			memberID = a.Dimensions["user_id"]
		}
		ws := ""
		if !a.WindowStart.IsZero() {
			ws = anomalyMinuteTS(a.WindowStart)
		}
		if st, ok := idx[anomalyKey(a.Rule, ws, memberID)]; ok && st != "" {
			a.Status = st
		}
	}
}

// anomalyKey 三元组序列化：rule|window_start|member_id。
func anomalyKey(rule, ws, memberID string) string {
	return rule + "|" + ws + "|" + memberID
}
