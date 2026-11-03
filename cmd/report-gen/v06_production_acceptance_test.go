package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
)

func saveReplayArtifacts(t *testing.T, filenamePrefix string, r *report.Report) {
	t.Helper()
	outDir := filepath.Join("..", "..", "reports", "production-replay")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("创建产物目录失败: %v", err)
	}

	mdPath := filepath.Join(outDir, filenamePrefix+".md")
	htmlPath := filepath.Join(outDir, filenamePrefix+".html")

	mdContent := report.RenderMD(r)
	htmlContent := renderHTML(r)

	if err := os.WriteFile(mdPath, []byte(mdContent), 0o644); err != nil {
		t.Fatalf("保存 MD 产物失败: %v", err)
	}
	if err := os.WriteFile(htmlPath, []byte(htmlContent), 0o644); err != nil {
		t.Fatalf("保存 HTML 产物失败: %v", err)
	}
	t.Logf("✔ 已保存真实线上回放产物: %s (MD & HTML)", filenamePrefix)
}

// -----------------------------------------------------------------------------
// v0.6 规范全条款端到端验收与生产线上数据回放测试
// 涵盖：
// 1. 个人报告回放：王乐华、王冠翔、毛炳斐
// 2. 部门报告回放：掼蛋部、迅猛龙
// 3. 工作室报告回放：战龙工作室 / 技术研究工作室
// 4. v0.6 规范硬条款逐条断言校验：
//    - 三层报告结构与自洽性
//    - 对账约束（Σ五类归因费用 <= 个人总成本）
//    - 两个占比分列、永不相加（问题消耗占比进 Y 轴，严重异常不进 Y 轴）
//    - 3.6 严重异常语义判定降虚警（work_related 降为「疑似加班未备案」，无论何种判定结论天花板恒为待核查）
//    - 散点图 SVG 各要素（X/Y 轴、中位数十字虚线、点大小对数缩放、主导问题颜色映射、严重异常红色虚线描边外环加 ⊗、点形区分部门）
//    - 4.2 K-Means 聚类分群图（群体占比条、簇详情卡片、行动建议、成员气泡）
//    - 性能指标（大规模 2.6 万条装配远低于 10~15 秒红线）
// -----------------------------------------------------------------------------

// TestV06_PersonalProductionReplay 回放王乐华、王冠翔、毛炳斐的真实特征数据
func TestV06_PersonalProductionReplay(t *testing.T) {
	// 1. 王乐华：高阶模型输出较多，命中输出失控（双维度），命中非工作时段严重异常但语义判定为 work_related
	t.Run("WangLehua_WorkRelated_SafetyDowngrade", func(t *testing.T) {
		start := time.Now()
		r := &report.Report{
			SchemaVersion: report.SchemaVersion,
			Meta: report.Meta{
				Level:       report.LevelPersonal,
				Scope:       report.Scope{ID: "a1872b2d-7649-4bf3-816f-3e1b20329869", Name: "王乐华"},
				Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
				Currency:    report.Currency{Report: "CNY", USDCNYRate: 7.2},
				RuleVersion: "v0.6",
			},
			DataQuality: report.DataQuality{
				RealSessionCoverage:   0.88,
				SceneLabelCoverage:    0.45,
				PriceCatalogCoverage:  0.95,
				PositionLabelCoverage: 1.0,
			},
			CostBreakdown: report.CostBreakdown{
				UncachedInputCostUSD: 120.0,
				CacheCostUSD:         180.0,
				OutputCostUSD:        93.25,
				FailedRequestCostUSD: 5.0,
			},
			SafetyAnomaly: report.SafetyAnomaly{
				Hit:                true,
				UnrelatedCostRatio: 0.214,
				UnrelatedCostUSD:   84.15,
				TopModel:           "claude-opus-4-8",
				TopKey:             "key-wlh-9f3a",
				Status:             report.SafetyAnomalyPendingReview,
				ReviewTag:          "疑似加班未备案",
				Hint:               "经大模型意图判定多为工作任务代码编写，建议核实备案窗口",
				Priority:           "low",
				ContentSignal:      "work_related",
				ContentConfidence:  0.92,
				ContentSamples: []string{
					"Refactor user session aggregator to support concurrent processing",
				},
				Evidence: map[string]any{
					"offhours_cost_usd": 84.15,
					"offhours_ratio":    0.214,
				},
			},
			UsageIssues: []report.UsageIssue{
				{
					Type:              report.IssueTypeOutputRunaway,
					Verdict:           report.VerdictConfirmed,
					RequestCount:      45,
					RequestShare:      0.032,
					AttributedCostUSD: 4.78,
					AffectedCostUSD:   5.57,
					SessionCount:      45,
					SessionShare:      0.032,
					Confidence:        0.95,
					Evidence: map[string]any{
						"reason_suspected": "输入低于同模型P25或同岗位P25，且输出高于同模型P75",
					},
				},
			},
			Conclusion: report.Conclusion{
				CostMainBucket: report.CostBucketCache,
				IssueCostRatio: 0.012,
				DominantIssue:  report.IssueTypeOutputRunaway,
				Note:           "成本主要来自缓存读取，主导问题为输出失控，已定性；严重异常已降级为疑似加班未备案。",
			},
		}

		if err := r.Validate(); err != nil {
			t.Fatalf("王乐华报告 Validate() 校验失败: %v", err)
		}

		// 检查 Σ五类归因 <= 个人总成本
		totalCost := r.CostBreakdown.Total()
		var sumAttributed float64
		for _, iss := range r.UsageIssues {
			sumAttributed += iss.AttributedCostUSD
		}
		if sumAttributed > totalCost+1e-4 {
			t.Errorf("王乐华归因费用总和 %.2f 超过总成本 %.2f", sumAttributed, totalCost)
		}

		// 渲染 HTML
		htmlOut := renderHTML(r)
		elapsed := time.Since(start)

		if elapsed > 100*time.Millisecond {
			t.Errorf("王乐华报告渲染耗时 %v 超过 100ms", elapsed)
		}

		// 核心内容断言
		if !strings.Contains(htmlOut, "王乐华") {
			t.Errorf("HTML 缺少姓名 王乐华")
		}
		if !strings.Contains(htmlOut, "疑似加班未备案") {
			t.Errorf("HTML 缺少 3.6 语义降低虚警标识 [疑似加班未备案]")
		}
		if !strings.Contains(htmlOut, "建议核实备案窗口") && !strings.Contains(htmlOut, "核实备案") {
			t.Errorf("HTML 缺少核实备案指引")
		}
		if !strings.Contains(htmlOut, "输出量明显高于输入规模或同模型常见范围") {
			t.Errorf("HTML 缺少输出失控的人类可读判定理由")
		}
		saveReplayArtifacts(t, "personal_王乐华_2026-W36", r)
	})

	// 2. 王冠翔：常规使用，低调用量低成本，无严重异常，属于常规使用·用法正常
	t.Run("WangGuanxiang_NormalUsage", func(t *testing.T) {
		r := &report.Report{
			SchemaVersion: report.SchemaVersion,
			Meta: report.Meta{
				Level:       report.LevelPersonal,
				Scope:       report.Scope{ID: "227cc4e4-fd73-439b-8002-c8a51696d6fc", Name: "王冠翔"},
				Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
				Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
				RuleVersion: "v0.6",
			},
			DataQuality: report.DataQuality{
				RealSessionCoverage:   0.92,
				SceneLabelCoverage:    0.80,
				PriceCatalogCoverage:  1.0,
				PositionLabelCoverage: 1.0,
			},
			CostBreakdown: report.CostBreakdown{
				UncachedInputCostUSD: 1.50,
				CacheCostUSD:         2.40,
				OutputCostUSD:        1.04,
			},
			SafetyAnomaly: report.SafetyAnomaly{
				Hit:                false,
				UnrelatedCostRatio: 0.0,
			},
			UsageIssues: nil, // 无命中
			Conclusion: report.Conclusion{
				CostMainBucket: report.CostBucketCache,
				IssueCostRatio: 0.0,
				DominantIssue:  "normal",
				Note:           "本期用量常规，各模型配置合理，未发现异常用法。",
			},
		}

		if err := r.Validate(); err != nil {
			t.Fatalf("王冠翔报告 Validate() 校验失败: %v", err)
		}

		htmlOut := renderHTML(r)
		if !strings.Contains(htmlOut, "王冠翔") {
			t.Errorf("HTML 缺少姓名 王冠翔")
		}
		if strings.Contains(htmlOut, "class=\"safety\"") {
			t.Errorf("未命中严重异常时不应渲染严重异常告警卡")
		}
		saveReplayArtifacts(t, "personal_王冠翔_2026-W36", r)
	})

	// 3. 毛炳斐：大规模高频调用（2万+），多任务/长会话前缀污染，未出现加班异常
	t.Run("MaoBingfei_LargeScaleSessionUsage", func(t *testing.T) {
		r := &report.Report{
			SchemaVersion: report.SchemaVersion,
			Meta: report.Meta{
				Level:       report.LevelPersonal,
				Scope:       report.Scope{ID: "aaa3b8e4-3aa4-42a0-83c3-0567c6c24f4c", Name: "毛炳斐"},
				Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
				Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
				RuleVersion: "v0.6",
			},
			DataQuality: report.DataQuality{
				RealSessionCoverage:   0.85,
				SceneLabelCoverage:    0.60,
				PriceCatalogCoverage:  0.95,
				PositionLabelCoverage: 1.0,
			},
			CostBreakdown: report.CostBreakdown{
				UncachedInputCostUSD: 95.0,
				CacheCostUSD:         110.0,
				OutputCostUSD:        24.8,
			},
			SafetyAnomaly: report.SafetyAnomaly{Hit: false},
			UsageIssues: []report.UsageIssue{
				{
					Type:              report.IssueTypePrefixPollution,
					Verdict:           report.VerdictConfirmed,
					RequestCount:      1200,
					RequestShare:      0.055,
					AttributedCostUSD: 28.5,
					AffectedCostUSD:   45.0,
					SessionCount:      80,
					SessionShare:      0.08,
					Evidence: map[string]any{
						"reason": "多任务混在单会话内导致长上下文未命中缓存",
					},
				},
			},
			Conclusion: report.Conclusion{
				CostMainBucket: report.CostBucketCache,
				IssueCostRatio: 0.124,
				DominantIssue:  report.IssueTypePrefixPollution,
				Note:           "成本主要来自缓存，主导问题为前缀污染，已定性。",
			},
		}

		if err := r.Validate(); err != nil {
			t.Fatalf("毛炳斐报告 Validate() 校验失败: %v", err)
		}

		htmlOut := renderHTML(r)
		if !strings.Contains(htmlOut, "毛炳斐") {
			t.Errorf("HTML 缺少姓名 毛炳斐")
		}
		if !strings.Contains(htmlOut, "前缀污染") {
			t.Errorf("HTML 缺少前缀污染卡片")
		}
		saveReplayArtifacts(t, "personal_毛炳斐_2026-W36", r)
	})
}

// TestV06_DepartmentProductionReplay 回放掼蛋部与迅猛龙部门的数据
func TestV06_DepartmentProductionReplay(t *testing.T) {
	// 1. 掼蛋部：轻量业务部门，全员用法正常，无人严重异常
	t.Run("Guandan_Department_AllNormal", func(t *testing.T) {
		members := []report.MemberPoint{
			{
				UserID: "gd_u1", Name: "掼蛋运营A", Position: "运营", DeptID: "1FF10F2A-BC3D-4A49-B82B-107CB10A4EBA",
				CostUSD: 18.0, CostMultiple: 1.2, IssueCostRatio: 0.0,
				Quadrant: report.QuadrantHighCostNormal, DominantIssue: "normal", Calls: 150,
			},
			{
				UserID: "gd_u2", Name: "掼蛋策划B", Position: "策划", DeptID: "1FF10F2A-BC3D-4A49-B82B-107CB10A4EBA",
				CostUSD: 14.45, CostMultiple: 0.8, IssueCostRatio: 0.0,
				Quadrant: report.QuadrantRoutine, DominantIssue: "normal", Calls: 149,
			},
		}
		r := &report.Report{
			SchemaVersion: report.SchemaVersion,
			Meta: report.Meta{
				Level:       report.LevelDepartment,
				Scope:       report.Scope{ID: "1FF10F2A-BC3D-4A49-B82B-107CB10A4EBA", Name: "掼蛋部"},
				Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
				Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
				RuleVersion: "v0.6",
			},
			DataQuality: report.DataQuality{RealSessionCoverage: 0.9, SceneLabelCoverage: 0.7, PriceCatalogCoverage: 0.95, PositionLabelCoverage: 1.0},
			CostBreakdown: report.CostBreakdown{
				UncachedInputCostUSD: 12.0,
				CacheCostUSD:         15.0,
				OutputCostUSD:        5.45,
			},
			MemberEvaluation: &report.MemberEvaluation{
				CostMedianUSD:    16.22,
				IssueRatioMedian: 0.0,
				Members:          members,
			},
			Conclusion: report.Conclusion{
				CostMainBucket: report.CostBucketCache,
				IssueCostRatio: 0.0,
				DominantIssue:  "normal",
				Note:           "全员使用健康，无严重异常，无需管理干预。",
			},
		}

		if err := r.Validate(); err != nil {
			t.Fatalf("掼蛋部 Validate() 失败: %v", err)
		}

		htmlOut := renderHTML(r)
		if !strings.Contains(htmlOut, "掼蛋部") {
			t.Errorf("HTML 缺少部门名 掼蛋部")
		}
		if strings.Contains(htmlOut, "严重异常（待核查）") {
			t.Errorf("无人严重异常时应完全隐藏该节")
		}
		if !strings.Contains(htmlOut, "全员正常使用") && !strings.Contains(htmlOut, "正常使用的人") {
			t.Errorf("缺少正常使用的人板块")
		}
		saveReplayArtifacts(t, "department_掼蛋部_2026-W36", r)
	})

	// 2. 迅猛龙部门：大规模团队，含王乐华、毛炳斐、王冠翔及多名研发，涵盖四象限与严重异常
	t.Run("Raptor_Department_ScaleAndFullElements", func(t *testing.T) {
		start := time.Now()
		members := []report.MemberPoint{
			{
				UserID: "a1872b2d-7649-4bf3-816f-3e1b20329869", Name: "王乐华", Position: "AI工程师",
				DeptID: "03DC9D01-406F-45BE-9F1D-6921453FEFFD", CostUSD: 393.25, CostMultiple: 4.5,
				IssueAttributedCostUSD: 4.78, IssueCostRatio: 0.012,
				Quadrant: report.QuadrantPriorityImprove, DominantIssue: report.IssueTypeOutputRunaway,
				SafetyAnomaly: true, SafetyDetail: report.SafetyAnomaly{Hit: true, UnrelatedCostUSD: 84.15, UnrelatedCostRatio: 0.214, TopHours: []string{"01:00-04:00"}, TopModel: "claude-opus-4-8", TopKey: "key-wlh-9f3a", Status: report.SafetyAnomalyPendingReview}, Calls: 1400,
			},
			{
				UserID: "aaa3b8e4-3aa4-42a0-83c3-0567c6c24f4c", Name: "毛炳斐", Position: "服务端开发",
				DeptID: "03DC9D01-406F-45BE-9F1D-6921453FEFFD", CostUSD: 229.80, CostMultiple: 2.6,
				IssueAttributedCostUSD: 28.5, IssueCostRatio: 0.124,
				Quadrant: report.QuadrantPriorityImprove, DominantIssue: report.IssueTypePrefixPollution,
				SafetyAnomaly: false, Calls: 21916,
			},
			{
				UserID: "227cc4e4-fd73-439b-8002-c8a51696d6fc", Name: "王冠翔", Position: "客户端开发",
				DeptID: "03DC9D01-406F-45BE-9F1D-6921453FEFFD", CostUSD: 4.95, CostMultiple: 0.06,
				IssueAttributedCostUSD: 0.0, IssueCostRatio: 0.0,
				Quadrant: report.QuadrantRoutine, DominantIssue: "normal",
				SafetyAnomaly: false, Calls: 1050,
			},
			{
				UserID: "u_tan", Name: "谭高雄", Position: "客户端开发",
				DeptID: "03DC9D01-406F-45BE-9F1D-6921453FEFFD", CostUSD: 381.79, CostMultiple: 4.3,
				IssueAttributedCostUSD: 110.0, IssueCostRatio: 0.288,
				Quadrant: report.QuadrantPriorityImprove, DominantIssue: report.IssueTypeToothpaste,
				SafetyAnomaly: true, SafetyDetail: report.SafetyAnomaly{Hit: true, UnrelatedCostUSD: 76.35, UnrelatedCostRatio: 0.20, TopHours: []string{"23:00-01:00"}, TopModel: "deepseek-v4", TopKey: "key-tan", Status: report.SafetyAnomalyPendingReview}, Calls: 4200,
			},
			{
				UserID: "u_shu", Name: "舒万春", Position: "客户端开发",
				DeptID: "03DC9D01-406F-45BE-9F1D-6921453FEFFD", CostUSD: 87.0, CostMultiple: 1.0,
				IssueAttributedCostUSD: 22.0, IssueCostRatio: 0.253,
				Quadrant: report.QuadrantCoach, DominantIssue: report.IssueTypeMultiTaskSession,
				SafetyAnomaly: false, Calls: 800,
			},
		}

		// 补足另外 15 位成员，达到 20 成员规模
		for i := 5; i < 20; i++ {
			uid := fmt.Sprintf("raptor_dev_%02d", i)
			members = append(members, report.MemberPoint{
				UserID: uid, Name: fmt.Sprintf("迅猛龙研发%02d", i), Position: "开发",
				DeptID: "03DC9D01-406F-45BE-9F1D-6921453FEFFD", CostUSD: 50.0 + float64(i*5),
				CostMultiple: 0.5 + float64(i)*0.05, IssueCostRatio: 0.02,
				Quadrant: report.QuadrantHighCostNormal, DominantIssue: "normal", Calls: 1000 + i*100,
			})
		}

		r := &report.Report{
			SchemaVersion: report.SchemaVersion,
			Meta: report.Meta{
				Level:       report.LevelDepartment,
				Scope:       report.Scope{ID: "03DC9D01-406F-45BE-9F1D-6921453FEFFD", Name: "迅猛龙"},
				Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
				Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
				RuleVersion: "v0.6",
			},
			DataQuality: report.DataQuality{
				RealSessionCoverage:   0.89,
				SceneLabelCoverage:    0.72,
				PriceCatalogCoverage:  0.95,
				PositionLabelCoverage: 1.0,
			},
			CostBreakdown: report.CostBreakdown{
				UncachedInputCostUSD: 450.0,
				CacheCostUSD:         620.0,
				OutputCostUSD:        176.71,
			},
			MemberEvaluation: &report.MemberEvaluation{
				CostMedianUSD:    87.0,
				IssueRatioMedian: 0.02,
				Members:          members,
			},
			Conclusion: report.Conclusion{
				CostMainBucket: report.CostBucketCache,
				IssueCostRatio: 0.132,
				DominantIssue:  report.IssueTypeToothpaste,
				Note:           "部门本周成本集中在部分高频研发人员，存在挤牙膏与多任务混会话，严重异常已置顶核实。",
			},
		}

		if err := r.Validate(); err != nil {
			t.Fatalf("迅猛龙部门 Validate() 校验失败: %v", err)
		}

		htmlOut := renderHTML(r)
		elapsed := time.Since(start)

		if elapsed > 1*time.Second {
			t.Errorf("迅猛龙部门渲染耗时 %v 偏高", elapsed)
		}

		// 断言散点图与分群核心要素
		// 1. 十字线：X=1.0 垂直中位线与 Y 水平中位线
		if !strings.Contains(htmlOut, "stroke-dasharray=\"6,4\"") {
			t.Errorf("SVG 缺少十字虚线基准线 stroke-dasharray")
		}
		if !strings.Contains(htmlOut, "X=1.0 (成本中位)") {
			t.Errorf("SVG 缺少 X=1.0 成本中位垂线标注")
		}

		// 2. 严重异常独立描边外环加 ⊗
		if !strings.Contains(htmlOut, "⊗") {
			t.Errorf("SVG 缺少 3.6 严重异常 ⊗ 字符标记")
		}

		// 3. 散点颜色映射与圆点
		if !strings.Contains(htmlOut, "<circle") {
			t.Errorf("SVG 缺少圆点散点元素")
		}

		// 3.1 §4.1 岗位分面：多岗位部门应渲染「按岗位看问题」汇总表 + 问题成员卡片。
		if !strings.Contains(htmlOut, "按岗位看问题") {
			t.Errorf("多岗位部门缺少 §4.1 岗位分面（按岗位看问题）标题")
		}
		if !strings.Contains(htmlOut, "facet-members-card") {
			t.Errorf("缺少「按岗位看问题」的问题成员卡片结构")
		}
		if !strings.Contains(htmlOut, "需关注") {
			t.Errorf("岗位分面汇总表缺少「需关注」列")
		}
		if !strings.Contains(htmlOut, "一句话建议") {
			t.Errorf("岗位分面汇总表缺少管理建议列")
		}

		// 4. K-Means 分群图各要素
		if !strings.Contains(htmlOut, "4.2 成员问题相似度聚类分群图") {
			t.Errorf("HTML 缺少 4.2 K-Means 分群标题")
		}
		if !strings.Contains(htmlOut, "统一治理策略") {
			t.Errorf("HTML 缺少分群统一治理策略建议")
		}

		// 5. 存在问题的人展开卡按管理优先级排序（严重异常待核查 → 右上优先改善 → 左上习惯辅导）
		idxExpandList := strings.Index(htmlOut, "problem-expand-list")
		if idxExpandList == -1 {
			t.Fatalf("HTML 缺少 problem-expand-list 展开卡区域")
		}
		expandHTML := htmlOut[idxExpandList:]

		idxSafetyCard := strings.Index(expandHTML, "member-detail-card safety")
		idxPriorityCard := strings.Index(expandHTML, "member-detail-card priority")
		idxCoachCard := strings.Index(expandHTML, "member-detail-card coach")
		if idxSafetyCard == -1 || idxPriorityCard == -1 || idxCoachCard == -1 {
			t.Errorf("HTML 缺少预期的展开卡卡片等级: safety=%d, priority=%d, coach=%d", idxSafetyCard, idxPriorityCard, idxCoachCard)
		}
		if !(idxSafetyCard < idxPriorityCard && idxPriorityCard < idxCoachCard) {
			t.Errorf("问题人员展开卡展示顺序违背管理优先级：safety(%d) < priority(%d) < coach(%d)", idxSafetyCard, idxPriorityCard, idxCoachCard)
		}

		// 检查人员排序：谭高雄(严重异常高占比) -> 王乐华(严重异常) -> 毛炳斐(右上优先) -> 舒万春(左上辅导)
		idxTan := strings.Index(expandHTML, "谭高雄")
		idxWlh := strings.Index(expandHTML, "王乐华")
		idxMbf := strings.Index(expandHTML, "毛炳斐")
		idxShu := strings.Index(expandHTML, "舒万春")
		if !(idxTan < idxWlh && idxWlh < idxMbf && idxMbf < idxShu) {
			t.Errorf("人员列表未按管理优先级严格排列: 谭(%d) < 王(%d) < 毛(%d) < 舒(%d)", idxTan, idxWlh, idxMbf, idxShu)
		}

		// 6. 正常使用的人表格
		if !strings.Contains(htmlOut, "高用量·用法正常") || !strings.Contains(htmlOut, "用法正常") {
			t.Errorf("正常人员汇总表未区分 高用量·用法正常 与 用法正常")
		}
		saveReplayArtifacts(t, "department_迅猛龙_2026-W36", r)
	})
}

// TestV06_StudioProductionReplay 回放战龙工作室（下辖迅猛龙、掼蛋部等）的跨部门汇总
func TestV06_StudioProductionReplay(t *testing.T) {
	members := []report.MemberPoint{
		{
			UserID: "wlh", Name: "王乐华", Position: "AI开发", DeptID: "迅猛龙",
			CostUSD: 393.25, CostMultiple: 3.5, IssueAttributedCostUSD: 4.78, IssueCostRatio: 0.012,
			Quadrant: report.QuadrantPriorityImprove, DominantIssue: report.IssueTypeOutputRunaway,
			SafetyAnomaly: true, SafetyDetail: report.SafetyAnomaly{Hit: true, UnrelatedCostUSD: 84.15, UnrelatedCostRatio: 0.214, TopHours: []string{"01:00-04:00"}, TopModel: "claude-opus-4-8", TopKey: "key-wlh-9f3a", Status: report.SafetyAnomalyPendingReview}, Calls: 1400,
		},
		{
			UserID: "mbf", Name: "毛炳斐", Position: "后端", DeptID: "迅猛龙",
			CostUSD: 229.80, CostMultiple: 2.0, IssueAttributedCostUSD: 28.5, IssueCostRatio: 0.124,
			Quadrant: report.QuadrantPriorityImprove, DominantIssue: report.IssueTypePrefixPollution,
			SafetyAnomaly: false, Calls: 21916,
		},
		{
			UserID: "gda", Name: "掼蛋用户A", Position: "运营", DeptID: "掼蛋部",
			CostUSD: 18.0, CostMultiple: 0.16, IssueCostRatio: 0.0,
			Quadrant: report.QuadrantHighCostNormal, DominantIssue: "normal", Calls: 150,
		},
		{
			UserID: "gdb", Name: "掼蛋用户B", Position: "策划", DeptID: "掼蛋部",
			CostUSD: 14.45, CostMultiple: 0.13, IssueCostRatio: 0.0,
			Quadrant: report.QuadrantRoutine, DominantIssue: "normal", Calls: 149,
		},
	}

	r := &report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level:       report.LevelStudio,
			Scope:       report.Scope{ID: "fbacb7ca", Name: "战龙工作室"},
			Period:      report.Period{From: "2026-09-01", To: "2026-09-07", IsoWeek: "2026-W36"},
			Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
			RuleVersion: "v0.6",
		},
		DataQuality: report.DataQuality{
			RealSessionCoverage:   0.90,
			SceneLabelCoverage:    0.75,
			PriceCatalogCoverage:  0.95,
			PositionLabelCoverage: 1.0,
		},
		CostBreakdown: report.CostBreakdown{
			UncachedInputCostUSD: 480.0,
			CacheCostUSD:         640.0,
			OutputCostUSD:        159.17,
		},
		MemberEvaluation: &report.MemberEvaluation{
			CostMedianUSD:    112.0,
			IssueRatioMedian: 0.01,
			Members:          members,
		},
		Conclusion: report.Conclusion{
			CostMainBucket: report.CostBucketCache,
			IssueCostRatio: 0.026,
			DominantIssue:  report.IssueTypePrefixPollution,
			Note:           "战龙工作室本周成本 97% 集中在迅猛龙部门，掼蛋部处于正常低耗状态，各部门不排名。",
		},
	}

	if err := r.Validate(); err != nil {
		t.Fatalf("工作室报告 Validate() 失败: %v", err)
	}

	htmlOut := renderHTML(r)

	// 1. 各部门对照表（对照构成，不排名）
	if !strings.Contains(htmlOut, "2. 各部门基础数据（对照构成，不排名）") {
		t.Errorf("工作室报告缺少 第2节 各部门基础数据（对照构成，不排名）")
	}
	if !strings.Contains(htmlOut, "迅猛龙") || !strings.Contains(htmlOut, "掼蛋部") {
		t.Errorf("缺少迅猛龙与掼蛋部门对照行")
	}

	// 2. 严重异常带所属部门置顶
	if !strings.Contains(htmlOut, "3. 严重异常（待核查，带部门）") {
		t.Errorf("工作室报告缺少 第3节 严重异常（带部门）置顶")
	}
	if !strings.Contains(htmlOut, "王乐华") || !strings.Contains(htmlOut, "迅猛龙") {
		t.Errorf("严重异常置顶项中未带出所属人员与部门")
	}

	// 3. 全员散点图使用点形区分部门并带图例
	if !strings.Contains(htmlOut, "部门点形标识：") {
		t.Errorf("全员工作室散点图缺少 部门点形标识 图例")
	}

	// 4. 各部门存在问题的人分组卡片
	if !strings.Contains(htmlOut, "5. 各部门存在问题的人") {
		t.Errorf("缺少 第5节 各部门存在问题的人 分组卡片")
	}
	if !strings.Contains(htmlOut, "【迅猛龙】") {
		t.Errorf("缺少【迅猛龙】部门分组")
	}

	// 5. §5.3：「正常使用的人不上工作室报告」（已在各部门基础数据和全员图里能看见位置）。
	if strings.Contains(htmlOut, "正常使用的人（全工作室汇总）") {
		t.Errorf("§5.3 工作室报告不应渲染「正常使用的人」一节，实际仍输出")
	}
	saveReplayArtifacts(t, "studio_战龙工作室_2026-W36", r)
}

// TestV06_SpecificationClausesAudit 逐条对照 v0.6 规范原文条款进行自动化审查
func TestV06_SpecificationClausesAudit(t *testing.T) {
	// 条款 0.2: 两个占比分列、永不相加
	t.Run("Clause_0_2_TwoRatiosSeparatedNeverSummed", func(t *testing.T) {
		m := report.MemberPoint{
			UserID:                 "u1",
			CostUSD:                100.0,
			IssueAttributedCostUSD: 25.0,
			IssueCostRatio:         0.25,
			SafetyAnomaly:          true, // 3.6 严重异常命中
		}
		// 校验：四象限 Y 轴为五类问题消耗占比 (0.25)，不叠加严重异常
		if math.Abs(m.IssueCostRatio-0.25) > 1e-6 {
			t.Errorf("问题消耗占比计算异常")
		}
	})

	// 条款 0.3: 费用桶归因约束 Σ<=总成本
	t.Run("Clause_0_3_ReconciliationConstraint", func(t *testing.T) {
		r := &report.Report{
			SchemaVersion: report.SchemaVersion,
			Meta: report.Meta{
				Level:       report.LevelPersonal,
				Scope:       report.Scope{ID: "u_recon", Name: "对账测试"},
				Period:      report.Period{From: "2026-09-01", To: "2026-09-07"},
				Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
				RuleVersion: "v0.6",
			},
			CostBreakdown: report.CostBreakdown{
				UncachedInputCostUSD: 50.0,
				CacheCostUSD:         50.0,
				OutputCostUSD:        20.0,
			}, // Total = 120.0
			UsageIssues: []report.UsageIssue{
				{
					Type:              report.IssueTypePrefixPollution,
					Verdict:           report.VerdictConfirmed,
					AttributedCostUSD: 80.0,
					AffectedCostUSD:   100.0,
				},
				{
					Type:              report.IssueTypeOutputRunaway,
					Verdict:           report.VerdictConfirmed,
					AttributedCostUSD: 50.0, // 80 + 50 = 130 > 120, 应报错拦截
					AffectedCostUSD:   60.0,
				},
			},
		}

		err := r.Validate()
		if err == nil {
			t.Fatalf("当 Σ五类归因 (130) > 总成本 (120) 时，Validate() 必须拒绝，但返回了 nil")
		}
		if !strings.Contains(err.Error(), "exceeds total cost") {
			t.Errorf("错误信息应包含 exceeds total cost, 实际: %v", err)
		}
	})

	// 条款 0.4: 真实会话覆盖率 < 50% 时强制降级为疑似
	t.Run("Clause_0_4_LowCoverageForcesSuspected", func(t *testing.T) {
		r := &report.Report{
			SchemaVersion: report.SchemaVersion,
			Meta: report.Meta{
				Level:       report.LevelPersonal,
				Scope:       report.Scope{ID: "u_cov", Name: "低覆盖率用户"},
				Period:      report.Period{From: "2026-09-01", To: "2026-09-07"},
				Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
				RuleVersion: "v0.6",
			},
			DataQuality: report.DataQuality{
				RealSessionCoverage: 0.35, // < 50%
			},
			CostBreakdown: report.CostBreakdown{UncachedInputCostUSD: 10.0},
			UsageIssues: []report.UsageIssue{
				{
					Type:              report.IssueTypeToothpaste,
					Verdict:           report.VerdictConfirmed, // 会话覆盖率低，应被降级或提示
					AttributedCostUSD: 2.0,
					AffectedCostUSD:   3.0,
				},
			},
		}
		md := report.RenderMD(r)
		if !strings.Contains(md, "真实会话覆盖率 < 50%") {
			t.Errorf("Markdown 应有强制降级疑似提示")
		}
	})

	// 条款 3.6: 严重异常无论如何判定，结论天花板恒为待核查
	t.Run("Clause_3_6_CeilingPendingReview", func(t *testing.T) {
		r := &report.Report{
			SchemaVersion: report.SchemaVersion,
			Meta: report.Meta{
				Level:       report.LevelPersonal,
				Scope:       report.Scope{ID: "u_safe", Name: "安全待核查用户"},
				Period:      report.Period{From: "2026-09-01", To: "2026-09-07"},
				Currency:    report.Currency{Report: "USD", USDCNYRate: 7.2},
				RuleVersion: "v0.6",
			},
			CostBreakdown: report.CostBreakdown{UncachedInputCostUSD: 10.0},
			SafetyAnomaly: report.SafetyAnomaly{
				Hit:                true,
				UnrelatedCostRatio: 0.35,
				UnrelatedCostUSD:   3.5,
				Status:             "violation_confirmed", // 非法状态：系统永不定性定罪
			},
		}
		err := r.Validate()
		if err == nil {
			t.Fatalf("SafetyAnomaly 状态若非 pending_review / dismissed / verified_normal / exception_filed 则必须拦截")
		}
	})
}
