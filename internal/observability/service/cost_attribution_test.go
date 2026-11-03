package service

import (
	"math"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

// 对账等式紧致检查。
func reconApprox(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func assertReconciliation(t *testing.T, res CostAttributionResult) {
	t.Helper()
	if !reconApprox(res.TotalCost, res.AttributedTotal+res.OverlapTotal+res.Residual) {
		t.Fatalf("reconciliation broken: total=%v ≠ attributed=%v+overlap=%v+residual=%v",
			res.TotalCost, res.AttributedTotal, res.OverlapTotal, res.Residual)
	}
	// 每情况影响面 >= 归因净额。
	for _, p := range res.PerIssue {
		if p.AffectedCostUSD+1e-9 < p.AttributedCostUSD {
			t.Errorf("issue %s: affected=%v < attributed=%v", p.IssueType, p.AffectedCostUSD, p.AttributedCostUSD)
		}
	}
}

// 核心场景：同一请求被 2+ 规则命中，主因择一。
func TestReconcileCostAttribution_SameRequestTwoRules(t *testing.T) {
	total := 2.0
	claims := []CostClaim{
		{IssueType: report.IssueTypeOutputRunaway, EntityKind: EntityKindRequest, EntityKey: "R", BucketCost: 0.50, AffectedCost: 1.0},
		{IssueType: report.IssueTypeOverModel, EntityKind: EntityKindRequest, EntityKey: "R", BucketCost: 0.30, AffectedCost: 1.0},
	}
	res, err := ReconcileCostAttribution(claims, total)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 主因 = output_runaway（0.50 > 0.30）。
	if !reconApprox(res.AttributedTotal, 0.50) {
		t.Errorf("attributed_total = %v, want 0.50", res.AttributedTotal)
	}
	if !reconApprox(res.OverlapTotal, 0.30) {
		t.Errorf("overlap_total = %v, want 0.30", res.OverlapTotal)
	}
	if !reconApprox(res.Residual, 1.20) {
		t.Errorf("residual = %v, want 1.20", res.Residual)
	}
	assertReconciliation(t, res)

	// 按情况核对。
	get := func(it string) IssueAttribution {
		for _, p := range res.PerIssue {
			if p.IssueType == it {
				return p
			}
		}
		t.Fatalf("missing issue %s", it)
		return IssueAttribution{}
	}
	or := get(report.IssueTypeOutputRunaway)
	if !reconApprox(or.AttributedCostUSD, 0.50) || !reconApprox(or.OverlapCostUSD, 0) || !reconApprox(or.AffectedCostUSD, 1.0) {
		t.Errorf("output_runaway: attr=%v over=%v aff=%v, want 0.50/0/1.0", or.AttributedCostUSD, or.OverlapCostUSD, or.AffectedCostUSD)
	}
	om := get(report.IssueTypeOverModel)
	if !reconApprox(om.AttributedCostUSD, 0) || !reconApprox(om.OverlapCostUSD, 0.30) || !reconApprox(om.AffectedCostUSD, 1.0) {
		t.Errorf("over_model: attr=%v over=%v aff=%v, want 0/0.30/1.0", om.AttributedCostUSD, om.OverlapCostUSD, om.AffectedCostUSD)
	}
}

// 会话级去重：同一会话被 2 个交互规则命中。
func TestReconcileCostAttribution_SameSessionTwoRules(t *testing.T) {
	total := 3.0
	claims := []CostClaim{
		{IssueType: report.IssueTypeToothpaste, EntityKind: EntityKindSession, EntityKey: "S", BucketCost: 0.40, AffectedCost: 2.0},
		{IssueType: report.IssueTypeMultiTaskSession, EntityKind: EntityKindSession, EntityKey: "S", BucketCost: 0.60, AffectedCost: 2.0},
	}
	res, err := ReconcileCostAttribution(claims, total)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reconApprox(res.AttributedTotal, 0.60) {
		t.Errorf("attributed_total = %v, want 0.60", res.AttributedTotal)
	}
	if !reconApprox(res.OverlapTotal, 0.40) {
		t.Errorf("overlap_total = %v, want 0.40", res.OverlapTotal)
	}
	if !reconApprox(res.Residual, 2.00) {
		t.Errorf("residual = %v, want 2.00", res.Residual)
	}
	assertReconciliation(t, res)
}

// 无重叠：不同载体各自为主因，无 overlap。
func TestReconcileCostAttribution_NoOverlap(t *testing.T) {
	total := 2.0
	claims := []CostClaim{
		{IssueType: report.IssueTypeOutputRunaway, EntityKind: EntityKindRequest, EntityKey: "R1", BucketCost: 0.50, AffectedCost: 1.0},
		{IssueType: report.IssueTypeOverModel, EntityKind: EntityKindRequest, EntityKey: "R2", BucketCost: 0.20, AffectedCost: 0.5},
	}
	res, err := ReconcileCostAttribution(claims, total)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reconApprox(res.AttributedTotal, 0.70) || !reconApprox(res.OverlapTotal, 0) || !reconApprox(res.Residual, 1.30) {
		t.Errorf("no-overlap: attributed=%v overlap=%v residual=%v, want 0.70/0/1.30", res.AttributedTotal, res.OverlapTotal, res.Residual)
	}
	assertReconciliation(t, res)
}

// 金额相同 → 按 IssueType 字典序确定主因（确定性）。
func TestReconcileCostAttribution_TieBreak(t *testing.T) {
	total := 1.0
	claims := []CostClaim{
		{IssueType: report.IssueTypeOverModel, EntityKind: EntityKindRequest, EntityKey: "R", BucketCost: 0.50, AffectedCost: 1.0},
		{IssueType: report.IssueTypeOutputRunaway, EntityKind: EntityKindRequest, EntityKey: "R", BucketCost: 0.50, AffectedCost: 1.0},
	}
	res, err := ReconcileCostAttribution(claims, total)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "output_runaway" < "over_model" 字典序 → output_runaway 主因。
	var or, om IssueAttribution
	for _, p := range res.PerIssue {
		if p.IssueType == report.IssueTypeOutputRunaway {
			or = p
		}
		if p.IssueType == report.IssueTypeOverModel {
			om = p
		}
	}
	if !reconApprox(or.AttributedCostUSD, 0.50) || !reconApprox(om.OverlapCostUSD, 0.50) {
		t.Errorf("tie-break: output_runaway attr=%v (want 0.50), over_model overlap=%v (want 0.50)", or.AttributedCostUSD, om.OverlapCostUSD)
	}
}

// 违规：Σ归因 > 总成本 → 返回 error（对齐 report.Validate()）。
func TestReconcileCostAttribution_OverAttributionError(t *testing.T) {
	claims := []CostClaim{
		{IssueType: report.IssueTypeOutputRunaway, EntityKind: EntityKindRequest, EntityKey: "R", BucketCost: 5.0, AffectedCost: 5.0},
	}
	if _, err := ReconcileCostAttribution(claims, 3.0); err == nil {
		t.Fatal("expected over-attribution error, got nil")
	}
}

// 违规：bucket > affected → 返回 error。
func TestReconcileCostAttribution_BucketExceedsAffectedError(t *testing.T) {
	claims := []CostClaim{
		{IssueType: report.IssueTypeOutputRunaway, EntityKind: EntityKindRequest, EntityKey: "R", BucketCost: 2.0, AffectedCost: 1.0},
	}
	if _, err := ReconcileCostAttribution(claims, 3.0); err == nil {
		t.Fatal("expected bucket>affected error, got nil")
	}
}

// 空输入 → 全部零，残差 = 总成本。
func TestReconcileCostAttribution_Empty(t *testing.T) {
	res, err := ReconcileCostAttribution(nil, 5.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reconApprox(res.AttributedTotal, 0) || !reconApprox(res.OverlapTotal, 0) || !reconApprox(res.Residual, 5.0) {
		t.Errorf("empty: attributed=%v overlap=%v residual=%v, want 0/0/5.0", res.AttributedTotal, res.OverlapTotal, res.Residual)
	}
	assertReconciliation(t, res)
}
