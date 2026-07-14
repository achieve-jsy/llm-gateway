package main

// backfill_test.go —— ②c 数据源：AnomalyHit.Dimensions["user_name"] 回填（解锁风险清单成员列显示姓名）单测。
//
// 背景（第4轮②c）：工作室风险清单「对象」列由 render riskObject 渲染为「成员 <uuid>」。本函数用
// 成员聚合(roster)建 user_id→真实姓名 映射，把命中异常的 Dimensions["user_name"] 点亮，供渲染层显示姓名而非 UUID。
// 本测试覆盖「已即时归因(user_id) 但缺姓名」的零网络路径（A1/A2 已有 user_id、A3 等非 A1/A2 规则的命中异常），
// 以及 roster 缺名时的降级（不硬写 user_name）。窗口归因分支（A1/A2 无 user_id 需拉窗口）不做网络，不在此测。

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/tools/usage-report/client"
)

func TestBackfillAnomalyWindowUser_FillsUserName(t *testing.T) {
	userNameByID := map[string]string{
		"u-zhangsan": "张三",
		"u-lisi":     "李四",
	}
	rep := &report.Report{
		Judgment: report.Judgment{
			Anomalies: []report.AnomalyHit{
				// A3 已即时归因(user_id)但缺 user_name → 应补姓名（覆盖 A1–A3 各规则）。
				{Rule: "A3", Name: "重试循环", Severity: "P2", Hit: true, Dimensions: map[string]string{"user_id": "u-zhangsan"}},
				// A1 已即时归因(user_id)且窗口为空 → 应补姓名，但不再走窗口拉取。
				{Rule: "A1", Name: "程序失控", Severity: "P1", Hit: true, Dimensions: map[string]string{"user_id": "u-lisi"}},
				// 未命中异常不处理、不补姓名。
				{Rule: "A1", Name: "程序失控", Severity: "P1", Hit: false, Dimensions: map[string]string{"user_id": "u-zhangsan"}},
				// user_id 可定位但 roster 无此名 → 不硬写 user_name（渲染降级为 UUID）。
				{Rule: "A2", Name: "成本燃烧", Severity: "P1", Hit: true, Dimensions: map[string]string{"user_id": "u-unknown"}},
			},
		},
	}

	// cli 仅需非 nil；上述路径不触发任何 client 方法（窗口归因分支未进入）。
	backfillAnomalyWindowUser(context.Background(), &client.Client{}, Config{}, report.Scope{}, rep, userNameByID)

	if got := rep.Judgment.Anomalies[0].Dimensions["user_name"]; got != "张三" {
		t.Errorf("A3 user_name=%q, want 张三", got)
	}
	if got := rep.Judgment.Anomalies[1].Dimensions["user_name"]; got != "李四" {
		t.Errorf("A1 user_name=%q, want 李四", got)
	}
	if _, ok := rep.Judgment.Anomalies[2].Dimensions["user_name"]; ok {
		t.Error("未命中异常不应被补 user_name")
	}
	if got := rep.Judgment.Anomalies[3].Dimensions["user_name"]; got != "" {
		t.Errorf("roster 无此名时 user_name=%q, want 空（渲染降级 UUID）", got)
	}
	// user_id/user_id_basis/user_cost_share 等既有维度不被破坏。
	if rep.Judgment.Anomalies[3].Dimensions["user_id"] != "u-unknown" {
		t.Errorf("A2 user_id=%q, want u-unknown（未被破坏）", rep.Judgment.Anomalies[3].Dimensions["user_id"])
	}
}
