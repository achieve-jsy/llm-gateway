package report_test

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

// validPersonalReport 构造一份可通过 Validate() 的最小合法个人报告，供各测试在此基础上改字段。
func validPersonalReport() report.Report {
	return report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level: report.LevelPersonal,
			Scope: report.Scope{ID: "user-1", Name: "张三"},
			Period: report.Period{
				From:    "2026-08-14",
				To:      "2026-08-21",
				IsoWeek: "2026-W33",
			},
			Currency:    report.Currency{Report: "CNY", USDCNYRate: 7.2},
			RuleVersion: "v0.6",
		},
		DataQuality: report.DataQuality{
			RealSessionCoverage:   0.63,
			SceneLabelCoverage:    0.41,
			PriceCatalogCoverage:  0.92,
			PositionLabelCoverage: 0.88,
			FailedCostCoverage:    1.0,
		},
		CostBreakdown: report.CostBreakdown{
			UncachedInputCostUSD: 100,
			CacheCostUSD:         200,
			OutputCostUSD:        50,
			FailedRequestCostUSD: 10,
		},
		SafetyAnomaly: report.SafetyAnomaly{Hit: false},
		UsageIssues: []report.UsageIssue{
			{
				Type:              report.IssueTypeMultiTaskSession,
				Verdict:           report.VerdictSuspected,
				RequestCount:      10,
				SessionCount:      2,
				RequestShare:      0.1,
				SessionShare:      0.2,
				AttributedCostUSD: 60,
				OverlapCostUSD:    12,
				AffectedCostUSD:   210,
				Confidence:        0.6,
			},
		},
		Conclusion: report.Conclusion{
			CostMainBucket: report.CostBucketCache,
			IssueCostRatio: 0.171,
			DominantIssue:  report.IssueTypeMultiTaskSession,
			Note:           "high_cost_by_workload",
		},
	}
}

// validMemberPoint 构造一个通过校验的最小成员点，costUSD 与 attributed 需自洽。
func validMemberPoint(userID string, costUSD, attributedUSD float64) report.MemberPoint {
	return report.MemberPoint{
		UserID:                 userID,
		Name:                   "成员-" + userID,
		Position:               "WEB前端开发",
		CostUSD:                costUSD,
		CostMultiple:           1.0,
		IssueAttributedCostUSD: attributedUSD,
		IssueCostRatio:         attributedUSD / costUSD,
		Quadrant:               report.QuadrantRoutine,
		DominantIssue:          report.IssueTypeNormal,
		Calls:                  100,
		UsageIssues: []report.UsageIssue{
			{
				Type:              report.IssueTypeToothpaste,
				Verdict:           report.VerdictSuspected,
				AttributedCostUSD: attributedUSD,
				AffectedCostUSD:   attributedUSD,
				Confidence:        0.5,
			},
		},
	}
}

func validDepartmentReport() report.Report {
	r := validPersonalReport()
	r.Meta.Level = report.LevelDepartment
	r.Meta.Scope = report.Scope{ID: "dept-1", Name: "X-Lab", StudioID: "studio-1", StudioName: "工作室A"}
	r.UsageIssues = nil
	r.MemberEvaluation = &report.MemberEvaluation{
		CostMultipleBaseline: "median",
		CostMedianUSD:        100,
		IssueRatioMedian:     0.09,
		Members: []report.MemberPoint{
			validMemberPoint("user-1", 100, 20),
			validMemberPoint("user-2", 200, 30),
		},
		Clusters: []report.MemberCluster{
			{Label: report.IssueTypeToothpaste, MemberIDs: []string{"user-1", "user-2"}},
		},
	}
	return r
}

func TestValidate_ValidPersonalReport(t *testing.T) {
	r := validPersonalReport()
	if err := r.Validate(); err != nil {
		t.Fatalf("expected valid personal report, got error: %v", err)
	}
}

func TestValidate_ValidDepartmentReport(t *testing.T) {
	r := validDepartmentReport()
	if err := r.Validate(); err != nil {
		t.Fatalf("expected valid department report, got error: %v", err)
	}
}

func TestValidate_ValidStudioReport(t *testing.T) {
	r := validDepartmentReport()
	r.Meta.Level = report.LevelStudio
	r.Meta.Scope = report.Scope{ID: "studio-1", Name: "工作室A"}
	if err := r.Validate(); err != nil {
		t.Fatalf("expected valid studio report, got error: %v", err)
	}
}

func TestValidate_SchemaVersionMismatch(t *testing.T) {
	r := validPersonalReport()
	r.SchemaVersion = "1.0"
	assertErrContains(t, r.Validate(), "schema_version")
}

func TestValidate_InvalidLevel(t *testing.T) {
	r := validPersonalReport()
	r.Meta.Level = "team"
	assertErrContains(t, r.Validate(), "invalid level")
}

func TestValidate_EmptyScopeID(t *testing.T) {
	r := validPersonalReport()
	r.Meta.Scope.ID = ""
	assertErrContains(t, r.Validate(), "scope.id")
}

func TestValidate_PeriodInvalidFrom(t *testing.T) {
	r := validPersonalReport()
	r.Meta.Period.From = "not-a-date"
	assertErrContains(t, r.Validate(), "period.from")
}

func TestValidate_PeriodInvalidTo(t *testing.T) {
	r := validPersonalReport()
	r.Meta.Period.To = "not-a-date"
	assertErrContains(t, r.Validate(), "period.to")
}

func TestValidate_PeriodToBeforeFrom(t *testing.T) {
	r := validPersonalReport()
	r.Meta.Period.From = "2026-08-21"
	r.Meta.Period.To = "2026-08-14"
	assertErrContains(t, r.Validate(), "precedes")
}

func TestValidate_MissingCurrencyReport(t *testing.T) {
	r := validPersonalReport()
	r.Meta.Currency.Report = ""
	assertErrContains(t, r.Validate(), "currency.report")
}

func TestValidate_NonPositiveRate(t *testing.T) {
	r := validPersonalReport()
	r.Meta.Currency.USDCNYRate = 0
	assertErrContains(t, r.Validate(), "usd_cny_rate")
}

func TestValidate_MissingRuleVersion(t *testing.T) {
	r := validPersonalReport()
	r.Meta.RuleVersion = ""
	assertErrContains(t, r.Validate(), "rule_version")
}

func TestValidate_DataQualityOutOfRange(t *testing.T) {
	r := validPersonalReport()
	r.DataQuality.RealSessionCoverage = 1.5
	assertErrContains(t, r.Validate(), "real_session_coverage")
}

func TestValidate_DataQualityNegative(t *testing.T) {
	r := validPersonalReport()
	r.DataQuality.SceneLabelCoverage = -0.1
	assertErrContains(t, r.Validate(), "scene_label_coverage")
}

func TestValidate_NegativeCostBreakdownFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*report.CostBreakdown)
		wantMsg string
	}{
		{"uncached_input", func(c *report.CostBreakdown) { c.UncachedInputCostUSD = -1 }, "uncached_input_cost_usd"},
		{"cache", func(c *report.CostBreakdown) { c.CacheCostUSD = -1 }, "cache_cost_usd"},
		{"output", func(c *report.CostBreakdown) { c.OutputCostUSD = -1 }, "output_cost_usd"},
		{"failed", func(c *report.CostBreakdown) { c.FailedRequestCostUSD = -1 }, "failed_request_cost_usd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validPersonalReport()
			tt.mutate(&r.CostBreakdown)
			assertErrContains(t, r.Validate(), tt.wantMsg)
		})
	}
}

func TestValidate_FailedCostExceedsTotal(t *testing.T) {
	r := validPersonalReport()
	r.CostBreakdown.FailedRequestCostUSD = r.CostBreakdown.Total() + 100
	assertErrContains(t, r.Validate(), "failed_request_cost_usd")
}

func TestValidate_ByModelTierEmptyModel(t *testing.T) {
	r := validPersonalReport()
	r.CostBreakdown.ByModelTier = []report.ModelTierCost{{Model: "", CostUSD: 1}}
	assertErrContains(t, r.Validate(), "by_model_tier[0].model")
}

func TestValidate_ByModelTierNegativeCost(t *testing.T) {
	r := validPersonalReport()
	r.CostBreakdown.ByModelTier = []report.ModelTierCost{{Model: "gpt-x", CostUSD: -1}}
	assertErrContains(t, r.Validate(), "by_model_tier[0]")
}

func TestValidate_SafetyAnomalyRatioOutOfRange(t *testing.T) {
	r := validPersonalReport()
	r.SafetyAnomaly = report.SafetyAnomaly{Hit: true, UnrelatedCostRatio: 1.5, Status: report.SafetyAnomalyPendingReview}
	assertErrContains(t, r.Validate(), "unrelated_cost_ratio")
}

func TestValidate_SafetyAnomalyNegativeCost(t *testing.T) {
	r := validPersonalReport()
	r.SafetyAnomaly = report.SafetyAnomaly{Hit: true, UnrelatedCostUSD: -1, Status: report.SafetyAnomalyPendingReview}
	assertErrContains(t, r.Validate(), "unrelated_cost_usd")
}

func TestValidate_SafetyAnomalyInvalidStatus(t *testing.T) {
	r := validPersonalReport()
	r.SafetyAnomaly = report.SafetyAnomaly{Hit: true, Status: "confirmed_bad"}
	assertErrContains(t, r.Validate(), "invalid status")
}

func TestValidate_SafetyAnomalyHitRequiresStatus(t *testing.T) {
	r := validPersonalReport()
	r.SafetyAnomaly = report.SafetyAnomaly{Hit: true, Status: ""}
	assertErrContains(t, r.Validate(), "status must not be empty")
}

func TestValidate_SafetyAnomalyHitWithAnyValidStatusPasses(t *testing.T) {
	// 红线校验层面允许任何合法状态（真正"初始必须 pending_review"由装配器保证），
	// 这里验证 Validate() 本身不会因为负责人已回填 confirmed_ok 就报错。
	for _, status := range []string{
		report.SafetyAnomalyPendingReview,
		report.SafetyAnomalyConfirmedOK,
		report.SafetyAnomalyNeedCheck,
		report.SafetyAnomalyFalsePositive,
	} {
		r := validPersonalReport()
		r.SafetyAnomaly = report.SafetyAnomaly{Hit: true, Status: status}
		if err := r.Validate(); err != nil {
			t.Fatalf("status=%q: expected valid, got error: %v", status, err)
		}
	}
}

func TestValidate_UsageIssueInvalidType(t *testing.T) {
	r := validPersonalReport()
	r.UsageIssues[0].Type = "unknown_issue"
	assertErrContains(t, r.Validate(), "invalid type")
}

func TestValidate_UsageIssueInvalidVerdict(t *testing.T) {
	r := validPersonalReport()
	r.UsageIssues[0].Verdict = "maybe"
	assertErrContains(t, r.Validate(), "invalid verdict")
}

func TestValidate_UsageIssueNegativeCounts(t *testing.T) {
	r := validPersonalReport()
	r.UsageIssues[0].RequestCount = -1
	assertErrContains(t, r.Validate(), "request_count")
}

func TestValidate_UsageIssueShareOutOfRange(t *testing.T) {
	r := validPersonalReport()
	r.UsageIssues[0].RequestShare = 1.2
	assertErrContains(t, r.Validate(), "request_share")
}

func TestValidate_UsageIssueConfidenceOutOfRange(t *testing.T) {
	r := validPersonalReport()
	r.UsageIssues[0].Confidence = 1.2
	assertErrContains(t, r.Validate(), "confidence")
}

func TestValidate_UsageIssueAttributedExceedsAffected(t *testing.T) {
	r := validPersonalReport()
	r.UsageIssues[0].AttributedCostUSD = 300
	r.UsageIssues[0].AffectedCostUSD = 100
	assertErrContains(t, r.Validate(), "exceeds affected_cost_usd")
}

// TestValidate_AttributedCostExceedsTotal 是本任务的核心校验：
// Σ UsageIssues[].AttributedCostUSD 不能超过 CostBreakdown 总成本（§3.6/§5 费用桶归因）。
func TestValidate_AttributedCostExceedsTotal(t *testing.T) {
	r := validPersonalReport()
	total := r.CostBreakdown.Total() // 100+200+50=350
	// 让 Σ attributed 明显超过总成本。
	r.UsageIssues = []report.UsageIssue{
		{
			Type:              report.IssueTypePrefixPollution,
			Verdict:           report.VerdictConfirmed,
			AttributedCostUSD: total + 1,
			AffectedCostUSD:   total + 1,
			Confidence:        0.9,
		},
	}
	assertErrContains(t, r.Validate(), "cost bucket attribution must not double count")
}

func TestValidate_AttributedCostSumAcrossMultipleIssuesExceedsTotal(t *testing.T) {
	r := validPersonalReport()
	total := r.CostBreakdown.Total() // 350
	half := total/2 + 1              // 两条相加刚好超过 total
	r.UsageIssues = []report.UsageIssue{
		{Type: report.IssueTypePrefixPollution, Verdict: report.VerdictConfirmed, AttributedCostUSD: half, AffectedCostUSD: half, Confidence: 0.9},
		{Type: report.IssueTypeOutputRunaway, Verdict: report.VerdictConfirmed, AttributedCostUSD: half, AffectedCostUSD: half, Confidence: 0.9},
	}
	assertErrContains(t, r.Validate(), "cost bucket attribution must not double count")
}

func TestValidate_AttributedCostEqualsTotalPasses(t *testing.T) {
	// 等于总成本（边界情况）应当通过，不应误报。
	r := validPersonalReport()
	total := r.CostBreakdown.Total()
	r.UsageIssues = []report.UsageIssue{
		{Type: report.IssueTypePrefixPollution, Verdict: report.VerdictConfirmed, AttributedCostUSD: total, AffectedCostUSD: total, Confidence: 0.9},
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("attributed==total should pass, got error: %v", err)
	}
}

func TestValidate_PersonalLevelMustNotSetMemberEvaluation(t *testing.T) {
	r := validPersonalReport()
	r.MemberEvaluation = &report.MemberEvaluation{
		Members: []report.MemberPoint{validMemberPoint("u1", 10, 0)},
	}
	assertErrContains(t, r.Validate(), "must not set member_evaluation")
}

func TestValidate_DepartmentLevelMustNotSetTopLevelUsageIssues(t *testing.T) {
	r := validDepartmentReport()
	r.UsageIssues = []report.UsageIssue{
		{Type: report.IssueTypeToothpaste, Verdict: report.VerdictSuspected},
	}
	assertErrContains(t, r.Validate(), "must not set top-level usage_issues")
}

func TestValidate_DepartmentLevelRequiresMemberEvaluation(t *testing.T) {
	r := validDepartmentReport()
	r.MemberEvaluation = nil
	assertErrContains(t, r.Validate(), "requires member_evaluation")
}

func TestValidate_MemberEvaluationIssueRatioMedianOutOfRange(t *testing.T) {
	r := validDepartmentReport()
	r.MemberEvaluation.IssueRatioMedian = 1.5
	assertErrContains(t, r.Validate(), "issue_ratio_median")
}

func TestValidate_MemberEvaluationNegativeCostMedian(t *testing.T) {
	r := validDepartmentReport()
	r.MemberEvaluation.CostMedianUSD = -1
	assertErrContains(t, r.Validate(), "cost_median_usd")
}

func TestValidate_MemberEmptyUserID(t *testing.T) {
	r := validDepartmentReport()
	r.MemberEvaluation.Members[0].UserID = ""
	assertErrContains(t, r.Validate(), "user_id must not be empty")
}

func TestValidate_MemberDuplicateUserID(t *testing.T) {
	r := validDepartmentReport()
	r.MemberEvaluation.Members[1].UserID = r.MemberEvaluation.Members[0].UserID
	assertErrContains(t, r.Validate(), "duplicate user_id")
}

func TestValidate_MemberNegativeCost(t *testing.T) {
	r := validDepartmentReport()
	r.MemberEvaluation.Members[0].CostUSD = -1
	assertErrContains(t, r.Validate(), "members[0]: cost_usd")
}

func TestValidate_MemberInvalidQuadrant(t *testing.T) {
	r := validDepartmentReport()
	r.MemberEvaluation.Members[0].Quadrant = "top_right"
	assertErrContains(t, r.Validate(), "invalid quadrant")
}

func TestValidate_MemberInvalidDominantIssue(t *testing.T) {
	r := validDepartmentReport()
	r.MemberEvaluation.Members[0].DominantIssue = "unknown"
	assertErrContains(t, r.Validate(), "invalid dominant_issue")
}

// TestValidate_MemberAttributedCostExceedsMemberCost 校验每个成员各自的费用桶归因约束：
// Σ member.UsageIssues[].AttributedCostUSD <= member.CostUSD。
func TestValidate_MemberAttributedCostExceedsMemberCost(t *testing.T) {
	r := validDepartmentReport()
	member := &r.MemberEvaluation.Members[0]
	member.UsageIssues = []report.UsageIssue{
		{
			Type:              report.IssueTypeToothpaste,
			Verdict:           report.VerdictConfirmed,
			AttributedCostUSD: member.CostUSD + 1,
			AffectedCostUSD:   member.CostUSD + 1,
			Confidence:        0.8,
		},
	}
	assertErrContains(t, r.Validate(), "members[0].usage_issues")
}

func TestValidate_ClusterEmptyLabel(t *testing.T) {
	r := validDepartmentReport()
	r.MemberEvaluation.Clusters = []report.MemberCluster{{Label: "", MemberIDs: []string{"user-1"}}}
	assertErrContains(t, r.Validate(), "clusters[0]: label")
}

func TestValidate_ClusterUnknownMemberID(t *testing.T) {
	r := validDepartmentReport()
	r.MemberEvaluation.Clusters = []report.MemberCluster{{Label: "x", MemberIDs: []string{"user-not-exist"}}}
	assertErrContains(t, r.Validate(), "not found in members")
}

func TestValidate_ConclusionInvalidCostMainBucket(t *testing.T) {
	r := validPersonalReport()
	r.Conclusion.CostMainBucket = "principal"
	assertErrContains(t, r.Validate(), "invalid cost_main_bucket")
}

func TestValidate_ConclusionIssueCostRatioOutOfRange(t *testing.T) {
	r := validPersonalReport()
	r.Conclusion.IssueCostRatio = 1.5
	assertErrContains(t, r.Validate(), "issue_cost_ratio")
}

func TestValidate_ConclusionInvalidDominantIssue(t *testing.T) {
	r := validPersonalReport()
	r.Conclusion.DominantIssue = "unknown"
	assertErrContains(t, r.Validate(), "invalid dominant_issue")
}

func TestValidate_ConclusionAllowsNormalDominantIssue(t *testing.T) {
	r := validPersonalReport()
	r.Conclusion.DominantIssue = report.IssueTypeNormal
	r.UsageIssues = nil
	if err := r.Validate(); err != nil {
		t.Fatalf("dominant_issue=normal should be valid, got error: %v", err)
	}
}

func TestCostBreakdown_Total(t *testing.T) {
	c := report.CostBreakdown{
		UncachedInputCostUSD: 10,
		CacheCostUSD:         20,
		OutputCostUSD:        5,
		FailedRequestCostUSD: 2, // 已含于上面三块，不应叠加
	}
	if got, want := c.Total(), 35.0; got != want {
		t.Fatalf("Total() = %v, want %v", got, want)
	}
}

func assertErrContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected error containing %q, got: %v", substr, err)
	}
}
