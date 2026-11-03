package report_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

var goldenUpdate = flag.Bool("update", false, "rewrite golden snapshots")

// 三份报告黄金快照逐字锁定。
func TestRenderMD_Golden(t *testing.T) {
	cases := map[string]*report.Report{
		"personal":   samplePersonal(),
		"department": sampleDepartment(),
		"studio":     sampleStudio(),
	}
	for name, r := range cases {
		got := report.RenderMD(r)
		path := filepath.Join("testdata", name+".md")
		if *goldenUpdate {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatalf("write golden %s: %v", name, err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read golden %s: %v (run with -update to create)", name, err)
		}
		if got != string(want) {
			t.Errorf("golden mismatch [%s]\n--- got ---\n%s--- want ---\n%s", name, got, string(want))
		}
	}
}

func TestRenderMD_V06TokenAndClassification(t *testing.T) {
	r := sampleDepartment()
	r.CostBreakdown.TokenBreakdown = report.TokenBreakdown{InputTokens: 100, CachedTokens: 40, OutputTokens: 20, TotalTokens: 125}
	r.MemberEvaluation.Members = append(r.MemberEvaluation.Members, report.MemberPoint{Name: "下半区确认成员", Quadrant: report.QuadrantRoutine, UsageIssues: []report.UsageIssue{{Type: report.IssueTypePrefixPollution, Verdict: report.VerdictConfirmed}}})
	got := report.RenderMD(r)
	for _, required := range []string{"输入（未缓存", "缓存（写入", "输出（推理", "总计（缓存不重复计入）", "调用次数", "下半区确认成员"} {
		if !strings.Contains(got, required) {
			t.Errorf("missing v0.6 markdown contract %q", required)
		}
	}
}

// 边界态1：部门全员正常空态 —— 存在问题的人=无，正常使用的人=全部。
func TestRenderMD_DeptAllNormal(t *testing.T) {
	me := &report.MemberEvaluation{
		CostMedianUSD: 100, IssueRatioMedian: 0.1,
		Members: []report.MemberPoint{
			{UserID: "a", Name: "甲", DeptID: "D", CostUSD: 100, IssueCostRatio: 0.05, Quadrant: report.QuadrantRoutine, DominantIssue: report.IssueTypeNormal, Calls: 10},
			{UserID: "b", Name: "乙", DeptID: "D", CostUSD: 250, IssueCostRatio: 0.02, Quadrant: report.QuadrantHighCostNormal, DominantIssue: report.IssueTypeNormal, Calls: 20},
		},
	}
	r := &report.Report{SchemaVersion: "2.0", Meta: mkMeta(report.LevelDepartment, "d1"), DataQuality: report.DataQuality{RealSessionCoverage: 0.78}, MemberEvaluation: me,
		Conclusion: report.Conclusion{CostMainBucket: report.CostBucketCache, DominantIssue: report.IssueTypeNormal, Note: "全员用法正常。"}}
	got := report.RenderMD(r)
	if !strings.Contains(got, "**无**") {
		t.Errorf("problem-member section should say 无, got:\n%s", got)
	}
	if !strings.Contains(got, "用法正常") {
		t.Errorf("normal section should list members")
	}
	if strings.Contains(got, "严重异常（待核查）") {
		t.Errorf("expected safety section to be completely absent, got:\n%s", got)
	}
}

// 边界态2：个人无上周数据降级 —— 环比「无上期」，成本构成仅绝对。
func TestRenderMD_NoPrev(t *testing.T) {
	r := samplePersonal()
	r.CostBreakdown.Prev = nil
	got := report.RenderMD(r)
	if !strings.Contains(got, "无上期") {
		t.Errorf("expected 无上期 delta, got:\n%s", got)
	}
	if !strings.Contains(got, "无上周数据，仅给绝对构成") {
		t.Errorf("expected composition degradation note")
	}
}

// 边界态3：真实会话覆盖率<50% —— 交互类(3.2/3.3)标注疑似说明。
func TestRenderMD_LowCoverage(t *testing.T) {
	r := samplePersonal()
	r.DataQuality.RealSessionCoverage = 0.4
	got := report.RenderMD(r)
	if !strings.Contains(got, "真实会话覆盖率 < 50%") {
		t.Errorf("expected low-coverage banner")
	}
	if !strings.Contains(got, "多任务不切会话") {
		t.Errorf("expected multi_task card")
	}
}

// 红线：严重异常只用「待核查/仅供参考」，绝不出现「违规/违纪/滥用」。
func TestRenderMD_SafetyRedLine(t *testing.T) {
	got := report.RenderMD(samplePersonal())
	for _, word := range []string{"违规", "违纪", "滥用"} {
		if strings.Contains(got, word) {
			t.Errorf("red line violated: rendered %q", word)
		}
	}
	if !strings.Contains(got, "待核查") {
		t.Errorf("expected 待核查 marker")
	}
}

func TestReportLabelsHideUUIDs(t *testing.T) {
	const id = "a015704a-55e2-4a66-a8e3-63f7ff594a6c"
	if got := report.DisplayKey(id); got != "Key ••••4a6c" {
		t.Fatalf("DisplayKey() = %q, want masked key", got)
	}
	p := report.MemberPoint{DeptID: id, DeptName: "X-Lab"}
	if got := report.MemberDeptLabel(p); got != "X-Lab" {
		t.Fatalf("MemberDeptLabel() = %q, want department name", got)
	}
	p.DeptName = ""
	if got := report.MemberDeptLabel(p); got != "—" {
		t.Fatalf("MemberDeptLabel() = %q, want hidden UUID", got)
	}

	r := samplePersonal()
	r.SafetyAnomaly.TopKey = id
	if got := report.RenderMD(r); strings.Contains(got, id) || !strings.Contains(got, "Key ••••4a6c") {
		t.Fatalf("rendered report leaked key UUID:\n%s", got)
	}
}

// 红线：存在问题的人按管理优先级排序，非总成本 —— 安全异常排最前，其次右上，再左上。
func TestRenderMD_ProblemSortPriority(t *testing.T) {
	got := report.RenderMD(sampleDepartment())
	idxSafety := strings.Index(got, "李锦程01")
	idxTopRight := strings.Index(got, "杨士俊")
	idxCoach := strings.Index(got, "李四")
	if !(idxSafety >= 0 && idxTopRight >= 0 && idxCoach >= 0 && idxSafety < idxTopRight && idxTopRight < idxCoach) {
		t.Errorf("problem members not sorted by management priority (safety→topright→coach): got idx %d/%d/%d", idxSafety, idxTopRight, idxCoach)
	}
}

func samplePersonal() *report.Report {
	return &report.Report{
		SchemaVersion: "2.0",
		Meta:          mkMeta(report.LevelPersonal, "u1"),
		DataQuality:   report.DataQuality{RealSessionCoverage: 0.58, SceneLabelCoverage: 0.33, PriceCatalogCoverage: 0.9, PositionLabelCoverage: 1.0},
		CostBreakdown: report.CostBreakdown{
			UncachedInputCostUSD: 1760, CacheCostUSD: 2347, OutputCostUSD: 793, CacheWriteIncluded: true, FailedRequestCostUSD: 210,
			Prev:        &report.CostBreakdownPrev{UncachedInputCostUSD: 93, CacheCostUSD: 105, OutputCostUSD: 37},
			ByModelTier: []report.ModelTierCost{{Model: "deepseek-v4-flash", Tier: "轻量", CostUSD: 2234, UncachedInputCostUSD: 800, CacheCostUSD: 1100, OutputCostUSD: 334}, {Model: "GPT-X", Tier: "高阶", CostUSD: 2009, UncachedInputCostUSD: 700, CacheCostUSD: 900, OutputCostUSD: 409}},
		},
		SafetyAnomaly: report.SafetyAnomaly{Hit: true, UnrelatedCostUSD: 1050, UnrelatedCostRatio: 0.214, TopHours: []string{"01:00-04:00"}, TopModel: "GPT-X", TopKey: "key-9f3a", Excluded: []string{"跨时区", "发布值班"}, Status: report.SafetyAnomalyPendingReview},
		UsageIssues: []report.UsageIssue{
			{Type: report.IssueTypePrefixPollution, Verdict: report.VerdictConfirmed, RequestCount: 940, RequestShare: 0.076, AttributedCostUSD: 1120, AffectedCostUSD: 1800, OverlapCostUSD: 95, Confidence: 0.8, SuggestionKey: "固定/复用前缀"},
			{Type: report.IssueTypeOutputRunaway, Verdict: report.VerdictConfirmed, RequestCount: 812, RequestShare: 0.065, AttributedCostUSD: 180, AffectedCostUSD: 400, Confidence: 0.75, SuggestionKey: "核对是否循环触发长输出"},
			{Type: report.IssueTypeMultiTaskSession, Verdict: report.VerdictSuspected, SessionCount: 14, SessionShare: 0.146, AttributedCostUSD: 128, AffectedCostUSD: 300, Confidence: 0.5, Evidence: map[string]any{"缺任务标签": "疑似"}, SuggestionKey: "新任务开新会话"},
			{Type: report.IssueTypeOverModel, Verdict: report.VerdictNotComputed, Confidence: 0},
			{Type: report.IssueTypeToothpaste, Verdict: report.VerdictNotHit},
		},
		Conclusion: report.Conclusion{CostMainBucket: report.CostBucketCache, IssueCostRatio: 0.29, DominantIssue: report.IssueTypePrefixPollution, Note: "本周成本主要来自缓存；主导问题为前缀污染。"},
	}
}

func sampleDepartment() *report.Report {
	me := &report.MemberEvaluation{
		CostMultipleBaseline: "median", CostMedianUSD: 150, IssueRatioMedian: 0.1375,
		Members: []report.MemberPoint{
			{UserID: "m1", Name: "李锦程01", Position: "AI工程师", DeptID: "X-Lab", CostUSD: 4900, CostMultiple: 3.0, IssueAttributedCostUSD: 1428, IssueCostRatio: 0.29, Quadrant: report.QuadrantPriorityImprove, DominantIssue: report.IssueTypePrefixPollution, DominantIssueCostUSD: 1120, SafetyAnomaly: true, Calls: 12400},
			{UserID: "m2", Name: "杨士俊", Position: "WEB前端", DeptID: "X-Lab", CostUSD: 11520, CostMultiple: 7.1, IssueAttributedCostUSD: 7100, IssueCostRatio: 0.62, Quadrant: report.QuadrantPriorityImprove, DominantIssue: report.IssueTypeMultiTaskSession, DominantIssueCostUSD: 7100, SuspectedOnly: true, Calls: 8000},
			{UserID: "m3", Name: "李四", Position: "测试", DeptID: "X-Lab", CostUSD: 620, CostMultiple: 0.4, IssueAttributedCostUSD: 210, IssueCostRatio: 0.55, Quadrant: report.QuadrantCoach, DominantIssue: report.IssueTypeToothpaste, DominantIssueCostUSD: 210, Calls: 300},
			{UserID: "m4", Name: "王五", Position: "运维", DeptID: "X-Lab", CostUSD: 8200, CostMultiple: 5.4, IssueAttributedCostUSD: 60, IssueCostRatio: 0.007, Quadrant: report.QuadrantHighCostNormal, DominantIssue: report.IssueTypeOutputRunaway, DominantIssueCostUSD: 60, Calls: 3100},
		},
		Clusters: []report.MemberCluster{{Label: "prefix_pollution", MemberIDs: []string{"m1", "m2"}}},
	}
	return &report.Report{
		SchemaVersion: "2.0", Meta: mkMeta(report.LevelDepartment, "d1"),
		DataQuality:      report.DataQuality{RealSessionCoverage: 0.66, SceneLabelCoverage: 0.4, PriceCatalogCoverage: 0.92, PositionLabelCoverage: 0.88},
		CostBreakdown:    report.CostBreakdown{UncachedInputCostUSD: 11700, CacheCostUSD: 24300, OutputCostUSD: 7648, CacheWriteIncluded: true, Prev: &report.CostBreakdownPrev{UncachedInputCostUSD: 10900, CacheCostUSD: 20900, OutputCostUSD: 7150}},
		SafetyAnomaly:    report.SafetyAnomaly{},
		MemberEvaluation: me,
		Conclusion:       report.Conclusion{CostMainBucket: report.CostBucketCache, IssueCostRatio: 0.2, DominantIssue: report.IssueTypePrefixPollution, Note: "本部门成本主要来自缓存；问题集中在少数右上成员。"},
	}
}

func sampleStudio() *report.Report {
	me := &report.MemberEvaluation{
		CostMultipleBaseline: "median", CostMedianUSD: 200, IssueRatioMedian: 0.1,
		Members: []report.MemberPoint{
			{UserID: "m1", Name: "李锦程01", Position: "AI工程师", DeptID: "X-Lab", CostUSD: 4900, CostMultiple: 3.0, IssueAttributedCostUSD: 1428, IssueCostRatio: 0.29, Quadrant: report.QuadrantPriorityImprove, DominantIssue: report.IssueTypePrefixPollution, DominantIssueCostUSD: 1120, SafetyAnomaly: true, Calls: 12400},
			{UserID: "m5", Name: "郑十一", Position: "后端", DeptID: "公共技术二部", CostUSD: 380, CostMultiple: 1.9, IssueAttributedCostUSD: 140, IssueCostRatio: 0.48, Quadrant: report.QuadrantCoach, DominantIssue: report.IssueTypeOutputRunaway, DominantIssueCostUSD: 140, Calls: 210},
			{UserID: "m6", Name: "陈九", Position: "产品", DeptID: "X-Lab", CostUSD: 9900, CostMultiple: 5.0, IssueAttributedCostUSD: 100, IssueCostRatio: 0.01, Quadrant: report.QuadrantHighCostNormal, DominantIssue: report.IssueTypeNormal, Calls: 4200},
		},
	}
	return &report.Report{
		SchemaVersion: "2.0", Meta: mkMeta(report.LevelStudio, "s1"),
		DataQuality:      report.DataQuality{RealSessionCoverage: 0.64, SceneLabelCoverage: 0.39, PriceCatalogCoverage: 0.91, PositionLabelCoverage: 0.87},
		CostBreakdown:    report.CostBreakdown{UncachedInputCostUSD: 12900, CacheCostUSD: 26900, OutputCostUSD: 8379, CacheWriteIncluded: true, Prev: &report.CostBreakdownPrev{UncachedInputCostUSD: 12000, CacheCostUSD: 23400, OutputCostUSD: 7900}},
		SafetyAnomaly:    report.SafetyAnomaly{},
		MemberEvaluation: me,
		Conclusion:       report.Conclusion{CostMainBucket: report.CostBucketCache, IssueCostRatio: 0.18, DominantIssue: report.IssueTypePrefixPollution, Note: "工作室成本主要来自缓存；问题集中在少数右上成员。"},
	}
}
