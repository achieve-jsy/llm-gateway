package derive

// derive_anomaly_user_test.go —— A1/A2/A3 命中后「窗口内成本最高 user」归因写 Dimensions["user_id"] 的白盒单测。
//
// 覆盖 t7「方案 A」：
//  1. detail 模式（无 currentBuckets）：A1/A2 命中窗口内按 UserID 归并 CostUSD，Dimensions["user_id"] =
//     窗口成本最高 user，另写 user_id_basis=window_max_cost 与 user_cost_share（该 user 占窗口成本比）。
//  2. -agg 桶模式（currentBuckets 非空，calls 空）：A1/A2 走 weightedWindows，无逐调用 → 不写 user_id（降级）。
//  3. A3：段窗口内失败调用(非200)归因到成本最高 user。

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
)

// findAnomalyHit 在命中列表里按规则名取回该规则的判定结果（白盒测试本地 helper）。
func findAnomalyHit(hits []report.AnomalyHit, rule string) *report.AnomalyHit {
	for i := range hits {
		if hits[i].Rule == rule {
			return &hits[i]
		}
	}
	return nil
}

func TestA1WindowAttribution_WritesTopUser(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.A1SustainMinutes = 1
	rules.A1MinCallsAbsolute = 2
	start := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	// uA 窗口内成本 100 > uB 40；uA 应为窗口成本最高 user。
	calls := []report.CallEvent{
		{TS: start, UserID: "uA", Status: 200, EstCostUSD: 40},
		{TS: start.Add(time.Second), UserID: "uA", Status: 200, EstCostUSD: 30},
		{TS: start.Add(2 * time.Second), UserID: "uA", Status: 200, EstCostUSD: 30},
		{TS: start.Add(3 * time.Second), UserID: "uB", Status: 200, EstCostUSD: 20},
		{TS: start.Add(4 * time.Second), UserID: "uB", Status: 200, EstCostUSD: 10},
		{TS: start.Add(5 * time.Second), UserID: "uB", Status: 200, EstCostUSD: 10},
	}
	a1 := findAnomalyHit(Anomalies(calls, nil, nil, nil, rules, nil), "A1")
	if a1 == nil || !a1.Hit {
		t.Fatalf("A1 should hit: %+v", a1)
	}
	if a1.Dimensions["user_id"] != "uA" {
		t.Errorf("A1 user_id=%q, want uA (window max cost)", a1.Dimensions["user_id"])
	}
	if a1.Dimensions["user_id_basis"] != "window_max_cost" {
		t.Errorf("A1 user_id_basis=%q, want window_max_cost", a1.Dimensions["user_id_basis"])
	}
	// 结构化命中窗口：起点=首个调用所在分钟（truncate），终点=起点+A1WindowMinutes(10)。
	wantWS := start.Truncate(time.Minute)
	wantWE := wantWS.Add(time.Duration(rules.A1WindowMinutes) * time.Minute)
	if a1.WindowStart.IsZero() || a1.WindowEnd.IsZero() {
		t.Fatalf("A1 must set WindowStart/WindowEnd: %+v", a1)
	}
	if !a1.WindowStart.Equal(wantWS) || !a1.WindowEnd.Equal(wantWE) {
		t.Errorf("A1 WindowStart/End = %v/%v, want %v/%v", a1.WindowStart, a1.WindowEnd, wantWS, wantWE)
	}
	// uA 成本 100 / 窗口总成本 140 ≈ 0.714...
	wantShare := 100.0 / 140.0
	if got := a1.Dimensions["user_cost_share"]; got == "" || parseShare(t, got, wantShare) {
		t.Errorf("A1 user_cost_share=%q, want ~%.4f", got, wantShare)
	}
}

func TestA2WindowAttribution_WritesTopUser(t *testing.T) {
	rules := report.DefaultRuleSet()
	start := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	// A2 无历史：baseThr = max(0, A2AbsMinUSD=8)。窗口成本必须 >8。
	calls := []report.CallEvent{
		{TS: start, UserID: "uC", Status: 200, EstCostUSD: 70},
		{TS: start.Add(time.Second), UserID: "uC", Status: 200, EstCostUSD: 30},
		{TS: start.Add(2 * time.Second), UserID: "uD", Status: 200, EstCostUSD: 8},
		{TS: start.Add(3 * time.Second), UserID: "uD", Status: 200, EstCostUSD: 2},
	}
	a2 := findAnomalyHit(Anomalies(calls, nil, nil, nil, rules, nil), "A2")
	if a2 == nil || !a2.Hit {
		t.Fatalf("A2 should hit: %+v", a2)
	}
	if a2.Dimensions["user_id"] != "uC" {
		t.Errorf("A2 user_id=%q, want uC (window max cost)", a2.Dimensions["user_id"])
	}
	if a2.Dimensions["user_id_basis"] != "window_max_cost" {
		t.Errorf("A2 user_id_basis=%q, want window_max_cost", a2.Dimensions["user_id_basis"])
	}
	// 结构化命中窗口：起点=首个调用所在分钟（truncate），终点=起点+A2WindowMinutes(15)。
	wantWS := start.Truncate(time.Minute)
	wantWE := wantWS.Add(time.Duration(rules.A2WindowMinutes) * time.Minute)
	if a2.WindowStart.IsZero() || a2.WindowEnd.IsZero() {
		t.Fatalf("A2 must set WindowStart/WindowEnd: %+v", a2)
	}
	if !a2.WindowStart.Equal(wantWS) || !a2.WindowEnd.Equal(wantWE) {
		t.Errorf("A2 WindowStart/End = %v/%v, want %v/%v", a2.WindowStart, a2.WindowEnd, wantWS, wantWE)
	}
	// uC 成本 100 / 窗口总成本 110 ≈ 0.909...
	wantShare := 100.0 / 110.0
	if got := a2.Dimensions["user_cost_share"]; got == "" || parseShare(t, got, wantShare) {
		t.Errorf("A2 user_cost_share=%q, want ~%.4f", got, wantShare)
	}
}

// -agg 桶模式（currentBuckets 非空、calls 空）：A1 走 weightedWindows，无逐调用 → 不写 user_id（降级）。
func TestA1BucketMode_NoUserAttribution(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.A1SustainMinutes = 1
	rules.A1MinCallsAbsolute = 2
	start := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	buckets := []report.MinuteBucket{{Minute: start, Count: 3, Cost: 100}}
	a1 := findAnomalyHit(Anomalies(nil, nil, nil, buckets, rules, nil), "A1")
	if a1 == nil || !a1.Hit {
		t.Fatalf("A1 bucket mode should hit: %+v", a1)
	}
	if a1.Dimensions["bucket_mode"] != "1" {
		t.Errorf("A1 bucket_mode=%q, want 1", a1.Dimensions["bucket_mode"])
	}
	if uid := a1.Dimensions["user_id"]; uid != "" {
		t.Errorf("A1 bucket mode must not write user_id (no per-call data), got %q", uid)
	}
	if _, ok := a1.Dimensions["user_cost_share"]; ok {
		t.Errorf("A1 bucket mode must not write user_cost_share")
	}
	// 桶模式仍应写出结构化命中窗口（供两阶段按窗精准拉取归因）。
	wantWS := start.Truncate(time.Minute)
	wantWE := wantWS.Add(time.Duration(rules.A1WindowMinutes) * time.Minute)
	if a1.WindowStart.IsZero() || a1.WindowEnd.IsZero() {
		t.Fatalf("A1 bucket mode must set WindowStart/WindowEnd: %+v", a1)
	}
	if !a1.WindowStart.Equal(wantWS) || !a1.WindowEnd.Equal(wantWE) {
		t.Errorf("A1 bucket WindowStart/End = %v/%v, want %v/%v", a1.WindowStart, a1.WindowEnd, wantWS, wantWE)
	}
}

func TestTopUserByCost_ReusableAttribution(t *testing.T) {
	// TopUserByCost 是对外可复用的窗口内成本归因（供工具/装配层按需精准拉取后调用）。
	calls := []report.CallEvent{
		{UserID: "uA", EstCostUSD: 70}, {UserID: "uA", EstCostUSD: 30},
		{UserID: "uB", EstCostUSD: 8}, {UserID: "uB", EstCostUSD: 2},
	}
	uid, share := TopUserByCost(calls)
	if uid != "uA" {
		t.Errorf("TopUserByCost uid=%q, want uA", uid)
	}
	want := 100.0 / 110.0
	if diff := share - want; diff < -0.001 || diff > 0.001 {
		t.Errorf("TopUserByCost share=%.4f, want ~%.4f", share, want)
	}
	// 空 user / 空集降级。
	if uid, _ := TopUserByCost([]report.CallEvent{{UserID: "", EstCostUSD: 5}}); uid != "" {
		t.Errorf("empty-user calls must return empty uid, got %q", uid)
	}
	if uid, _ := TopUserByCost(nil); uid != "" {
		t.Errorf("nil calls must return empty uid, got %q", uid)
	}
}

func TestA3WindowAttribution_WritesFailingTopUser(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.A3WindowMinutes = 5
	start := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	// dense 窗口内 20 条失败调用，KeyID/model 相同；uX 成本最高 → 归因 uX。
	var calls []report.CallEvent
	for i := 0; i < 20; i++ {
		uid := "uX"
		cost := 10.0
		if i%3 == 0 {
			uid = "uY"
			cost = 1.0
		}
		calls = append(calls, report.CallEvent{TS: start.Add(time.Duration(i) * time.Second), KeyID: "k", Model: "m", UserID: uid, Status: 0, EstCostUSD: cost})
	}
	a3 := findAnomalyHit(Anomalies(calls, nil, nil, nil, rules, nil), "A3")
	if a3 == nil || !a3.Hit {
		t.Fatalf("A3 should hit: %+v", a3)
	}
	if a3.Dimensions["user_id"] != "uX" {
		t.Errorf("A3 user_id=%q, want uX (failing window max cost)", a3.Dimensions["user_id"])
	}
	if a3.Dimensions["user_id_basis"] != "window_max_cost" {
		t.Errorf("A3 user_id_basis=%q, want window_max_cost", a3.Dimensions["user_id_basis"])
	}
}

// parseShare 解析 Dimensions["user_cost_share"]（0~1 小数），与期望值容差比较，返回 true 表示不匹配。
func parseShare(t *testing.T, s string, want float64) bool {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Logf("user_cost_share=%q not parseable: %v", s, err)
		return true
	}
	if diff := v - want; diff < -0.01 || diff > 0.01 {
		t.Logf("user_cost_share=%.4f, want ~%.4f", v, want)
		return true
	}
	return false
}

// TestMedianIntervalSec 覆盖「尽力而为」聚合桶命中窗口后的逐调用中位间隔校验（zero-extra-fetch）。
// 用于 A1 bucket 命中窗按窗拉取后确认持续性；调用不足 2 条返回 +Inf（语义=不可判定）。
func TestMedianIntervalSec(t *testing.T) {
	start := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		calls []report.CallEvent
		want  float64 // 期望中位间隔（秒）
	}{
		{"constant 1s interval", []report.CallEvent{
			{TS: start}, {TS: start.Add(1 * time.Second)}, {TS: start.Add(2 * time.Second)}, {TS: start.Add(3 * time.Second)},
		}, 1.0},
		{"1,2,3s", []report.CallEvent{
			{TS: start}, {TS: start.Add(1 * time.Second)}, {TS: start.Add(3 * time.Second)}, {TS: start.Add(6 * time.Second)},
		}, 2.0}, // 间隔 [1,2,3] 中位=2
		{"<2 calls", []report.CallEvent{{TS: start}}, math.Inf(1)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := MedianIntervalSec(tt.calls)
			if tt.name == "<2 calls" {
				if !math.IsInf(got, 1) {
					t.Fatalf("MedianIntervalSec=%.3f, want +Inf", got)
				}
				return
			}
			if math.Abs(got-tt.want) > 0.001 {
				t.Fatalf("MedianIntervalSec=%.3f, want %.3f", got, tt.want)
			}
		})
	}
}
