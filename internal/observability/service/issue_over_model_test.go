package service

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

// overModelStub 打桩 OverModelResolver。
type overModelStub struct {
	info    ModelTierInfo
	infoErr error
	alt     *LightestAlternative
	altErr  error
}

func (s overModelStub) GetModelTier(ctx context.Context, modelName string) (ModelTierInfo, error) {
	return s.info, s.infoErr
}

func (s overModelStub) GetLightestAlternative(ctx context.Context, groupID uint, excludeModelName string) (*LightestAlternative, error) {
	return s.alt, s.altErr
}

func TestEvaluateOverModel(t *testing.T) {
	params := DefaultOverModelParams()
	base := OverModelBaselines{
		PositionPromptP25:     map[string]float64{"engineer": 1000},
		PositionCompletionP50: map[string]float64{"engineer": 500},
	}

	// 命中可算溢价：turns==1 + 输入500<1000 + 输出300<500 + high 档；轻量替代在手。
	// premium = (10-2)*(500-100)/1e6 + (20-5)*300/1e6 = 0.0032+0.0045=0.0077
	hitStub := overModelStub{
		info: ModelTierInfo{ModelName: "gpt-4o", GroupID: 1, Tier: TierHigh, InputPrice: 10, OutputPrice: 20},
		alt:  &LightestAlternative{ModelName: "gpt-4o-mini", Tier: TierLow, InputPrice: 2, OutputPrice: 5},
	}
	rows := []OverModelRow{
		{RequestID: "r1", UserID: "u1", SessionID: "s1", Position: "engineer", Model: "gpt-4o",
			SessionTurns: 1, PromptTokens: 500, CachedTokens: 100, CompletionTokens: 300, ActualCostUSD: 0.05},
	}
	issue := EvaluateOverModel(context.Background(), rows, hitStub, base, params)
	if issue.Verdict != report.VerdictSuspected {
		t.Fatalf("hit: verdict = %q, want suspected", issue.Verdict)
	}
	if issue.RequestCount != 1 {
		t.Errorf("hit: request_count = %d, want 1", issue.RequestCount)
	}
	if !approx(issue.AttributedCostUSD, 0.0077) {
		t.Errorf("hit: attributed = %v, want 0.0077", issue.AttributedCostUSD)
	}
	if !approx(issue.AffectedCostUSD, 0.05) {
		t.Errorf("hit: affected = %v, want 0.05", issue.AffectedCostUSD)
	}
	if issue.Evidence["reason_suspected"] == nil {
		t.Error("hit: evidence missing reason_suspected")
	}

	// 无轻量替代 → not_computed。
	noAltStub := overModelStub{
		info: ModelTierInfo{ModelName: "gpt-4o", GroupID: 1, Tier: TierHigh, InputPrice: 10, OutputPrice: 20},
		alt:  nil,
	}
	issue2 := EvaluateOverModel(context.Background(), rows, noAltStub, base, params)
	if issue2.Verdict != report.VerdictNotComputed {
		t.Fatalf("no alt: verdict = %q, want not_computed", issue2.Verdict)
	}
	if issue2.RequestCount != 0 {
		t.Errorf("no alt: request_count = %d, want 0", issue2.RequestCount)
	}

	// 走高阶但非简单（输入>=P25）→ not_hit。
	notSimple := overModelStub{
		info: ModelTierInfo{ModelName: "gpt-4o", GroupID: 1, Tier: TierHigh, InputPrice: 10, OutputPrice: 20},
		alt:  &LightestAlternative{ModelName: "gpt-4o-mini", Tier: TierLow, InputPrice: 2, OutputPrice: 5},
	}
	rowsNotSimple := []OverModelRow{
		{RequestID: "r2", UserID: "u2", SessionID: "s2", Position: "engineer", Model: "gpt-4o",
			SessionTurns: 1, PromptTokens: 5000, CachedTokens: 0, CompletionTokens: 300, ActualCostUSD: 0.5},
	}
	issue3 := EvaluateOverModel(context.Background(), rowsNotSimple, notSimple, base, params)
	if issue3.Verdict != report.VerdictNotHit {
		t.Fatalf("not simple: verdict = %q, want not_hit", issue3.Verdict)
	}

	// 简单但非高阶（low 档）→ not_hit。
	lowTier := overModelStub{
		info: ModelTierInfo{ModelName: "gpt-4o", GroupID: 1, Tier: TierLow, InputPrice: 2, OutputPrice: 5},
		alt:  nil,
	}
	issue4 := EvaluateOverModel(context.Background(), rows, lowTier, base, params)
	if issue4.Verdict != report.VerdictNotHit {
		t.Fatalf("low tier: verdict = %q, want not_hit", issue4.Verdict)
	}

	// 档位 unknown（未关联能力组）→ 不视为高阶 → not_hit。
	unknown := overModelStub{
		info: ModelTierInfo{ModelName: "gpt-4o", Tier: TierUnknown},
	}
	issue5 := EvaluateOverModel(context.Background(), rows, unknown, base, params)
	if issue5.Verdict != report.VerdictNotHit {
		t.Fatalf("unknown tier: verdict = %q, want not_hit", issue5.Verdict)
	}

	// mul-turn 会话（turns != 1）→ 排除，不命中。
	multiTurn := overModelStub{
		info: ModelTierInfo{ModelName: "gpt-4o", GroupID: 1, Tier: TierHigh, InputPrice: 10, OutputPrice: 20},
		alt:  &LightestAlternative{ModelName: "gpt-4o-mini", Tier: TierLow, InputPrice: 2, OutputPrice: 5},
	}
	rowsMT := []OverModelRow{
		{RequestID: "r3", UserID: "u3", SessionID: "s3", Position: "engineer", Model: "gpt-4o",
			SessionTurns: 3, PromptTokens: 500, CachedTokens: 0, CompletionTokens: 300, ActualCostUSD: 0.05},
	}
	issue6 := EvaluateOverModel(context.Background(), rowsMT, multiTurn, base, params)
	if issue6.Verdict != report.VerdictNotHit {
		t.Fatalf("multi-turn: verdict = %q, want not_hit", issue6.Verdict)
	}
}
