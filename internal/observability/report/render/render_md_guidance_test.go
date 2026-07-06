package render

// render_md_guidance_test.go —— 个人第五章「场景与合理用模」模型选用建议 + 省钱预估的渲染单测。
//
// 单独成文件以隔离于 render-eng 正在改动的 render_md_test.go；复用同包 test helper markdownReport。
// 断言：场景/建议/省钱预估（估算）与估算依据出现在「## 五、场景与合理用模」下，数字来自 ModelGuidance（不臆造）。

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestRenderMDPersonalModelGuidance(t *testing.T) {
	r := markdownReport("personal")
	r.Fact.Personal.TopModel = "qwen-max"
	r.Fact.Personal.ReqCount = 100
	r.Fact.Personal.ModelCount = 1
	r.Fact.Personal.HighTierCostRatio = 0.798
	r.Fact.Personal.TopModels = []report.TopModel{{Model: "qwen-max", Calls: 82, TotalTokens: 500000, CostUSD: 10.0}}
	r.Judgment.ModelGuidance = &report.ModelGuidance{
		Supported:          true,
		TopModel:           "qwen-max",
		TopModelCallShare:  0.82,
		TopModelCostRatio:  0.798,
		PersonUnitCostUSD:  20.0,
		PeerUnitCostP50USD: 8.0,
		EstWeeklySaveUSD:   6.0, // 10 * (1 - 8/20)
		SaveIsEstimate:     true,
		ScenarioNote:       "本周主用模型 `qwen-max`（占调用 82.0%），占本周成本 79.8%。",
		Recommendation:     "本周主用模型 `qwen-max`（占调用 82.0%），占本周成本 79.8%；建议对比同岗多数采用的档位，评估是否可将部分调用降级到更低档模型（灰度对比质量）。",
		SaveBasis:          "按本周主用模型成本 10.00 USD、本人单位成本 20.00 对同岗 P50 8.00 USD/百万Token 对齐估算。",
	}
	md := RenderMD(&r)
	if !strings.Contains(md, "## 五、场景与合理用模") {
		t.Fatalf("missing ## 场景与合理用模:\n%s", md)
	}
	if !strings.Contains(md, "场景：本周主用模型 `qwen-max`（占调用 82.0%），占本周成本 79.8%。") {
		t.Errorf("missing scenario note:\n%s", md)
	}
	if !strings.Contains(md, "建议：") || !strings.Contains(md, "（灰度对比质量）") {
		t.Errorf("missing recommendation:\n%s", md)
	}
	// 省钱预估 = 6 USD * 7.2 = 43.20 CNY，标注估算
	if !strings.Contains(md, "省钱预估（估算）：预计每周约省 `¥43.20`。") {
		t.Errorf("missing savings estimate:\n%s", md)
	}
	if !strings.Contains(md, "估算依据：按本周主用模型成本 10.00 USD") {
		t.Errorf("missing savings basis:\n%s", md)
	}
	if strings.Count(md, "## 五、场景与合理用模") != 1 {
		t.Errorf("## 场景与合理用模 should appear exactly once:\n%s", md)
	}
}

// TestRenderMDPersonalModelGuidanceAbsent 验证未设置 ModelGuidance 时个人第五章保持原行为，不输出场景/建议/省钱行。
func TestRenderMDPersonalModelGuidanceAbsent(t *testing.T) {
	r := markdownReport("personal")
	r.Fact.Personal.ReqCount = 100
	r.Fact.Personal.ModelCount = 2
	r.Fact.Personal.TopModels = []report.TopModel{{Model: "qwen-max", Calls: 60, CostUSD: 1.2}}
	md := RenderMD(&r)
	if !strings.Contains(md, "主用模型：`qwen-max`（占调用 60.0%，成本 `¥8.64`）。") {
		t.Errorf("missing deterministic top-model line:\n%s", md)
	}
	if strings.Contains(md, "省钱预估") {
		t.Errorf("should not render savings estimate when ModelGuidance absent:\n%s", md)
	}
	if strings.Contains(md, "估算依据：") {
		t.Errorf("should not render savings basis when ModelGuidance absent:\n%s", md)
	}
}
