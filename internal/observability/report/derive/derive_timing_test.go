package derive_test

import (
	"math"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/internal/observability/report/derive"
)

// timingMeta 构造合法的 personal/department Meta（周期、scope、currency）。
func timingMeta(level, id, name string, scope report.Scope) report.Meta {
	return report.Meta{
		Level:    level,
		Period:   report.Period{From: "2026-08-01", To: "2026-08-07", Type: "week", Label: "week 2026-08-01~2026-08-07"},
		Scope:    scope,
		Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2, RateSource: "test", RateAt: time.Now().Format(time.RFC3339)},
		QA:       report.QA{Alignment: []string{""}, AnomalyEnabled: false},
	}
}

// TestDeriveMemberAgg_UnitTimingWeighted 验证 `-agg` 单元级：u.Member 的耗时/TTFT/上下文
// 由各成员服务端聚合字段按 req_count 加权平均装配，而非简单求和。
func TestDeriveMemberAgg_UnitTimingWeighted(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			// u1: req=100, dur=1000, p95=2000, ttft=300, ctx=50
			{UserID: "u1", UserName: "张三", DeptID: "d1", ReqCount: 100, SuccessCount: 95, ErrorCount: 5,
				PromptTokens: 1000, CachedTokens: 200, CompletionTokens: 500, ReasoningTokens: 100,
				TotalTokens: 1600, CostUSD: 5, ActiveDays: 6, ModelCount: 3,
				AvgDurationMs: 1000, P95DurationMs: 2000, AvgFirstTokenMs: 300, AvgInputCtxTokens: 50},
			// u2: req=50, dur=2000, p95=3000, ttft=400, ctx=100
			{UserID: "u2", UserName: "李四", DeptID: "d1", ReqCount: 50, SuccessCount: 48, ErrorCount: 2,
				PromptTokens: 500, CompletionTokens: 200, ReasoningTokens: 0,
				TotalTokens: 700, CostUSD: 2, ActiveDays: 4, ModelCount: 1,
				AvgDurationMs: 2000, P95DurationMs: 3000, AvgFirstTokenMs: 400, AvgInputCtxTokens: 100},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一", DeptID: "d1"},
	}
	meta := timingMeta("department", "d1", "部门一", report.Scope{Level: "department", ID: "d1", Name: "部门一", DeptID: "d1"})

	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if u == nil {
		t.Fatal("expected department fact from agg")
	}
	// 权重和 = 100+50 = 150
	// dur = (1000*100 + 2000*50)/150   = 200000/150   = 1333.333...
	// p95 = (2000*100 + 3000*50)/150   = 350000/150   = 2333.333...
	// ttft = (300*100 + 400*50)/150    = 50000/150    = 333.333...
	// ctx = (50*100 + 100*50)/150      = 10000/150    = 66.666...
	eps := 1e-6
	if math.Abs(u.Member.AvgDurationMs-1333.3333333333333) > eps {
		t.Errorf("AvgDurationMs=%v, want 1333.333... (req加权)", u.Member.AvgDurationMs)
	}
	if math.Abs(u.Member.P95DurationMs-2333.3333333333333) > eps {
		t.Errorf("P95DurationMs=%v, want 2333.333... (req加权)", u.Member.P95DurationMs)
	}
	if math.Abs(u.Member.AvgTTFTMs-333.3333333333333) > eps {
		t.Errorf("AvgTTFTMs=%v, want 333.333... (avg_first_token_ms 加权)", u.Member.AvgTTFTMs)
	}
	if math.Abs(u.Member.AvgInputCtxTokens-66.66666666666667) > eps {
		t.Errorf("AvgInputCtxTokens=%v, want 66.666... (req加权)", u.Member.AvgInputCtxTokens)
	}
}

// TestDeriveMemberAgg_PersonalTiming 验证 `-agg` 个人级：单成员时耗时/TTFT/上下文直接装配服务端字段，
// 且 AvgInputCtxTokens 优先取服务端字段而非 prompt/req 推算。
func TestDeriveMemberAgg_PersonalTiming(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", UserName: "张三", ReqCount: 100, SuccessCount: 95, ErrorCount: 5,
				PromptTokens: 10000, CachedTokens: 200, CompletionTokens: 500, ReasoningTokens: 100,
				TotalTokens: 10600, CostUSD: 5, ActiveDays: 6, ModelCount: 3,
				AvgDurationMs: 1200, P95DurationMs: 2400, AvgFirstTokenMs: 350, AvgInputCtxTokens: 80},
		},
		LevelMeta: report.Scope{Level: "personal", ID: "u1", Name: "张三"},
	}
	meta := timingMeta("personal", "u1", "张三", report.Scope{Level: "personal", ID: "u1", Name: "张三"})

	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	f := rep.Fact.Personal
	if f == nil {
		t.Fatal("expected personal fact from agg")
	}
	if f.AvgDurationMs != 1200 {
		t.Errorf("AvgDurationMs=%v, want 1200", f.AvgDurationMs)
	}
	if f.P95DurationMs != 2400 {
		t.Errorf("P95DurationMs=%v, want 2400", f.P95DurationMs)
	}
	if f.AvgTTFTMs != 350 {
		t.Errorf("AvgTTFTMs=%v, want 350", f.AvgTTFTMs)
	}
	// prompt/req = 10000/100 = 100；但服务端字段=80 应优先
	if f.AvgInputCtxTokens != 80 {
		t.Errorf("AvgInputCtxTokens=%v, want 80 (优先服务端字段)", f.AvgInputCtxTokens)
	}
}

// TestDeriveMemberAgg_ServiceKeyAssembly 验证 `-agg` 单元级：in.ServiceKeys（每 Key 一行）装配 UnitFact.ServiceKey 块，
// 数值字段跨 Key 求和、ActiveDays 取最大、OwnerName 取成本最高 Key 的 owner、ModelCount 求和近似。
func TestDeriveMemberAgg_ServiceKeyAssembly(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", UserName: "张三", DeptID: "d1", ReqCount: 100, SuccessCount: 95, ErrorCount: 5,
				PromptTokens: 1000, CompletionTokens: 500, ReasoningTokens: 100, TotalTokens: 1600, CostUSD: 5, ActiveDays: 6},
		},
		ServiceKeys: []report.ServiceKeyAgg{
			{KeyName: "程序A", OwnerName: "张三", ReqCount: 200, SuccessCount: 198, ErrorCount: 2, TotalTokens: 2800, CostUSD: 10, ModelCount: 3, ActiveDays: 6},
			{KeyName: "程序B", OwnerName: "李四", ReqCount: 100, SuccessCount: 95, ErrorCount: 5, TotalTokens: 1500, CostUSD: 15, ModelCount: 2, ActiveDays: 4},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一", DeptID: "d1"},
	}
	meta := timingMeta("department", "d1", "部门一", report.Scope{Level: "department", ID: "d1", Name: "部门一", DeptID: "d1"})

	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if u == nil {
		t.Fatal("expected department fact from agg")
	}
	sk := u.ServiceKey
	if sk.ReqCount != 300 || sk.SuccessCount != 293 || sk.ErrorCount != 7 {
		t.Errorf("ServiceKey 求和错: req=%d succ=%d err=%d, want 300/293/7", sk.ReqCount, sk.SuccessCount, sk.ErrorCount)
	}
	if sk.TotalTokens != 4300 {
		t.Errorf("ServiceKey total_tokens=%d, want 4300", sk.TotalTokens)
	}
	if sk.EstCostUSD != 25 {
		t.Errorf("ServiceKey cost=%v, want 25", sk.EstCostUSD)
	}
	if sk.ActiveDays != 6 { // max(6,4)
		t.Errorf("ServiceKey active_days=%d, want 6 (各 Key 最大)", sk.ActiveDays)
	}
	if sk.ModelCount != 5 { // 3+2
		t.Errorf("ServiceKey model_count=%d, want 5 (求和近似)", sk.ModelCount)
	}
	if sk.OwnerName != "李四" { // cost 15 > 10 → 成本最高 Key 的 owner
		t.Errorf("ServiceKey owner_name=%q, want 李四 (成本最高 Key)", sk.OwnerName)
	}
	if sk.Notes == "" {
		t.Errorf("ServiceKey notes 不应为空（已装配）")
	}
}

// TestDeriveMemberAgg_ServiceKeyEmpty 验证 `-agg` 单元级无 ServiceKeys 时：ServiceKey 块降级为 0 并标注 Notes。
func TestDeriveMemberAgg_ServiceKeyEmpty(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", UserName: "张三", DeptID: "d1", ReqCount: 100, SuccessCount: 95, ErrorCount: 5,
				PromptTokens: 1000, CompletionTokens: 500, ReasoningTokens: 100, TotalTokens: 1600, CostUSD: 5, ActiveDays: 6},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一", DeptID: "d1"},
	}
	meta := timingMeta("department", "d1", "部门一", report.Scope{Level: "department", ID: "d1", Name: "部门一", DeptID: "d1"})

	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	sk := rep.Fact.Department.ServiceKey
	if sk.ReqCount != 0 {
		t.Errorf("无 ServiceKeys 时 ServiceKey.req 应为 0, got %d", sk.ReqCount)
	}
	if sk.Notes == "" {
		t.Errorf("无 ServiceKeys 时 Notes 应标注降级原因")
	}
}

// TestDeriveDepartment_AnonymousPeerUnitCost 验证「单位活跃人成本」匿名横比：derive 按 DeptName 分组算各部门
// 单位活跃人成本（Σ成本/去重活跃人），对当前部门本值（Σ成本/活跃人数）compute 全公司分位 → judge.PeerUnitCost。
func TestDeriveDepartment_AnonymousPeerUnitCost(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	// 当前部门 d1：u1=$100 + u2=$200 → 单位活跃人成本 = 300/2 = 150
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", UserName: "张三", DeptID: "d1", ReqCount: 60, SuccessCount: 58, ErrorCount: 2,
				PromptTokens: 600, CompletionTokens: 300, ReasoningTokens: 0, TotalTokens: 900, CostUSD: 100, ActiveDays: 5},
			{UserID: "u2", UserName: "李四", DeptID: "d1", ReqCount: 40, SuccessCount: 39, ErrorCount: 1,
				PromptTokens: 400, CompletionTokens: 200, ReasoningTokens: 0, TotalTokens: 600, CostUSD: 200, ActiveDays: 4},
		},
		CompanyUserMetrics: []report.CompanyUserMetric{
			// 部门一(d1): 300/2=150
			{UserName: "张三", DeptName: "部门一", EstCostUSD: 100, CallCount: 60, ActiveDays: 5},
			{UserName: "李四", DeptName: "部门一", EstCostUSD: 200, CallCount: 40, ActiveDays: 4},
			// 产品: 300/3=100
			{UserName: "u3", DeptName: "产品", EstCostUSD: 150, CallCount: 30, ActiveDays: 3},
			{UserName: "u4", DeptName: "产品", EstCostUSD: 50, CallCount: 20, ActiveDays: 2},
			{UserName: "u5", DeptName: "产品", EstCostUSD: 100, CallCount: 10, ActiveDays: 1},
			// 运营: 60/2=30
			{UserName: "u6", DeptName: "运营", EstCostUSD: 30, CallCount: 5, ActiveDays: 2},
			{UserName: "u6b", DeptName: "运营", EstCostUSD: 30, CallCount: 5, ActiveDays: 2},
			// 市场: 200/1=200
			{UserName: "u7", DeptName: "市场", EstCostUSD: 200, CallCount: 8, ActiveDays: 1},
			// 财务: 180/2=90
			{UserName: "u8", DeptName: "财务", EstCostUSD: 90, CallCount: 6, ActiveDays: 2},
			{UserName: "u9", DeptName: "财务", EstCostUSD: 90, CallCount: 6, ActiveDays: 2},
			// 人事: 240/2=120
			{UserName: "ua", DeptName: "人事", EstCostUSD: 120, CallCount: 7, ActiveDays: 2},
			{UserName: "ub", DeptName: "人事", EstCostUSD: 120, CallCount: 7, ActiveDays: 2},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一", DeptID: "d1"},
	}
	meta := timingMeta("department", "d1", "部门一", report.Scope{Level: "department", ID: "d1", Name: "部门一", DeptID: "d1"})

	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if rep.Fact.Department == nil {
		t.Fatal("expected department fact from agg")
	}
	peer := rep.Judgment.PeerUnitCost
	if !peer.Supported {
		t.Fatalf("PeerUnitCost 应 supported, note=%q", peer.Note)
	}
	if peer.Metric != "unit_cost_per_active_user" || peer.PoolType != "company_departments" {
		t.Errorf("PeerUnitCost metric/pool=%q/%q, want unit_cost_per_active_user/company_departments", peer.Metric, peer.PoolType)
	}
	if peer.PoolN != 6 || peer.SampleN != 6 {
		t.Errorf("PeerUnitCost pool/sample=%d/%d, want 6/6", peer.PoolN, peer.SampleN)
	}
	// 当前部门单位活跃人成本 = 300/2 = 150
	if peer.Current != 150 {
		t.Errorf("PeerUnitCost.Current=%v, want 150", peer.Current)
	}
	// 池 [30,90,100,120,150,200]；rank(150)=5/6≈0.8333；P50=110
	eps := 1e-9
	if math.Abs(peer.PercentileRank-5.0/6.0) > eps {
		t.Errorf("PercentileRank=%v, want %v", peer.PercentileRank, 5.0/6.0)
	}
	if math.Abs(peer.P50-110) > eps {
		t.Errorf("P50=%v, want 110", peer.P50)
	}
}
