package derive

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
)

// TestWeightedWindowsMatchesExpand 验证加权滑窗与「分钟桶展开成逐调用 → 逐调用滑窗」结果一致。
// 桶聚合按分钟叠加 count/cost；展开则每桶产生 count 个调用（成本均摊），两者滑窗 count/cost 应等价。
func TestWeightedWindowsMatchesExpand(t *testing.T) {
	const width = 10
	base := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	// 构造稀疏的分钟桶：第 0 分钟 3 次调用（成本 3.0），第 7 分钟 2 次调用（成本 0.4），第 20 分钟 1 次（成本 0.1）。
	buckets := []report.MinuteBucket{
		{Minute: base, Count: 3, Cost: 3.0},
		{Minute: base.Add(7 * time.Minute), Count: 2, Cost: 0.4},
		{Minute: base.Add(20 * time.Minute), Count: 1, Cost: 0.1},
	}
	// 逐调用路径：按相同桶数据展开成 count 个调用（成本均摊）。
	var calls []report.CallEvent
	for _, bk := range buckets {
		per := bk.Cost / float64(bk.Count)
		for i := 0; i < bk.Count; i++ {
			calls = append(calls, report.CallEvent{TS: bk.Minute, EstCostUSD: per})
		}
	}

	got := weightedWindows(buckets, width)
	want := windows(bucket(calls), width)

	// 只在有桶分布的窗口段对齐比对（got 自 minB..maxB，want 亦然）。
	if len(got) != len(want) {
		t.Fatalf("窗口数量不一致: got=%d want=%d", len(got), len(want))
	}
	for i := range got {
		if got[i].start != want[i].start {
			t.Fatalf("[%d] start: got=%d want=%d", i, got[i].start, want[i].start)
		}
		if got[i].count != want[i].count {
			t.Fatalf("[%d] count: got=%d want=%d", i, got[i].count, want[i].count)
		}
		if diff := got[i].cost - want[i].cost; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("[%d] cost: got=%f want=%f", i, got[i].cost, want[i].cost)
		}
	}
	// 第 0 分钟窗应覆盖 [0,10)，包含 0 和 7 分钟两桶：count=3+2=5, cost=3.0+0.4=3.4。
	if got[0].count != 5 || !floatEq(got[0].cost, 3.4) {
		t.Fatalf("窗口0: count=%d cost=%f (want 5, 3.4)", got[0].count, got[0].cost)
	}
	// 窗口1 从 minute 1 起，到 [1,11)：min0 桶滑出，只含 7 分钟桶（2 次/0.4）。
	if got[1].count != 2 || !floatEq(got[1].cost, 0.4) {
		t.Fatalf("窗口1: count=%d cost=%f (want 2, 0.4)", got[1].count, got[1].cost)
	}
}

func TestWeightedWindowsEmpty(t *testing.T) {
	if got := weightedWindows(nil, 10); got != nil {
		t.Fatalf("empty buckets should return nil, got %v", got)
	}
}

// floatEq 以 1e-9 容差比较浮点，避免滑窗减加累积的浮点误差导致精确 != 失败。
func floatEq(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}
