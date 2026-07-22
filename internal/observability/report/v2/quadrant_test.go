package report_test

import (
	"math"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// 四象限：跨越十字线的四组 + 边界（x==1 / y==yMedian）。
func TestQuadrantOf_Boundaries(t *testing.T) {
	params := report.DefaultQuadrantParams()
	x := params.XMedian // 1.0
	y := 0.4            // 组织问题占比中位
	cases := []struct {
		name                string
		costMultiple, ratio float64
		want                string
	}{
		{"右上 priority_improve: 成本高+问题占比高", 2.0, 0.6, report.QuadrantPriorityImprove},
		{"左上 coach: 成本不高+问题占比高", 0.5, 0.6, report.QuadrantCoach},
		{"右下 high_cost_normal: 成本高+问题占比低", 2.0, 0.2, report.QuadrantHighCostNormal},
		{"左下 routine: 成本不高+问题占比低", 0.5, 0.2, report.QuadrantRoutine},
		{"边界 x==1,y==yMedian → routine(左/下)", x, y, report.QuadrantRoutine},
		{"边界 x==1,y>yMedian → coach(左/上)", x, 0.6, report.QuadrantCoach},
		{"边界 x>1,y==yMedian → high_cost_normal(右/下)", 2.0, y, report.QuadrantHighCostNormal},
	}
	for _, c := range cases {
		got := report.QuadrantOf(c.costMultiple, c.ratio, y, x)
		if got != c.want {
			t.Errorf("%s: QuadrantOf(%v,%v) = %q, want %q", c.name, c.costMultiple, c.ratio, got, c.want)
		}
	}
}

func TestCostMultiple(t *testing.T) {
	if got := report.CostMultiple(100, 50); !near(got, 2.0) {
		t.Errorf("CostMultiple(100,50)=%v, want 2", got)
	}
	// median<=0 → 0（不可比，防除零）。
	if got := report.CostMultiple(100, 0); !near(got, 0) {
		t.Errorf("CostMultiple(100,0)=%v, want 0", got)
	}
}

func TestIssueCostRatio(t *testing.T) {
	if got := report.IssueCostRatio(0.5, 1.0); !near(got, 0.5) {
		t.Errorf("IssueCostRatio(0.5,1.0)=%v, want 0.5", got)
	}
	if got := report.IssueCostRatio(2, 1.0); !near(got, 1.0) {
		t.Errorf("IssueCostRatio(2,1)=%v, want clip 1", got)
	}
	if got := report.IssueCostRatio(1, 0); !near(got, 0) {
		t.Errorf("IssueCostRatio(1,0)=%v, want 0", got)
	}
	if got := report.IssueCostRatio(-0.5, 1.0); !near(got, 0) {
		t.Errorf("IssueCostRatio(-0.5,1)=%v, want 0", got)
	}
}

func TestDominantIssue(t *testing.T) {
	// 最大归因种。
	got, cost := report.DominantIssue(map[string]float64{
		report.IssueTypePrefixPollution: 0.5,
		report.IssueTypeOutputRunaway:   0.3,
	}, 0)
	if got != report.IssueTypePrefixPollution || !near(cost, 0.5) {
		t.Errorf("DominantIssue = %q/%v, want %q/0.5", got, cost, report.IssueTypePrefixPollution)
	}
	// 平局按 IssueTypeOrder 顺序取前者（确定性）。
	got, _ = report.DominantIssue(map[string]float64{
		report.IssueTypePrefixPollution: 0.5,
		report.IssueTypeOutputRunaway:   0.5,
	}, 0)
	if got != report.IssueTypePrefixPollution {
		t.Errorf("tie DominantIssue = %q, want %q", got, report.IssueTypePrefixPollution)
	}
	// 全低于阈值 → normal。
	got, cost = report.DominantIssue(map[string]float64{
		report.IssueTypeToothpaste: 0.01,
	}, 1.0)
	if got != report.IssueTypeNormal || !near(cost, 0) {
		t.Errorf("below-threshold DominantIssue = %q/%v, want normal/0", got, cost)
	}
	// 空 → normal。
	got, _ = report.DominantIssue(nil, 0)
	if got != report.IssueTypeNormal {
		t.Errorf("empty DominantIssue = %q, want normal", got)
	}
}

// 端到端：EvaluateMemberQuadrants 计算中位十字线 + 每人象限。
func TestEvaluateMemberQuadrants(t *testing.T) {
	inputs := []report.MemberQuadrantInput{
		{UserID: "m1", Name: "甲", CostUSD: 100, Calls: 10, IssueAttributed: map[string]float64{report.IssueTypePrefixPollution: 50}},
		{UserID: "m2", Name: "乙", CostUSD: 200, Calls: 20, IssueAttributed: map[string]float64{report.IssueTypeOutputRunaway: 80}},
		{UserID: "m3", Name: "丙", CostUSD: 300, Calls: 30, IssueAttributed: map[string]float64{report.IssueTypeToothpaste: 30}},
	}
	points, eval := report.EvaluateMemberQuadrants(inputs, report.DefaultQuadrantParams())

	// 中位：cost [100,200,300] → 200；ratio [0.5,0.4,0.1] → 0.4。
	if !near(eval.CostMedianUSD, 200) {
		t.Errorf("CostMedianUSD=%v, want 200", eval.CostMedianUSD)
	}
	if !near(eval.IssueRatioMedian, 0.4) {
		t.Errorf("IssueRatioMedian=%v, want 0.4", eval.IssueRatioMedian)
	}
	if len(points) != 3 {
		t.Fatalf("points=%d, want 3", len(points))
	}
	byID := map[string]report.MemberPoint{}
	for _, p := range points {
		byID[p.UserID] = p
	}
	if p := byID["m1"]; p.Quadrant != report.QuadrantCoach || !near(p.CostMultiple, 0.5) || !near(p.IssueCostRatio, 0.5) || p.DominantIssue != report.IssueTypePrefixPollution {
		t.Errorf("m1 quadrant/cost/ratio/dominant = %q/%v/%v/%q", p.Quadrant, p.CostMultiple, p.IssueCostRatio, p.DominantIssue)
	}
	if p := byID["m2"]; p.Quadrant != report.QuadrantRoutine || !near(p.CostMultiple, 1.0) || !near(p.IssueCostRatio, 0.4) {
		t.Errorf("m2 quadrant/cost/ratio = %q/%v/%v", p.Quadrant, p.CostMultiple, p.IssueCostRatio)
	}
	if p := byID["m3"]; p.Quadrant != report.QuadrantHighCostNormal || !near(p.CostMultiple, 1.5) || !near(p.IssueCostRatio, 0.1) {
		t.Errorf("m3 quadrant/cost/ratio = %q/%v/%v", p.Quadrant, p.CostMultiple, p.IssueCostRatio)
	}
}
