package derive

// derive_modelguidance_test.go —— 个人第五章「场景与合理用模」模型选用建议 + 省钱预估的白盒单测。
//
// 铁律：数字来自已算好的事实（PersonalFact.TopModels / HighTierCostRatio / TopModelPortrait 单位成本），
// 本函数只做确定性推算，不臆造数字；省钱预估为估算，仅当本人单位成本 > 同岗 P50 时才产出。

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestBuildModelGuidance_SaveEstimateWhenUnitCostAbovePeer(t *testing.T) {
	p := &report.PersonalFact{
		TopModel:          "qwen-max",
		ReqCount:          100,
		ModelCount:        1,
		HighTierCostRatio: 0.798,
		TopModels:         []report.TopModel{{Model: "qwen-max", Calls: 82, TotalTokens: 500000, CostUSD: 10.0}},
	}
	mp := &report.TopModelPortrait{
		Model:           "qwen-max",
		PersonCostUSD:   10.0,
		PersonCostRatio: 0.798,
		UnitCostP50:     8.0,  // 同岗 P50 更低 → 有可对齐空间
		PersonUnitCost:  20.0, // 本人 20 USD/百万Token > 8 → 省钱
		Supported:       true,
	}
	g := buildModelGuidance(p, mp)
	if !g.Supported {
		t.Fatalf("expected Supported=true, got false")
	}
	if g.TopModel != "qwen-max" {
		t.Errorf("TopModel=%q, want qwen-max", g.TopModel)
	}
	if g.TopModelCallShare != 0.82 {
		t.Errorf("TopModelCallShare=%.2f, want 0.82", g.TopModelCallShare)
	}
	if g.TopModelCostRatio != 0.798 {
		t.Errorf("TopModelCostRatio=%.3f, want 0.798", g.TopModelCostRatio)
	}
	if !g.SaveIsEstimate {
		t.Errorf("SaveIsEstimate should be true (估算)")
	}
	// save = topCostUSD * (1 - p50UnitCost / personUnitCost) = 10 * (1 - 8/20) = 10 * 0.6 = 6
	want := 10.0 * (1 - 8.0/20.0)
	if got := g.EstWeeklySaveUSD; got < 5.99 || got > 6.01 {
		t.Errorf("EstWeeklySaveUSD=%.4f, want ~%.4f", got, want)
	}
	if g.SaveBasis == "" {
		t.Errorf("SaveBasis should not be empty")
	}
	if g.Recommendation == "" {
		t.Errorf("Recommendation should not be empty")
	}
}

func TestBuildModelGuidance_NoEstimateWhenUnitCostAtOrBelowPeer(t *testing.T) {
	p := &report.PersonalFact{
		TopModel:          "qwen-max",
		ReqCount:          100,
		HighTierCostRatio: 0.5,
		TopModels:         []report.TopModel{{Model: "qwen-max", Calls: 82, TotalTokens: 500000, CostUSD: 10.0}},
	}
	mp := &report.TopModelPortrait{
		Model:           "qwen-max",
		PersonCostUSD:   10.0,
		PersonCostRatio: 0.5,
		UnitCostP50:     20.0, // 同岗 P50 不高于本人 → 无节省
		PersonUnitCost:  20.0,
		Supported:       true,
	}
	g := buildModelGuidance(p, mp)
	if !g.Supported {
		t.Errorf("Supported should be true when peer unit-cost data available (even without savings headroom)")
	}
	if g.EstWeeklySaveUSD != 0 {
		t.Errorf("EstWeeklySaveUSD=%.4f, want 0", g.EstWeeklySaveUSD)
	}
	if g.Note == "" {
		t.Errorf("Note should explain no-estimate reason")
	}
}

func TestBuildModelGuidance_PeerUnsupportedDegrades(t *testing.T) {
	p := &report.PersonalFact{
		TopModel:  "qwen-max",
		ReqCount:  100,
		TopModels: []report.TopModel{{Model: "qwen-max", Calls: 82, TotalTokens: 500000, CostUSD: 10.0}},
	}
	mp := &report.TopModelPortrait{Supported: false, Note: "样本不足"}
	g := buildModelGuidance(p, mp)
	if g.Supported {
		t.Errorf("Supported should be false when peer portrait unsupported")
	}
	if g.EstWeeklySaveUSD != 0 {
		t.Errorf("EstWeeklySaveUSD=%.4f, want 0", g.EstWeeklySaveUSD)
	}
	if g.ScenarioNote == "" {
		t.Errorf("ScenarioNote should still emit basic scenario annotation")
	}
}

func TestBuildModelGuidance_NilPersonalFact(t *testing.T) {
	g := buildModelGuidance(nil, nil)
	if g.Supported {
		t.Errorf("Supported should be false for nil PersonalFact")
	}
	if g.Note == "" {
		t.Errorf("Note should explain missing personal fact")
	}
}

// TestBuildModelGuidance_PeerAdoptionPeerAdoptionNote 同岗采用率（「高阶模型 vs 同岗多数」的可支撑核心）
// 填充到 ModelGuidance：adoption 数据来自 TopModelPortrait，非臆造；任务类型（代码补全/对话）当前数据无法推断（② 结论）。
func TestBuildModelGuidance_PeerAdoption(t *testing.T) {
	p := &report.PersonalFact{
		TopModel:          "qwen-max",
		ReqCount:          100,
		HighTierCostRatio: 0.798,
		TopModels:         []report.TopModel{{Model: "qwen-max", Calls: 82, TotalTokens: 500000, CostUSD: 10.0}},
	}
	mp := &report.TopModelPortrait{
		Model: "qwen-max", PersonCostUSD: 10.0, PersonCostRatio: 0.798,
		AdoptionCount: 3, PoolN: 34, AdoptionRate: 3.0 / 34.0,
		UnitCostP50: 8.0, PersonUnitCost: 20.0, Supported: true,
	}
	g := buildModelGuidance(p, mp)
	if g.PeerAdoptionRate != mp.AdoptionRate {
		t.Errorf("PeerAdoptionRate=%.4f, want %.4f", g.PeerAdoptionRate, mp.AdoptionRate)
	}
	if g.PeerAdoptionN != 3 || g.PeerPoolN != 34 {
		t.Errorf("adopt N/Pool = %d/%d, want 3/34", g.PeerAdoptionN, g.PeerPoolN)
	}
	if want := "采用率"; !strings.Contains(g.ScenarioNote, want) {
		t.Errorf("ScenarioNote should mention 采用率, got %q", g.ScenarioNote)
	}
}
