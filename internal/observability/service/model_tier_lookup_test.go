package service

import (
	"testing"

	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
)

func TestResolvePosition(t *testing.T) {
	tests := []struct {
		name            string
		cat, fam, title string
		want            string
	}{
		{"category优先", "研发", "后端", "后端工程师", "研发"},
		{"无category用family", "", "后端", "后端工程师", "后端"},
		{"无category无family用title", "", "", "后端工程师", "后端工程师"},
		{"全空", "", "", "", ""},
		{"family优先于title", "研发", "", "工程师", "研发"},
	}
	for _, tc := range tests {
		if got := resolvePosition(tc.cat, tc.fam, tc.title); got != tc.want {
			t.Errorf("%s: resolvePosition(%q,%q,%q)=%q want %q", tc.name, tc.cat, tc.fam, tc.title, got, tc.want)
		}
	}
}

func TestDedupNonEmptyIDs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"空数组", []string{}, []string{}},
		{"去重保序", []string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{"剔除空白并trim", []string{" a ", "", "  ", "b"}, []string{"a", "b"}},
		{"全空白", []string{"", " "}, []string{}},
	}
	for _, tc := range tests {
		got := dedupNonEmptyIDs(tc.in)
		if !equalStrings(got, tc.want) {
			t.Errorf("%s: dedupNonEmptyIDs(%v)=%v want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestResolveTier(t *testing.T) {
	tests := []struct {
		complexity string
		want       TierLevel
	}{
		{"COMPLEX", TierHigh},
		{"REASONING", TierHigh},
		{"complex", TierHigh},
		{"SIMPLE", TierLow},
		{"simple", TierLow},
		{"MEDIUM", TierMid},
		{"medium", TierMid},
		{"", TierUnknown},
		{"BOGUS", TierUnknown},
		{"  COMPLEX  ", TierHigh},
	}
	for _, tc := range tests {
		if got := resolveTier(tc.complexity); got != tc.want {
			t.Errorf("resolveTier(%q)=%q want %q", tc.complexity, got, tc.want)
		}
	}
}

func TestTierRank(t *testing.T) {
	if tierRank(TierHigh) <= tierRank(TierMid) {
		t.Errorf("high must rank above mid")
	}
	if tierRank(TierMid) <= tierRank(TierLow) {
		t.Errorf("mid must rank above low")
	}
	if tierRank(TierLow) <= tierRank(TierUnknown) {
		t.Errorf("low must rank above unknown")
	}
	if tierRank(TierUnknown) != 0 {
		t.Errorf("unknown must rank 0")
	}
}

func TestResolvePrice_Modes(t *testing.T) {
	// price_mode=manual 优先 model 自身价（即使 catalog 绑定也不读 catalog）。
	// 未绑定 catalog 且非 manual → catalog 价 0。
	// 这里只测纯函数，不经 DB；依赖 cpmodel 构造。
	// 说明：为保持单测无需 DB 连接，构造轻量 cpmodel.Model。
	manualPrice := 5.0
	catalogIn := 8.0
	catalogOut := 9.0
	cached := 1.0
	manualModel := &cpmodel.Model{PriceMode: "manual", InputPrice: manualPrice}
	if got := resolveInputPrice(manualModel); got != manualPrice {
		t.Errorf("manual input price got %v want %v", got, manualPrice)
	}

	// 未绑定 catalog → 0
	noCatalog := &cpmodel.Model{PriceMode: "catalog"}
	if got := resolveInputPrice(noCatalog); got != 0 {
		t.Errorf("no-catalog input price got %v want 0", got)
	}

	// catalog 绑定 → catalog 价
	withCatalog := &cpmodel.Model{PriceMode: "catalog", Catalog: &cpmodel.ModelCatalog{InputPricePerMillion: catalogIn, OutputPricePerMillion: catalogOut, CacheReadPrice: &cached}}
	if got := resolveInputPrice(withCatalog); got != catalogIn {
		t.Errorf("catalog input price got %v want %v", got, catalogIn)
	}
	if got := resolveOutputPrice(withCatalog); got != catalogOut {
		t.Errorf("catalog output price got %v want %v", got, catalogOut)
	}
	if got := resolveCacheReadPrice(withCatalog); got == nil || *got != cached {
		t.Errorf("catalog cache read price got %v want %v", got, cached)
	}

	// 手动模式忽略 catalog 价
	manualWithCatalog := &cpmodel.Model{PriceMode: "manual", InputPrice: manualPrice, Catalog: &cpmodel.ModelCatalog{InputPricePerMillion: catalogIn}}
	if got := resolveInputPrice(manualWithCatalog); got != manualPrice {
		t.Errorf("manual input price should ignore catalog, got %v want %v", got, manualPrice)
	}

	// nil 安全
	if got := resolveInputPrice(nil); got != 0 {
		t.Errorf("nil input price got %v want 0", got)
	}
	if got := resolveCacheReadPrice(nil); got != nil {
		t.Errorf("nil cache read price got %v want nil", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
