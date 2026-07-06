package derive

// derive_member_cause_test.go —— MemberAnomalyCause 成员级主因归因（方向 B）的白盒单测。
//
// 背景：TopChanged 按 |WoWCostPct| 降序取 Top5（两期都有的成员），而全局窗口热点 user 常不在 Top5 →
// correlateTopChangedAnomaly 匹配不到 → 主因列「待确认」。方向 B 改为对每个 TopChanged 成员用其**本人**
// 调用流做 A1(程序失控)/A2(成本燃烧)/A5(非工作时段) 判定，命中即回填（如 "A2 成本燃烧"）。
//
// 本测试验证 MemberAnomalyCause 纯函数：空入参/无命中返回 ""；单规则命中返回 rule+name；
// 多规则命中按 "; " 连接。

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
)

// TestMemberAnomalyCause_Empty: 空调用不 panic，返回 ""。
func TestMemberAnomalyCause_Empty(t *testing.T) {
	rules := report.DefaultRuleSet()
	if got := MemberAnomalyCause(nil, rules); got != "" {
		t.Errorf("MemberAnomalyCause(nil)=%q, want empty", got)
	}
}

// TestMemberAnomalyCause_A2Hit: 15min 窗口成本超 A2AbsMinUSD → A2 成本燃烧。
func TestMemberAnomalyCause_A2Hit(t *testing.T) {
	rules := report.DefaultRuleSet()
	start := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	// 窗口成本 70+30=100 > A2AbsMinUSD(8)，无历史 → baseThr=max(0,8)=8。
	calls := []report.CallEvent{
		{TS: start, UserID: "uC", Status: 200, EstCostUSD: 70},
		{TS: start.Add(time.Second), UserID: "uC", Status: 200, EstCostUSD: 30},
	}
	if got := MemberAnomalyCause(calls, rules); got != "A2 成本燃烧" {
		t.Errorf("MemberAnomalyCause=%q, want %q", got, "A2 成本燃烧")
	}
}

// TestMemberAnomalyCause_MultipleHits: A1+A2 同时命中 → "; " 连接。
func TestMemberAnomalyCause_MultipleHits(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.A1SustainMinutes = 1
	rules.A1MinCallsAbsolute = 2
	start := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	// A1：窗口调用 3 > max(2, 历史p99=0)，持续 1min → 程序失控。A2：窗口成本 100 > 8 → 成本燃烧。
	calls := []report.CallEvent{
		{TS: start, UserID: "uA", Status: 200, EstCostUSD: 40},
		{TS: start.Add(time.Second), UserID: "uA", Status: 200, EstCostUSD: 30},
		{TS: start.Add(2 * time.Second), UserID: "uA", Status: 200, EstCostUSD: 30},
	}
	if got := MemberAnomalyCause(calls, rules); got != "A1 程序失控; A2 成本燃烧" {
		t.Errorf("MemberAnomalyCause=%q, want %q", got, "A1 程序失控; A2 成本燃烧")
	}
}

// TestMemberAnomalyCause_NoHit: 调用量小且成本未超阈 → 无命中返回 ""。
func TestMemberAnomalyCause_NoHit(t *testing.T) {
	rules := report.DefaultRuleSet()
	start := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	// 2 条调用，成本极低；不满足 A1(需 >A1MinCallsAbsolute)也不满足 A2(需 >8)；A5 数量不足。
	calls := []report.CallEvent{
		{TS: start, UserID: "u1", Status: 200, EstCostUSD: 0.01},
		{TS: start.Add(time.Second), UserID: "u1", Status: 200, EstCostUSD: 0.01},
	}
	if got := MemberAnomalyCause(calls, rules); got != "" {
		t.Errorf("MemberAnomalyCause=%q, want empty（无命中）", got)
	}
}

// TestMemberAnomalyCause_A5Hit: 非工作时段(凌晨)成本超 floor → A5 非工作时段。
func TestMemberAnomalyCause_A5Hit(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.A5MinCallCount = 2
	rules.A5FloorCNY = 1 // ¥1 几乎为 0，便于命中
	start := time.Date(2026, 8, 3, 3, 0, 0, 0, time.UTC) // 凌晨 03:00，非工作时段
	// offCost=100 USD=¥720 远超 floor ¥1 → A5 命中。窗口内成本亦超 A2(8) → 命中规则为 A2; A5。
	calls := []report.CallEvent{
		{TS: start, UserID: "u1", Status: 200, EstCostUSD: 60},
		{TS: start.Add(time.Second), UserID: "u1", Status: 200, EstCostUSD: 40},
	}
	if got := MemberAnomalyCause(calls, rules); got != "A2 成本燃烧; A5 非工作时段" {
		t.Errorf("MemberAnomalyCause=%q, want %q", got, "A2 成本燃烧; A5 非工作时段")
	}
}
