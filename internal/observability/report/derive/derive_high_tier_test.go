package derive

import (
	"math"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

// personalMetricsInput 构造一组 3 模型的日聚合，用于验证「高阶模型成本占比」＝Σ高阶模型成本/总成本。
func personalMetricsInput(highTier map[string]bool) report.ReportInput {
	return report.ReportInput{
		LevelMeta: report.Scope{ID: "user_x", Name: "测试用户"},
		HighTierModelNames: highTier,
		Metrics: []report.DailyMetric{
			{Model: "high-a", Date: "2026-08-17", ReqCount: 3, TotalTokens: 600, EstCostUSD: 60},
			{Model: "mid-b", Date: "2026-08-18", ReqCount: 2, TotalTokens: 300, EstCostUSD: 30},
			{Model: "low-c", Date: "2026-08-19", ReqCount: 1, TotalTokens: 100, EstCostUSD: 10},
		},
	}
}

func almost(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestBuildPersonalFact_HighTierCostRatio_DefaultsToTop1Fallback(t *testing.T) {
	// 无高阶映射：回退旧口径「成本最高模型/总成本」＝ 60/100。
	p := buildPersonalFact(personalMetricsInput(nil))
	if !almost(p.HighTierCostRatio, 0.6) {
		t.Fatalf("无高阶映射应回退 top1 占比 0.60，实际 %v", p.HighTierCostRatio)
	}
	if p.TopModel != "high-a" {
		t.Fatalf("TopModel 应为成本最高(calls高)模型 high-a，实际 %q", p.TopModel)
	}
}

func TestBuildPersonalFact_HighTierCostRatio_SumHighTierModels(t *testing.T) {
	// 高阶={high-a, low-c}：Σ高阶成本=60+10=70，占比=0.70。
	p := buildPersonalFact(personalMetricsInput(map[string]bool{"high-a": true, "low-c": true}))
	if !almost(p.HighTierCostRatio, 0.70) {
		t.Fatalf("高阶成本占比应为 Σ高阶/总=0.70，实际 %v", p.HighTierCostRatio)
	}
}

func TestBuildPersonalFact_HighTierCostRatio_SingleHighTier(t *testing.T) {
	// 高阶={mid-b}：Σ高阶成本=30，占比=0.30。
	p := buildPersonalFact(personalMetricsInput(map[string]bool{"mid-b": true}))
	if !almost(p.HighTierCostRatio, 0.30) {
		t.Fatalf("仅 mid-b 高阶时应为 0.30，实际 %v", p.HighTierCostRatio)
	}
}

func TestBuildPersonalFact_HighTierCostRatio_NoHighTierSpend(t *testing.T) {
	// 高阶映射存在但本期无高阶模型成本：占比=0（而非回退 top1）。
	p := buildPersonalFact(personalMetricsInput(map[string]bool{"none-m": true}))
	if !almost(p.HighTierCostRatio, 0) {
		t.Fatalf("有映射但无高阶成本应为 0，实际 %v", p.HighTierCostRatio)
	}
}
