package derive

// derive_servicekey_topn_test.go —— Service Key TopN 成本集中度（③，derive 层）白盒单测。
//
// 验证 computeServiceKeyConcentration：活跃 Key 数 + 按成本降序取前 M 的成本占比，
// 产出「活跃 Key N 个，Top M 成本占比 X%」；活跃不足/总成本为 0 时不产出（降级）。

import (
	"math"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestComputeServiceKeyConcentration_Top2(t *testing.T) {
	sk := &report.UnitService{Keys: []report.ServiceKeyInfo{
		{KeyName: "k1", CostUSD: 100, ReqCount: 500},
		{KeyName: "k2", CostUSD: 50, ReqCount: 300},
		{KeyName: "k3", CostUSD: 30, ReqCount: 200},
		{KeyName: "k4", CostUSD: 20, ReqCount: 100},
	}}
	computeServiceKeyConcentration(sk, 2)
	if sk.ActiveKeyCount != 4 {
		t.Errorf("ActiveKeyCount=%d, want 4", sk.ActiveKeyCount)
	}
	if sk.TopN != 2 {
		t.Errorf("TopN=%d, want 2", sk.TopN)
	}
	if math.Abs(sk.TopNCostRatio-0.75) > 1e-9 {
		t.Errorf("TopNCostRatio=%v, want 0.75 (150/200)", sk.TopNCostRatio)
	}
	if sk.ConcentrationText != "活跃 Key 4 个，Top 2 成本占比 75%" {
		t.Errorf("ConcentrationText=%q, want %q", sk.ConcentrationText, "活跃 Key 4 个，Top 2 成本占比 75%")
	}
	if len(sk.TopNKeys) != 2 || sk.TopNKeys[0] != "k1" || sk.TopNKeys[1] != "k2" {
		t.Errorf("TopNKeys=%v, want [k1 k2]（按成本降序）", sk.TopNKeys)
	}
}

// 输入未按成本排序：集中度仍按成本降序取前 M（副本排序，不改 sk.Keys 顺序）。
func TestComputeServiceKeyConcentration_UnsortedInput(t *testing.T) {
	sk := &report.UnitService{Keys: []report.ServiceKeyInfo{
		{KeyName: "low", CostUSD: 10, ReqCount: 10},
		{KeyName: "high", CostUSD: 90, ReqCount: 10},
	}}
	computeServiceKeyConcentration(sk, 1)
	if sk.TopN != 1 || sk.TopNKeys[0] != "high" {
		t.Errorf("TopN=%d TopNKeys=%v, want 1 [high]", sk.TopN, sk.TopNKeys)
	}
	if math.Abs(sk.TopNCostRatio-0.9) > 1e-9 {
		t.Errorf("TopNCostRatio=%v, want 0.9", sk.TopNCostRatio)
	}
	// 不改 sk.Keys 原顺序
	if sk.Keys[0].KeyName != "low" || sk.Keys[1].KeyName != "high" {
		t.Errorf("sk.Keys order mutated: %v", sk.Keys)
	}
}

// 活跃 Key 不足 2（含 1 个有成本 + 1 个零成本零调用）→ 不产出集中度（降级）。
func TestComputeServiceKeyConcentration_NotEnoughActive(t *testing.T) {
	sk := &report.UnitService{Keys: []report.ServiceKeyInfo{
		{KeyName: "a", CostUSD: 30, ReqCount: 30},
		{KeyName: "idle", CostUSD: 0, ReqCount: 0},
	}}
	computeServiceKeyConcentration(sk, 2)
	if sk.ActiveKeyCount != 1 {
		t.Errorf("ActiveKeyCount=%d, want 1", sk.ActiveKeyCount)
	}
	if sk.ConcentrationText != "" || sk.TopNCostRatio != 0 || sk.TopN != 0 {
		t.Errorf("should be degraded, got text=%q ratio=%v topn=%d", sk.ConcentrationText, sk.TopNCostRatio, sk.TopN)
	}
}

// 空/无 Keys → nil 安全，无产出。
func TestComputeServiceKeyConcentration_Empty(t *testing.T) {
	computeServiceKeyConcentration(nil, 2)
	sk := &report.UnitService{}
	computeServiceKeyConcentration(sk, 2)
	if sk.ConcentrationText != "" || sk.ActiveKeyCount != 0 {
		t.Errorf("empty keys should not produce concentration, got text=%q active=%d", sk.ConcentrationText, sk.ActiveKeyCount)
	}
}

// m 超过活跃数：有效 topN 截断为活跃数，集中度=100%。
func TestComputeServiceKeyConcentration_TopNExceedsActive(t *testing.T) {
	sk := &report.UnitService{Keys: []report.ServiceKeyInfo{
		{KeyName: "x", CostUSD: 60, ReqCount: 60},
		{KeyName: "y", CostUSD: 40, ReqCount: 40},
	}}
	computeServiceKeyConcentration(sk, 5)
	if sk.TopN != 2 {
		t.Errorf("TopN=%d, want 2 (clamped to active count)", sk.TopN)
	}
	if math.Abs(sk.TopNCostRatio-1.0) > 1e-9 {
		t.Errorf("TopNCostRatio=%v, want 1.0", sk.TopNCostRatio)
	}
}
