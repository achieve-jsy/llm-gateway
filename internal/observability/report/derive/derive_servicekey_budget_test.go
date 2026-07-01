package derive

// derive_servicekey_budget_test.go —— ServiceKey 环比/主用模型装配 + 工作室预算执行装配的白盒单测。
//
// 覆盖：
//  1. assembleServiceKey：聚合层汇总环比（PrevReqCount/PrevTokens/PrevCostUSD）+ 主用模型（TopModel），
//     并逐 Key 带出环比/主用模型/成功/错误计数（Vicky「ServiceKey 环比列/主用模型列」数据源）。
//  2. buildStudioSummary：BudgetExec 有数据（Supported）原样装配；nil 输入降级 Supported=false（不可得）。
//
// 说明：本套单测只验证 derive 层的装配/降级逻辑（数据源无关），不依赖 be-eng 的 service-keys/budget-exec
// 后端接口是否可达；prev_*/top_model/budget 值由工具侧在 ReportInput/ServiceKeyAgg 中填充。

import (
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestAssembleServiceKey_FillsPrevAndTopModel(t *testing.T) {
	sk := &report.UnitService{}
	keys := []report.ServiceKeyAgg{
		{KeyName: "k1", OwnerName: "alice", ReqCount: 100, SuccessCount: 90, ErrorCount: 10, TotalTokens: 5000, CostUSD: 30, ModelCount: 3, ActiveDays: 5, PrevReqCount: 80, PrevTotalTokens: 4000, PrevCostUSD: 20, TopModel: "gpt-4o"},
		{KeyName: "k2", OwnerName: "bob", ReqCount: 50, SuccessCount: 48, ErrorCount: 2, TotalTokens: 2000, CostUSD: 10, ModelCount: 1, ActiveDays: 2, PrevReqCount: 60, PrevTotalTokens: 3000, PrevCostUSD: 12, TopModel: "claude"},
	}
	assembleServiceKey(sk, keys)

	// 聚合块：成本/调用/token 求和 + 环比求和 + TopModel 取成本最高 Key（k1）的主用模型
	if sk.ReqCount != 150 || sk.SuccessCount != 138 || sk.ErrorCount != 12 {
		t.Errorf("agg req/succ/err = %d/%d/%d, want 150/138/12", sk.ReqCount, sk.SuccessCount, sk.ErrorCount)
	}
	if sk.TotalTokens != 7000 || sk.EstCostUSD != 40 {
		t.Errorf("agg tokens/cost = %d/%v, want 7000/40", sk.TotalTokens, sk.EstCostUSD)
	}
	if sk.PrevReqCount != 140 || sk.PrevTokens != 7000 || sk.PrevCostUSD != 32 {
		t.Errorf("agg prev req/tokens/cost = %d/%d/%v, want 140/7000/32", sk.PrevReqCount, sk.PrevTokens, sk.PrevCostUSD)
	}
	if sk.TopModel != "gpt-4o" {
		t.Errorf("agg TopModel=%q, want gpt-4o (cost-highest key k1)", sk.TopModel)
	}
	if sk.OwnerName != "alice" {
		t.Errorf("agg OwnerName=%q, want alice", sk.OwnerName)
	}
	if sk.ActiveDays != 5 {
		t.Errorf("agg ActiveDays=%d, want 5 (max)", sk.ActiveDays)
	}

	// 逐 Key 明细：每行带出各自的环比/主用模型
	if len(sk.Keys) != 2 {
		t.Fatalf("len(Keys)=%d, want 2", len(sk.Keys))
	}
	if sk.Keys[0].TopModel != "gpt-4o" || sk.Keys[0].PrevCostUSD != 20 || sk.Keys[0].PrevReqCount != 80 || sk.Keys[0].PrevTokens != 4000 {
		t.Errorf("Keys[0] topmodel/prev = %q/%v/%d/%d, want gpt-4o/20/80/4000",
			sk.Keys[0].TopModel, sk.Keys[0].PrevCostUSD, sk.Keys[0].PrevReqCount, sk.Keys[0].PrevTokens)
	}
	if sk.Keys[0].SuccessCount != 90 || sk.Keys[0].ErrorCount != 10 {
		t.Errorf("Keys[0] succ/err = %d/%d, want 90/10", sk.Keys[0].SuccessCount, sk.Keys[0].ErrorCount)
	}
	if sk.Keys[1].TopModel != "claude" || sk.Keys[1].PrevCostUSD != 12 {
		t.Errorf("Keys[1] topmodel/prevcost = %q/%v, want claude/12", sk.Keys[1].TopModel, sk.Keys[1].PrevCostUSD)
	}
}

func TestAssembleServiceKey_EmptyLeavesZero(t *testing.T) {
	sk := &report.UnitService{ReqCount: 7}
	assembleServiceKey(sk, nil)
	// 传空 keys 时保持原值（调用方据此标注「不可得」），不覆盖既有字段
	if sk.ReqCount != 7 {
		t.Errorf("ReqCount=%d (empty keys must preserve original), want 7", sk.ReqCount)
	}
	if len(sk.Keys) != 0 {
		t.Errorf("len(Keys)=%d, want 0", len(sk.Keys))
	}
}

func TestBuildStudioSummary_BudgetExecSupported(t *testing.T) {
	in := report.ReportInput{
		BudgetExec: &report.BudgetExec{Supported: true, ScopeType: "studio", ScopeID: "s1", Month: "2026-08", MonthLimitUSD: 1000, ActualCostUSD: 650, Percent: 65},
	}
	uf := &report.UnitFact{}
	buildStudioSummary(uf, in, nil)

	if uf.BudgetExec == nil {
		t.Fatal("BudgetExec is nil, want non-nil")
	}
	if !uf.BudgetExec.Supported || uf.BudgetExec.MonthLimitUSD != 1000 || uf.BudgetExec.ActualCostUSD != 650 || uf.BudgetExec.Percent != 65 {
		t.Errorf("BudgetExec=%+v, want Supported + 1000/650/65", *uf.BudgetExec)
	}
}

func TestBuildStudioSummary_BudgetExecNilDefaultsUnsupported(t *testing.T) {
	uf := &report.UnitFact{}
	buildStudioSummary(uf, report.ReportInput{}, nil)

	if uf.BudgetExec == nil {
		t.Fatal("BudgetExec is nil, want non-nil default")
	}
	if uf.BudgetExec.Supported {
		t.Errorf("BudgetExec.Supported=true, want false when no data (降级不可得)")
	}
	if uf.BudgetExec.Note == "" {
		t.Errorf("BudgetExec.Note empty, want 说明不可得")
	}
}
