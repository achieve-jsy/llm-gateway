package main

import (
	"math"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

func TestRenderHTML_V06LeadershipAndContract(t *testing.T) {
	r := &report.Report{Meta: report.Meta{Level: report.LevelPersonal, Scope: report.Scope{Name: "测试用户"}, Period: report.Period{From: "2026-09-01", To: "2026-09-07"}, Currency: report.Currency{Report: "USD"}, RuleVersion: "v0.6"}, TotalCalls: 9, CostBreakdown: report.CostBreakdown{TokenBreakdown: report.TokenBreakdown{InputTokens: 100, UncachedInputTokens: 60, CachedTokens: 40, OutputTokens: 20, ReasoningTokens: 5, TotalTokens: 125}, UncachedInputCostUSD: 1, CacheCostUSD: 2, OutputCostUSD: 3, FailedRequestCostUSD: .5}, Conclusion: report.Conclusion{Note: "本周结论"}}
	got := renderHTML(r)
	for _, forbidden := range []string{"综合评级", "部门排位", "Top 15%", "Top 40%", "优秀实践"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("v0.6 HTML contains forbidden rating language %q", forbidden)
		}
	}
	for _, required := range []string{"本周结论", "核心数据", "下一步行动", "输入 Token", "缓存 Token", "输出 Token", "调用次数", "无效消耗（失败请求）", "展开全部明细", "返回顶部", "@media print"} {
		if !strings.Contains(got, required) {
			t.Errorf("missing leadership/contract output %q", required)
		}
	}
	if strings.Contains(got, "严重异常（待核查）") {
		t.Error("empty safety section must be hidden")
	}
}

func TestRenderHTML_V06LeadershipProgressiveDisclosure(t *testing.T) {
	levels := []string{report.LevelPersonal, report.LevelDepartment, report.LevelStudio}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			r := &report.Report{
				Meta:          report.Meta{Level: level, Scope: report.Scope{Name: "领导摘要验收"}, Period: report.Period{From: "2026-09-01", To: "2026-09-07"}, Currency: report.Currency{Report: "USD"}, RuleVersion: "v0.6"},
				TotalCalls:    3,
				CostBreakdown: report.CostBreakdown{TokenBreakdown: report.TokenBreakdown{InputTokens: 10, CachedTokens: 4, OutputTokens: 2, TotalTokens: 12}},
				Conclusion:    report.Conclusion{Note: "核心结论可读"},
			}
			if level != report.LevelPersonal {
				r.MemberEvaluation = &report.MemberEvaluation{Members: []report.MemberPoint{{UserID: "u1", Name: "成员甲", Position: "开发", DeptID: "部门甲", Calls: 3, Quadrant: report.QuadrantRoutine, DominantIssue: report.IssueTypeNormal}}}
			}

			got := renderHTML(r)
			// 核心摘要必须先于默认折叠的技术区，移除脚本后仍是可读静态 HTML。
			technical := strings.Index(got, "<details class=\"technical-section\"")
			for _, marker := range []string{"id=\"conclusion\"", "id=\"core-data\"", "id=\"next-actions\""} {
				idx := strings.Index(got, marker)
				if idx < 0 || technical < 0 || idx > technical {
					t.Errorf("leadership marker %q must exist before technical details: marker=%d details=%d", marker, idx, technical)
				}
			}
			if !strings.Contains(got, "核心结论可读") {
				t.Error("core conclusion must remain in server-rendered HTML without JavaScript")
			}
			for _, native := range []string{"<nav class=\"page-nav\" aria-label=\"页内导航\"", "href=\"#conclusion\"", "href=\"#top\"", "<button type=\"button\" id=\"toggle-details\"", "<details class=\"technical-section\"><summary>"} {
				if !strings.Contains(got, native) {
					t.Errorf("missing native navigation/disclosure structure %q", native)
				}
			}
			if strings.Contains(got, "<details class=\"technical-section\" open") {
				t.Error("technical section must be collapsed by default")
			}
			for _, css := range []string{".table-scroll { overflow-x:auto", "@media (max-width: 760px)", "@media print", "details > * { display:block !important;", "details > summary { display:none !important;"} {
				if !strings.Contains(got, css) {
					t.Errorf("missing mobile/print progressive enhancement CSS %q", css)
				}
			}
		})
	}
}

func TestRenderHTML_HidesInternalUUIDs(t *testing.T) {
	const keyID = "a015704a-55e2-4a66-a8e3-63f7ff594a6c"
	const deptID = "377A16DD-EC42-4992-AF77-ABFC4E7FA305"
	r := &report.Report{
		Meta: report.Meta{
			Level:       report.LevelStudio,
			Scope:       report.Scope{Name: "测试工作室"},
			Period:      report.Period{From: "2026-09-01", To: "2026-09-07"},
			Currency:    report.Currency{Report: "USD"},
			RuleVersion: "v0.6",
		},
		MemberEvaluation: &report.MemberEvaluation{Members: []report.MemberPoint{{
			UserID:        "u1",
			Name:          "成员甲",
			Position:      "开发",
			DeptID:        deptID,
			DeptName:      "X-Lab",
			SafetyAnomaly: true,
			SafetyDetail: report.SafetyAnomaly{
				Hit:      true,
				TopKey:   keyID,
				TopModel: "model-a",
				TopHours: []string{"01:00-02:00"},
				Status:   report.SafetyAnomalyPendingReview,
			},
			Quadrant:      report.QuadrantRoutine,
			DominantIssue: report.IssueTypeNormal,
		}}},
	}

	got := renderHTML(r)
	for _, leaked := range []string{keyID, deptID} {
		if strings.Contains(got, leaked) {
			t.Fatalf("rendered HTML leaked internal UUID %q", leaked)
		}
	}
	for _, visible := range []string{"X-Lab", "Key ••••4a6c"} {
		if !strings.Contains(got, visible) {
			t.Fatalf("rendered HTML missing readable label %q", visible)
		}
	}
}

func TestRenderHTML_Personal(t *testing.T) {
	r := &report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level:       report.LevelPersonal,
			Scope:       report.Scope{ID: "u1", Name: "李锦程"},
			Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
			Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
			RuleVersion: "v0.6",
		},
		DataQuality: report.DataQuality{
			RealSessionCoverage:   0.85,
			SceneLabelCoverage:    0.70,
			PriceCatalogCoverage:  0.95,
			PositionLabelCoverage: 0.90,
		},
		CostBreakdown: report.CostBreakdown{
			UncachedInputCostUSD: 40.0,
			CacheCostUSD:         30.0,
			OutputCostUSD:        30.0,
		},
		SafetyAnomaly: report.SafetyAnomaly{
			Hit:                true,
			UnrelatedCostRatio: 0.22,
			UnrelatedCostUSD:   22.0,
			TopModel:           "qwen3.8-max",
			TopKey:             "dev-key-1",
			Status:             report.SafetyAnomalyPendingReview,
		},
		UsageIssues: []report.UsageIssue{
			{
				Type:              report.IssueTypePrefixPollution,
				Verdict:           report.VerdictConfirmed,
				RequestCount:      12,
				RequestShare:      0.15,
				AttributedCostUSD: 18.0,
				AffectedCostUSD:   30.0,
				Evidence: map[string]any{
					"reason_suspected": "长前缀未复用缓存",
				},
			},
		},
		Conclusion: report.Conclusion{
			CostMainBucket: report.CostBucketUncachedInput,
			IssueCostRatio: 0.18,
			DominantIssue:  report.IssueTypePrefixPollution,
			Note:           "成本主要来自非缓存输入，主导问题为前缀污染，已定性。",
		},
	}

	htmlContent := renderHTML(r)
	if htmlContent == "" {
		t.Fatal("renderHTML returned empty string")
	}

	// 验证包含头部与标题
	if !strings.Contains(htmlContent, "个人报告 · 李锦程 · 2026-W36") {
		t.Errorf("missing header title in HTML")
	}

	// 验证严重异常
	if !strings.Contains(htmlContent, "严重异常（待核查）") || !strings.Contains(htmlContent, "22.0%") {
		t.Errorf("missing safety anomaly in HTML")
	}

	// 验证个人报告不包含成员四象限散点图
	if strings.Contains(htmlContent, "4.1 组织成员整合评估图") {
		t.Errorf("personal report should not contain 4.1 quadrant scatter plot")
	}
}

func TestRenderHTML_Department_QuadrantAndClusters(t *testing.T) {
	members := []report.MemberPoint{
		{
			UserID:                 "u1",
			Name:                   "李锦程01",
			Position:               "AI工程师",
			DeptID:                 "X-Lab",
			CostUSD:                300.0,
			CostMultiple:           3.0,
			IssueAttributedCostUSD: 90.0,
			IssueCostRatio:         0.30,
			Quadrant:               report.QuadrantPriorityImprove,
			DominantIssue:          report.IssueTypePrefixPollution,
			DominantIssueCostUSD:   70.0,
			SafetyAnomaly:          true,
			SuspectedOnly:          false,
			Calls:                  1500,
		},
		{
			UserID:                 "u2",
			Name:                   "杨士俊",
			Position:               "前端专家",
			DeptID:                 "X-Lab",
			CostUSD:                250.0,
			CostMultiple:           2.5,
			IssueAttributedCostUSD: 100.0,
			IssueCostRatio:         0.40,
			Quadrant:               report.QuadrantPriorityImprove,
			DominantIssue:          report.IssueTypeMultiTaskSession,
			DominantIssueCostUSD:   80.0,
			SafetyAnomaly:          false,
			SuspectedOnly:          true,
			Calls:                  2200,
		},
		{
			UserID:                 "u3",
			Name:                   "李四",
			Position:               "后端工程师",
			DeptID:                 "公共技术部",
			CostUSD:                60.0,
			CostMultiple:           0.6,
			IssueAttributedCostUSD: 24.0,
			IssueCostRatio:         0.40,
			Quadrant:               report.QuadrantCoach,
			DominantIssue:          report.IssueTypeToothpaste,
			DominantIssueCostUSD:   20.0,
			SafetyAnomaly:          false,
			SuspectedOnly:          false,
			Calls:                  600,
		},
		{
			UserID:                 "u4",
			Name:                   "陈九",
			Position:               "算法专家",
			DeptID:                 "X-Lab",
			CostUSD:                400.0,
			CostMultiple:           4.0,
			IssueAttributedCostUSD: 20.0,
			IssueCostRatio:         0.05,
			Quadrant:               report.QuadrantHighCostNormal,
			DominantIssue:          report.IssueTypeNormal,
			DominantIssueCostUSD:   0.0,
			SafetyAnomaly:          false,
			SuspectedOnly:          false,
			Calls:                  3500,
		},
		{
			UserID:                 "u5",
			Name:                   "王五",
			Position:               "测试工程师",
			DeptID:                 "公共技术部",
			CostUSD:                80.0,
			CostMultiple:           0.8,
			IssueAttributedCostUSD: 5.0,
			IssueCostRatio:         0.06,
			Quadrant:               report.QuadrantRoutine,
			DominantIssue:          report.IssueTypeNormal,
			DominantIssueCostUSD:   0.0,
			SafetyAnomaly:          false,
			SuspectedOnly:          false,
			Calls:                  400,
		},
	}

	me := &report.MemberEvaluation{
		CostMultipleBaseline: "median",
		CostMedianUSD:        100.0,
		IssueRatioMedian:     0.15,
		Members:              members,
		Clusters: []report.MemberCluster{
			{Label: report.IssueTypePrefixPollution, MemberIDs: []string{"u1"}},
			{Label: report.IssueTypeMultiTaskSession, MemberIDs: []string{"u2"}},
			{Label: report.IssueTypeToothpaste, MemberIDs: []string{"u3"}},
			{Label: report.IssueTypeNormal, MemberIDs: []string{"u4", "u5"}},
		},
	}

	r := &report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level:       report.LevelDepartment,
			Scope:       report.Scope{ID: "d1", Name: "技术研发部"},
			Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
			Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
			RuleVersion: "v0.6",
		},
		DataQuality: report.DataQuality{
			RealSessionCoverage:   0.88,
			SceneLabelCoverage:    0.65,
			PriceCatalogCoverage:  0.95,
			PositionLabelCoverage: 0.90,
		},
		CostBreakdown: report.CostBreakdown{
			UncachedInputCostUSD: 400.0,
			CacheCostUSD:         450.0,
			OutputCostUSD:        240.0,
		},
		MemberEvaluation: me,
		Conclusion: report.Conclusion{
			CostMainBucket: report.CostBucketCache,
			IssueCostRatio: 0.22,
			DominantIssue:  report.IssueTypePrefixPollution,
			Note:           "部门主要成本来自缓存，存在少数右上优先改善成员。",
		},
	}

	htmlContent := renderHTML(r)

	// 1. 验证 4.1 四象限散点图主图与 SVG
	if !strings.Contains(htmlContent, "4.1 组织成员整合评估图（四象限散点图）") {
		t.Fatalf("expected 4.1 quadrant title in HTML")
	}
	if !strings.Contains(htmlContent, "<svg viewBox=\"0 0 1040 640\"") {
		t.Errorf("expected SVG viewBox definition")
	}
	if !strings.Contains(htmlContent, "X=1.0 (成本中位)") {
		t.Errorf("expected X=1.0 vertical crosshair")
	}
	if !strings.Contains(htmlContent, "Y=15.0% (中位)") {
		t.Errorf("expected Y median horizontal crosshair with 15.0%%")
	}

	// 2. 验证四个象限指示卡与指示文字
	expectedQuadrants := []string{
		"右上 · 优先改善",
		"左上 · 习惯辅导",
		"右下 · 高用量·正常",
		"左下 · 常规使用",
		"优先看 / 重点关注",
		"教用法",
		"正常不干预",
		"例行",
	}
	for _, eq := range expectedQuadrants {
		if !strings.Contains(htmlContent, eq) {
			t.Errorf("expected quadrant guide text %q in HTML", eq)
		}
	}

	// 3. 验证散点样式
	// 验证严重异常描边与⊗标记
	if !strings.Contains(htmlContent, "stroke=\"#dc2626\" stroke-width=\"2.6\" stroke-dasharray=\"4,2\"") {
		t.Errorf("expected safety anomaly red dashed outer stroke")
	}
	if !strings.Contains(htmlContent, "⊗待核查") {
		t.Errorf("expected safety anomaly ⊗ marker")
	}
	// 验证全疑似虚线样式
	if !strings.Contains(htmlContent, "stroke-dasharray=\"3,2\"") {
		t.Errorf("expected suspected dashed stroke")
	}
	// 验证成员姓名标注
	for _, m := range members {
		if !strings.Contains(htmlContent, m.Name) {
			t.Errorf("expected member name %s rendered in SVG/HTML", m.Name)
		}
	}

	// 4. 验证 4.2 K-Means 聚类分群图
	if !strings.Contains(htmlContent, "4.2 成员问题相似度聚类分群图") {
		t.Fatalf("expected 4.2 cluster title in HTML")
	}
	if !strings.Contains(htmlContent, "cluster-dist-bar") {
		t.Errorf("expected cluster distribution bar in HTML")
	}
	if !strings.Contains(htmlContent, "前缀污染") || !strings.Contains(htmlContent, "多任务不切会话") || !strings.Contains(htmlContent, "挤牙膏") {
		t.Errorf("expected cluster issue labels in HTML")
	}
	if !strings.Contains(htmlContent, "固定复用系统前缀") || !strings.Contains(htmlContent, "新任务开启新会话") {
		t.Errorf("expected cluster unified advice in HTML")
	}

	// 5. 验证图例说明
	if !strings.Contains(htmlContent, "主导问题分色：") || !strings.Contains(htmlContent, "散点样式标识：") {
		t.Errorf("expected complete legend in HTML")
	}
}

func TestRenderHTML_ClusterMembersAutoCompute(t *testing.T) {
	// 测试当 MemberEvaluation.Clusters 为空时，renderHTML 会自动调用 report.ClusterMembers 计算分群
	members := []report.MemberPoint{
		{
			UserID:                 "u1",
			Name:                   "张三",
			CostUSD:                100.0,
			CostMultiple:           1.0,
			IssueAttributedCostUSD: 30.0,
			IssueCostRatio:         0.30,
			Quadrant:               report.QuadrantCoach,
			DominantIssue:          report.IssueTypeToothpaste,
			DominantIssueCostUSD:   30.0,
			Calls:                  500,
		},
		{
			UserID:                 "u2",
			Name:                   "李四",
			CostUSD:                120.0,
			CostMultiple:           1.2,
			IssueAttributedCostUSD: 40.0,
			IssueCostRatio:         0.33,
			Quadrant:               report.QuadrantPriorityImprove,
			DominantIssue:          report.IssueTypeToothpaste,
			DominantIssueCostUSD:   40.0,
			Calls:                  600,
		},
	}

	me := &report.MemberEvaluation{
		CostMultipleBaseline: "median",
		CostMedianUSD:        110.0,
		IssueRatioMedian:     0.31,
		Members:              members,
		Clusters:             nil, // 为空，触发自动计算
	}

	r := &report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level:       report.LevelDepartment,
			Scope:       report.Scope{ID: "d2", Name: "数据部"},
			Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
			Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
			RuleVersion: "v0.6",
		},
		CostBreakdown: report.CostBreakdown{
			UncachedInputCostUSD: 100.0,
			CacheCostUSD:         100.0,
			OutputCostUSD:        20.0,
		},
		MemberEvaluation: me,
		Conclusion: report.Conclusion{
			CostMainBucket: report.CostBucketCache,
			IssueCostRatio: 0.31,
			DominantIssue:  report.IssueTypeToothpaste,
			Note:           "主导挤牙膏。",
		},
	}

	htmlContent := renderHTML(r)
	if !strings.Contains(htmlContent, "4.2 成员问题相似度聚类分群图") {
		t.Fatalf("expected 4.2 cluster title even when clusters was initially nil")
	}
	if !strings.Contains(htmlContent, "挤牙膏") {
		t.Errorf("expected cluster computed for toothpaste")
	}
}

func TestRenderHTML_Department_Full(t *testing.T) {
	members := []report.MemberPoint{
		{
			UserID:                 "u_normal1",
			Name:                   "周八",
			Position:               "前端开发",
			CostUSD:                50.0,
			CostMultiple:           0.5,
			IssueAttributedCostUSD: 2.0,
			IssueCostRatio:         0.04,
			Quadrant:               report.QuadrantRoutine,
			DominantIssue:          report.IssueTypeNormal,
			Calls:                  200,
		},
		{
			UserID:                 "u_coach",
			Name:                   "李四",
			Position:               "测试工程师",
			CostUSD:                80.0,
			CostMultiple:           0.8,
			IssueAttributedCostUSD: 40.0,
			IssueCostRatio:         0.50,
			Quadrant:               report.QuadrantCoach,
			DominantIssue:          report.IssueTypeToothpaste,
			DominantIssueCostUSD:   40.0,
			Calls:                  400,
			UsageIssues: []report.UsageIssue{
				{
					Type:              report.IssueTypeToothpaste,
					Verdict:           report.VerdictConfirmed,
					AttributedCostUSD: 40.0,
					Evidence: map[string]any{
						"reason_suspected": "单会话内连续多轮追问且未重置会话",
					},
				},
			},
		},
		{
			UserID:                 "u_priority",
			Name:                   "杨士俊",
			Position:               "算法专家",
			CostUSD:                1500.0,
			CostMultiple:           15.0,
			IssueAttributedCostUSD: 900.0,
			IssueCostRatio:         0.60,
			Quadrant:               report.QuadrantPriorityImprove,
			DominantIssue:          report.IssueTypeMultiTaskSession,
			DominantIssueCostUSD:   900.0,
			Calls:                  3500,
		},
		{
			UserID:                 "u_safety",
			Name:                   "李锦程01",
			Position:               "后端专家",
			CostUSD:                800.0,
			CostMultiple:           8.0,
			IssueAttributedCostUSD: 200.0,
			IssueCostRatio:         0.25,
			Quadrant:               report.QuadrantPriorityImprove,
			DominantIssue:          report.IssueTypePrefixPollution,
			DominantIssueCostUSD:   200.0,
			SafetyAnomaly:          true,
			Calls:                  2100,
		},
		{
			UserID:                 "u_high_normal",
			Name:                   "陈九",
			Position:               "架构师",
			CostUSD:                1200.0,
			CostMultiple:           12.0,
			IssueAttributedCostUSD: 60.0,
			IssueCostRatio:         0.05,
			Quadrant:               report.QuadrantHighCostNormal,
			DominantIssue:          report.IssueTypeNormal,
			Calls:                  4000,
		},
	}

	me := &report.MemberEvaluation{
		CostMultipleBaseline: "median",
		CostMedianUSD:        100.0,
		IssueRatioMedian:     0.25,
		Members:              members,
	}

	r := &report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level:       report.LevelDepartment,
			Scope:       report.Scope{ID: "d_xlab", Name: "X-Lab"},
			Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
			Currency:    report.Currency{Report: "CNY", USDCNYRate: 7.2},
			RuleVersion: "v0.6",
		},
		DataQuality: report.DataQuality{
			RealSessionCoverage:   0.98,
			SceneLabelCoverage:    0.85,
			PriceCatalogCoverage:  0.92,
			PositionLabelCoverage: 0.88,
		},
		CostBreakdown: report.CostBreakdown{
			UncachedInputCostUSD: 1000.0,
			CacheCostUSD:         2000.0,
			OutputCostUSD:        630.0,
			ByModelTier: []report.ModelTierCost{
				{Model: "qwen3.8-max", Tier: "flagship", CostUSD: 2600.0},
				{Model: "deepseek-v4", Tier: "flagship", CostUSD: 1030.0},
			},
		},
		MemberEvaluation: me,
		Conclusion: report.Conclusion{
			CostMainBucket: report.CostBucketCache,
			IssueCostRatio: 0.32,
			DominantIssue:  report.IssueTypeMultiTaskSession,
			Note:           "本周部门总体成本主要由缓存贡献，存在重点成员多任务不切会话。",
		},
	}

	htmlContent := renderHTML(r)

	// 1. 标题与头部
	if !strings.Contains(htmlContent, "<h1>部门报告 · X-Lab · 2026-W36</h1>") {
		t.Errorf("expected department header with name and week")
	}

	// 2. 基础数据
	if !strings.Contains(htmlContent, "1. 部门本周基础数据") {
		t.Errorf("expected Section 1 title")
	}
	if !strings.Contains(htmlContent, "活跃 <strong>5</strong> 人") {
		t.Errorf("expected active members count")
	}

	// 3. 严重异常置顶
	if !strings.Contains(htmlContent, "2. 严重异常（待核查）") {
		t.Errorf("expected Section 2 safety title")
	}
	if !strings.Contains(htmlContent, "李锦程01") || !strings.Contains(htmlContent, "待核查") {
		t.Errorf("expected safety member in section 2")
	}
	if !strings.Contains(htmlContent, "触发原因：") || !strings.Contains(htmlContent, "缺少完整的时段费用比例") || !strings.Contains(htmlContent, "需要核实：") {
		t.Errorf("expected manager-readable safety reason and review action")
	}

	// 4. 散点图与分群图
	if !strings.Contains(htmlContent, "3. 部门成员整合评估图") {
		t.Errorf("expected Section 3 title")
	}
	if !strings.Contains(htmlContent, "4.1 组织成员整合评估图（四象限散点图）") {
		t.Errorf("expected 4.1 title")
	}
	if !strings.Contains(htmlContent, "4.2 成员问题相似度聚类分群图") {
		t.Errorf("expected 4.2 cluster title")
	}
	if !strings.Contains(htmlContent, "全员定位表") || !strings.Contains(htmlContent, "data-member-index=\"01\"") {
		t.Errorf("expected numbered quadrant points and full member directory")
	}
	if count := strings.Count(htmlContent, "class=\"point-index\""); count != len(members) {
		t.Errorf("expected one visible point index per member, got %d want %d", count, len(members))
	}
	if strings.Contains(htmlContent, "data-y-max=\"1.00\"") || !strings.Contains(htmlContent, "data-y-max=\"") {
		t.Errorf("expected adaptive Y axis instead of fixed 100%% scale")
	}
	if !strings.Contains(htmlContent, "横纵轴按组织中位线分段展开") {
		t.Errorf("expected scale disclosure below quadrant")
	}

	// 5. 存在问题的人（按管理优先级排序）
	if !strings.Contains(htmlContent, "4. 存在问题的人（按管理优先级，非总成本排序）") {
		t.Errorf("expected Section 4 problem members title")
	}
	// 验证排序：李锦程01 (safety rank 0) 应该出现在 杨士俊 (priority rank 1) 之前
	idxSafety := strings.Index(htmlContent, "李锦程01")
	idxPriority := strings.Index(htmlContent, "杨士俊")
	idxCoach := strings.Index(htmlContent, "李四")
	if idxSafety == -1 || idxPriority == -1 || idxCoach == -1 {
		t.Fatalf("expected all problem members present")
	}
	if idxSafety >= idxPriority {
		t.Errorf("expected safety member 李锦程01 to precede priority member 杨士俊 in management priority")
	}

	// 验证展开卡
	if !strings.Contains(htmlContent, "<details class=\"member-detail-card") {
		t.Errorf("expected collapsible details card for problem members")
	}
	if !strings.Contains(htmlContent, "为什么这样判断") || !strings.Contains(htmlContent, "需求被拆成多次补充") {
		t.Errorf("expected manager-readable reason in expand card")
	}
	for _, internalTerm := range []string{"P1 无", "reset_flag", "CSA 可据", "Evidence 合成", "判定封顶 suspected"} {
		if strings.Contains(htmlContent, internalTerm) {
			t.Errorf("manager-facing HTML leaked internal term %q", internalTerm)
		}
	}

	// 6. 正常使用的人
	if !strings.Contains(htmlContent, "5. 正常使用的人（大概展示，不展开五类）") {
		t.Errorf("expected Section 5 normal members title")
	}
	if !strings.Contains(htmlContent, "陈九") || !strings.Contains(htmlContent, "高用量·用法正常") {
		t.Errorf("expected Chen Jiu with high cost normal verdict")
	}
	if !strings.Contains(htmlContent, "周八") || !strings.Contains(htmlContent, "用法正常") {
		t.Errorf("expected Zhou Ba with routine normal verdict")
	}
}

func TestQuadrantScaleKeepsMedianReadable(t *testing.T) {
	if got := quadrantXPos(1, 8); math.Abs(got-0.45) > 1e-9 {
		t.Fatalf("median x position = %.3f, want 0.45", got)
	}
	if got := quadrantXPos(0, 8); got != 0 {
		t.Fatalf("zero-cost x position = %.3f, want 0", got)
	}
	if got := quadrantYPos(0.08, 0.08, 0.60); math.Abs(got-0.30) > 1e-9 {
		t.Fatalf("median y position = %.3f, want 0.30", got)
	}
	maxY, step := quadrantYAxis([]report.MemberPoint{{IssueCostRatio: 0.128}, {IssueCostRatio: 0.492}}, 0.078)
	if maxY != 0.60 || step != 0.10 {
		t.Fatalf("adaptive y axis = %.2f/%.2f, want 0.60/0.10", maxY, step)
	}
}

func TestRenderHTML_Studio_Full(t *testing.T) {
	members := []report.MemberPoint{
		{
			UserID:                 "u_xlab_safety",
			Name:                   "李锦程01",
			Position:               "后端专家",
			DeptID:                 "X-Lab",
			CostUSD:                700.0,
			CostMultiple:           7.0,
			IssueAttributedCostUSD: 160.0,
			IssueCostRatio:         0.23,
			Quadrant:               report.QuadrantPriorityImprove,
			DominantIssue:          report.IssueTypePrefixPollution,
			DominantIssueCostUSD:   160.0,
			SafetyAnomaly:          true,
			Calls:                  2100,
		},
		{
			UserID:                 "u_xlab_prob",
			Name:                   "杨士俊",
			Position:               "算法专家",
			DeptID:                 "X-Lab",
			CostUSD:                1600.0,
			CostMultiple:           16.0,
			IssueAttributedCostUSD: 980.0,
			IssueCostRatio:         0.61,
			Quadrant:               report.QuadrantPriorityImprove,
			DominantIssue:          report.IssueTypeMultiTaskSession,
			DominantIssueCostUSD:   980.0,
			Calls:                  3200,
		},
		{
			UserID:                 "u_pub_prob",
			Name:                   "郑十一",
			Position:               "平台工程师",
			DeptID:                 "公共技术二部",
			CostUSD:                90.0,
			CostMultiple:           0.9,
			IssueAttributedCostUSD: 45.0,
			IssueCostRatio:         0.50,
			Quadrant:               report.QuadrantCoach,
			DominantIssue:          report.IssueTypeOutputRunaway,
			DominantIssueCostUSD:   45.0,
			Calls:                  500,
		},
		{
			UserID:                 "u_pub_normal",
			Name:                   "张十二",
			Position:               "运维开发",
			DeptID:                 "公共技术二部",
			CostUSD:                60.0,
			CostMultiple:           0.6,
			IssueAttributedCostUSD: 3.0,
			IssueCostRatio:         0.05,
			Quadrant:               report.QuadrantRoutine,
			DominantIssue:          report.IssueTypeNormal,
			Calls:                  300,
		},
	}

	me := &report.MemberEvaluation{
		CostMultipleBaseline: "median",
		CostMedianUSD:        100.0,
		IssueRatioMedian:     0.20,
		Members:              members,
	}

	r := &report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level:       report.LevelStudio,
			Scope:       report.Scope{ID: "s_ai", Name: "游戏AI工作室"},
			Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
			Currency:    report.Currency{Report: "CNY", USDCNYRate: 7.2},
			RuleVersion: "v0.6",
		},
		DataQuality: report.DataQuality{
			RealSessionCoverage:   1.0,
			SceneLabelCoverage:    0.95,
			PriceCatalogCoverage:  0.95,
			PositionLabelCoverage: 0.90,
		},
		CostBreakdown: report.CostBreakdown{
			UncachedInputCostUSD: 800.0,
			CacheCostUSD:         1400.0,
			OutputCostUSD:        250.0,
		},
		MemberEvaluation: me,
		Conclusion: report.Conclusion{
			CostMainBucket: report.CostBucketCache,
			IssueCostRatio: 0.48,
			DominantIssue:  report.IssueTypeMultiTaskSession,
			Note:           "工作室本周主要成本集中于 X-Lab，整体缓存利用健康但存在混会话。",
		},
	}

	htmlContent := renderHTML(r)

	// 1. 标题与头部
	if !strings.Contains(htmlContent, "<h1>工作室报告 · 游戏AI工作室 · 2026-W36</h1>") {
		t.Errorf("expected studio header with name and week")
	}

	// 2. 工作室本周基础数据
	if !strings.Contains(htmlContent, "1. 工作室本周基础数据") {
		t.Errorf("expected Section 1 studio base data title")
	}

	// 3. 各部门基础数据（对照构成，不排名）
	if !strings.Contains(htmlContent, "2. 各部门基础数据（对照构成，不排名）") {
		t.Errorf("expected Section 2 dept breakdown title")
	}
	if !strings.Contains(htmlContent, "X-Lab") || !strings.Contains(htmlContent, "公共技术二部") {
		t.Errorf("expected departments X-Lab and Public Tech in breakdown table")
	}

	// 4. 严重异常带部门置顶
	if !strings.Contains(htmlContent, "3. 严重异常（待核查，带部门）") {
		t.Errorf("expected Section 3 studio safety title with dept")
	}
	if !strings.Contains(htmlContent, "李锦程01") || !strings.Contains(htmlContent, "X-Lab") {
		t.Errorf("expected safety member with department name")
	}

	// 5. 工作室散点主图（点形区分部门）
	if !strings.Contains(htmlContent, "4. 工作室成员整合评估图（全员四象限散点图与分群）") {
		t.Errorf("expected Section 4 studio evaluation title")
	}
	if !strings.Contains(htmlContent, "部门点形标识：") {
		t.Errorf("expected department shape legend in studio report")
	}
	if !strings.Contains(htmlContent, "<polygon points=") && !strings.Contains(htmlContent, "<circle cx=") {
		t.Errorf("expected distinct SVG shapes for departments")
	}

	// 6. 各部门存在问题的人分组卡片
	if !strings.Contains(htmlContent, "5. 各部门存在问题的人") {
		t.Errorf("expected Section 5 studio problem by dept title")
	}
	if !strings.Contains(htmlContent, "【X-Lab】") || !strings.Contains(htmlContent, "【公共技术二部】") {
		t.Errorf("expected department grouped problem sections")
	}

	// 7. §5.3：正常使用的人不上工作室报告（已在各部门基础数据和全员图里能看见位置）。
	if strings.Contains(htmlContent, "6. 正常使用的人（全工作室汇总）") {
		t.Errorf("§5.3 studio report must NOT render a 'normal members' section, but it did")
	}

	// 8. 暗色/亮色自适应 CSS 验证
	if !strings.Contains(htmlContent, "@media (prefers-color-scheme: dark)") {
		t.Errorf("expected prefers-color-scheme: dark in CSS")
	}
}
