package scheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
)

// newVideoRouteTable 构造一个 priority 组：A（P0，支持视频）、B（P1，不支持视频）。
func newVideoRouteTable() *RouteTable {
	cfg := &ControlPlaneConfig{
		Groups: []GroupConfig{
			{ID: 1, Name: "mixed-group", Strategy: StrategyPriority, Enabled: true},
		},
		Models: []GroupModel{
			{ID: 1, GroupID: 1, ModelID: 1, ModelName: "video-capable", ChannelType: "ark", ProviderName: "ark", Priority: intPtr(0), SupportsVideoInput: true},
			{ID: 2, GroupID: 1, ModelID: 2, ModelName: "text-only", ChannelType: "openai", ProviderName: "openai", Priority: intPtr(1)},
		},
	}
	rt := NewRouteTable()
	rt.ReplaceAll(cfg)
	return rt
}

func intPtr(v int) *int { return &v }

func videoRequest() *core.ChatRequest {
	return &core.ChatRequest{
		Model: "mixed-group",
		Messages: []core.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "what happens in this video?"},
				map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://a/b.mp4"}},
			},
		}},
	}
}

func textRequest() *core.ChatRequest {
	return &core.ChatRequest{
		Model:    "mixed-group",
		Messages: []core.Message{{Role: "user", Content: "hello"}},
	}
}

// TestRouteVideoCapability 视频请求只能落在声明支持视频的成员上；纯文本请求行为不变。
func TestRouteVideoCapability(t *testing.T) {
	rt := newVideoRouteTable()
	router := NewRouter(rt, NewHealthStore(3, 3, 3, 30*time.Second))

	decision, err := router.Route("mixed-group", videoRequest())
	if err != nil {
		t.Fatalf("video route failed: %v", err)
	}
	if decision.ModelName != "video-capable" {
		t.Fatalf("video request routed to %q, want video-capable", decision.ModelName)
	}

	// 不带视频：priority 最高者胜出，行为与改动前一致（B 是 P1，A 是 P0）
	decision, err = router.Route("mixed-group", textRequest())
	if err != nil {
		t.Fatalf("text route failed: %v", err)
	}
	if decision.ModelName != "video-capable" {
		t.Fatalf("text request routed to %q, want video-capable (P0)", decision.ModelName)
	}

	// req 为 nil（embeddings/rerank 组路由）不 panic 且不过滤
	if _, err := router.Route("mixed-group", nil); err != nil {
		t.Fatalf("nil request route failed: %v", err)
	}
}

// TestRouteVideoCapabilityNoMember 组内没有任何成员声明视频能力时给出专用错误，
// 便于上层转成 video_input_not_supported（而不是笼统的"无可用模型"）。
func TestRouteVideoCapabilityNoMember(t *testing.T) {
	cfg := &ControlPlaneConfig{
		Groups: []GroupConfig{
			{ID: 2, Name: "text-only-group", Strategy: StrategyPriority, Enabled: true},
		},
		Models: []GroupModel{
			{ID: 3, GroupID: 2, ModelName: "text-only", ChannelType: "openai", ProviderName: "openai"},
		},
	}
	rt := NewRouteTable()
	rt.ReplaceAll(cfg)
	router := NewRouter(rt, NewHealthStore(3, 3, 3, 30*time.Second))

	_, err := router.Route("text-only-group", videoRequest())
	if !errors.Is(err, ErrNoVideoCapableModel) {
		t.Fatalf("err = %v, want ErrNoVideoCapableModel", err)
	}

	// 纯文本请求仍然正常
	if _, err := router.Route("text-only-group", textRequest()); err != nil {
		t.Fatalf("text request should still work: %v", err)
	}
}

// TestFailoverStaysVideoCapable failover 不能在视频请求上换来一个不支持视频的成员
// （否则视频被静默丢弃，模型凭空作答，比直接报错更糟）。
func TestFailoverStaysVideoCapable(t *testing.T) {
	rt := newVideoRouteTable()
	hs := NewHealthStore(3, 3, 3, 30*time.Second)
	router := NewRouter(rt, hs)
	fh := NewFailoverHandler(rt, hs, router)

	decision, err := router.Route("mixed-group", videoRequest())
	if err != nil {
		t.Fatalf("initial video route failed: %v", err)
	}
	if decision.ModelName != "video-capable" {
		t.Fatalf("initial route = %q, want video-capable", decision.ModelName)
	}

	// 唯一的视频成员失败后，failover 不应落到 text-only
	_, err = fh.FailoverExcluding("mixed-group", []string{decision.ModelID}, videoRequest())
	if !errors.Is(err, ErrNoVideoCapableModel) {
		t.Fatalf("video failover err = %v, want ErrNoVideoCapableModel（不得回退到不支持视频的成员）", err)
	}

	// 纯文本请求的 failover 行为不变：应落到下一个候选
	textDecision, err := router.Route("mixed-group", textRequest())
	if err != nil {
		t.Fatalf("text route failed: %v", err)
	}
	next, err := fh.FailoverExcluding("mixed-group", []string{textDecision.ModelID}, textRequest())
	if err != nil {
		t.Fatalf("text failover failed: %v", err)
	}
	if next.ModelName != "text-only" {
		t.Fatalf("text failover = %q, want text-only", next.ModelName)
	}
}
