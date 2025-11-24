package scheduler

import (
	"testing"
	"time"
)

func TestProviderNamePropagation(t *testing.T) {
	cfg := &ControlPlaneConfig{
		Groups: []GroupConfig{
			{ID: 1, Name: "deepseek-v4-pro-official", Strategy: StrategyPriority, Enabled: true},
		},
		Models: []GroupModel{
			{ID: 1, GroupID: 1, ModelName: "deepseek-v4-pro", ChannelType: "deepseek", ProviderName: "deepseek"},
		},
	}

	rt := NewRouteTable()
	rt.ReplaceAll(cfg)

	group := rt.Get("deepseek-v4-pro-official")
	if group == nil {
		t.Fatal("group not found after ReplaceAll")
	}
	if len(group.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(group.Models))
	}
	if got := group.Models[0].ProviderName; got != "deepseek" {
		t.Fatalf("ModelRoutingEntry.ProviderName = %q, want %q", got, "deepseek")
	}

	// Router.Route
	hs := NewHealthStore(3, 3, 3, 30*time.Second)
	router := NewRouter(rt, hs)
	decision, err := router.Route("deepseek-v4-pro-official")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if got := decision.ProviderName; got != "deepseek" {
		t.Fatalf("RouteDecision.ProviderName = %q, want %q", got, "deepseek")
	}
	if got := decision.ModelName; got != "deepseek-v4-pro" {
		t.Fatalf("RouteDecision.ModelName = %q, want %q", got, "deepseek-v4-pro")
	}

	// 多个同模型名不同 provider 的场景
	cfg2 := &ControlPlaneConfig{
		Groups: []GroupConfig{
			{ID: 1, Name: "test-group", Strategy: StrategyPriority, Enabled: true},
		},
		Models: []GroupModel{
			{ID: 1, GroupID: 1, ModelName: "same-model", ChannelType: "deepseek", ProviderName: "deepseek"},
			{ID: 2, GroupID: 1, ModelName: "same-model", ChannelType: "dashscope", ProviderName: "阿里百炼"},
		},
	}
	rt.ReplaceAll(cfg2)

	group2 := rt.Get("test-group")
	if len(group2.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(group2.Models))
	}

	// 分别验证两个模型的 ProviderName 独立
	pNames := map[string]string{}
	for _, m := range group2.Models {
		pNames[m.ProviderName] = m.ChannelType
	}
	if pNames["deepseek"] != "deepseek" {
		t.Error("deepseek provider name not found")
	}
	if pNames["阿里百炼"] != "dashscope" {
		t.Error("阿里百炼 provider name not found")
	}

	// 路由选择后 ProviderName 应该是被选中的那个
	decision2, err := router.Route("test-group")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if decision2.ProviderName == "" {
		t.Fatal("RouteDecision.ProviderName should not be empty")
	}
	t.Logf("selected provider: %s (model: %s)", decision2.ProviderName, decision2.ModelName)
}