package derive

// derive_anomaly_cause_test.go —— TopChangedMember.AnomalyCause 主因装配的白盒单测。
//
// 背景：derive 层（buildTopChanged）不访问 Anomalies，AnomalyRule 恒空；异常阶段产出 Anomalies 后，
// 由装配层 correlateTopChangedAnomaly 做「尽力关联」——命中异常若能定位到 Dimensions["user_id"] 才回填
// AnomalyCause，否则留空（render 主因列降级为「待确认」）。
//
// 说明：当前 anomalies.go 各规则尚未写入 Dimensions["user_id"]，因此该函数在真实 Derive 路径下仍为
// 「无 user_id 则留空」的降级；本测试直接构造带 user_id 的命中异常，验证装配层关联/回填逻辑本身正确。

import (
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestCorrelateTopChangedAnomaly_FillsByUser(t *testing.T) {
	rep := &report.Report{}
	rep.Fact.Department = &report.UnitFact{
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "工程师", WoWCostPct: 100, UserID: "u1"},
			{Idx: 2, JobCategory: "产品", WoWCostPct: -50, UserID: "u2"},
			{Idx: 3, JobCategory: "工程师", WoWCostPct: 0, UserID: "u3"},
		},
	}
	rep.Judgment.Anomalies = []report.AnomalyHit{
		{Rule: "A2", Name: "成本燃烧", Hit: true, Dimensions: map[string]string{"user_id": "u1"}},
		{Rule: "A2", Name: "成本燃烧", Hit: true, Dimensions: map[string]string{"user_id": "u1"}}, // 同 user 多次命中 → 拼接
		{Rule: "A3", Name: "重试循环", Hit: true, Dimensions: map[string]string{"user_id": "u2"}},
		{Rule: "A1", Name: "程序失控", Hit: false, Dimensions: map[string]string{"user_id": "u3"}}, // 未命中不回填
		{Rule: "A4", Name: "超长上下文", Hit: true},                                                 // 无 user_id 维度 → 尽力关联失败，留空
	}

	correlateTopChangedAnomaly(rep)

	// u1：命中 A2（首个标签），两次命中拼接
	if rep.Fact.Department.TopChanged[0].AnomalyCause != "A2 成本燃烧; A2 成本燃烧" {
		t.Errorf("TopChanged[0].AnomalyCause=%q, want %q", rep.Fact.Department.TopChanged[0].AnomalyCause, "A2 成本燃烧; A2 成本燃烧")
	}
	// u2：命中 A3
	if rep.Fact.Department.TopChanged[1].AnomalyCause != "A3 重试循环" {
		t.Errorf("TopChanged[1].AnomalyCause=%q, want %q", rep.Fact.Department.TopChanged[1].AnomalyCause, "A3 重试循环")
	}
	// u3：仅未命中异常（不回填），或命中但不带 user_id（尽力关联失败）→ 留空
	if rep.Fact.Department.TopChanged[2].AnomalyCause != "" {
		t.Errorf("TopChanged[2].AnomalyCause=%q, want empty", rep.Fact.Department.TopChanged[2].AnomalyCause)
	}
	// AnomalyRule 恒空语义保持：任意成员都不应被写入 AnomalyRule
	for _, tc := range rep.Fact.Department.TopChanged {
		if tc.AnomalyRule != "" {
			t.Errorf("TopChanged[%d].AnomalyRule=%q, want empty（AnomalyRule 应恒空）", tc.Idx, tc.AnomalyRule)
		}
	}
}

func TestCorrelateTopChangedAnomaly_NoUserIDLeavesEmpty(t *testing.T) {
	rep := &report.Report{}
	rep.Fact.Department = &report.UnitFact{
		TopChanged: []report.TopChangedMember{{Idx: 1, JobCategory: "工程师", UserID: "u1"}},
	}
	// 命中异常但均不带 user_id（当前 anomalies.go 实际情况）→ AnomalyCause 应留空
	rep.Judgment.Anomalies = []report.AnomalyHit{
		{Rule: "A2", Name: "成本燃烧", Hit: true, Dimensions: map[string]string{"key_id": "k1"}},
		{Rule: "A3", Name: "重试循环", Hit: true, Dimensions: map[string]string{"segment": "k1|m"}},
	}
	correlateTopChangedAnomaly(rep)
	if rep.Fact.Department.TopChanged[0].AnomalyCause != "" {
		t.Errorf("AnomalyCause=%q, want empty（无 user_id 应留空）", rep.Fact.Department.TopChanged[0].AnomalyCause)
	}
}

func TestCorrelateTopChangedAnomaly_NilSafety(t *testing.T) {
	correlateTopChangedAnomaly(nil) // 不应 panic
	rep := &report.Report{}
	rep.Fact.Studio = &report.UnitFact{TopChanged: []report.TopChangedMember{{Idx: 1, UserID: "s1"}}}
	rep.Judgment.Anomalies = []report.AnomalyHit{{Rule: "A5", Name: "非工作时段", Hit: true, Dimensions: map[string]string{"user_id": "s1"}}}
	correlateTopChangedAnomaly(rep)
	if rep.Fact.Studio.TopChanged[0].AnomalyCause != "A5 非工作时段" {
		t.Errorf("studio AnomalyCause=%q, want %q", rep.Fact.Studio.TopChanged[0].AnomalyCause, "A5 非工作时段")
	}
}
