package derive_test

import (
	"math"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report/derive"
)

// S4 回归：MAD==0 且 current != median 时，RobustZ 不得返回 ±Inf（会污染 JSON marshal）。
// 修复前返回 +Inf/-Inf；修复后返回有界 6σ 符号值。
func TestRobustZNoInfWhenMADZero(t *testing.T) {
	cases := []struct {
		name   string
		value  float64
		median float64
		mad    float64
	}{
		{"mad0_current_gt_median", 100, 50, 0},
		{"mad0_current_lt_median", 30, 50, 0},
		{"mad0_current_eq_median", 50, 50, 0},
		{"mad_nonzero_sane", 100, 50, 10},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := derive.RobustZ(c.value, c.median, c.mad)
			if math.IsInf(got, 0) || math.IsNaN(got) {
				t.Fatalf("RobustZ(%v,%v,%v) = %v, must be finite", c.value, c.median, c.mad, got)
			}
			if c.mad == 0 && c.value != c.median && math.Abs(got) < 6 {
				t.Fatalf("RobustZ deviate case got %v, want |z|>=6 to mark significant deviation", got)
			}
		})
	}
}

// S4 延伸：全等基线（MAD==0）+ 当前偏离时，Baseline.DevSigma 有限且能标记偏离。
func TestBaselineDevSigmaFiniteOnFlatHistory(t *testing.T) {
	vals := []float64{50, 50, 50, 50, 50} // 全等，MAD=0
	res := derive.Baseline(vals, 100, 28, 3, "2026-08")
	if math.IsInf(res.DevSigma, 0) || math.IsNaN(res.DevSigma) {
		t.Fatalf("DevSigma on flat history = %v, must be finite", res.DevSigma)
	}
	if res.DevSigma == 0 {
		t.Fatalf("current!=median on flat history should have nonzero DevSigma, got 0")
	}
}
