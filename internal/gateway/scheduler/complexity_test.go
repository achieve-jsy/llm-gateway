package scheduler

import (
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/stretchr/testify/assert"
)

func userMsg(text string) core.Message {
	return core.Message{Role: "user", Content: text}
}

func sysMsg(text string) core.Message {
	return core.Message{Role: "system", Content: text}
}

func assistantMsg(text string) core.Message {
	return core.Message{Role: "assistant", Content: text}
}

func newReq(msgs ...core.Message) *core.ChatRequest {
	return &core.ChatRequest{Messages: msgs}
}

func TestClassify_EmptyMessages(t *testing.T) {
	c := NewComplexityClassifier()
	r := c.Classify(newReq())
	assert.Equal(t, TierSimple, r.Tier)
}

func TestClassify_Greeting(t *testing.T) {
	c := NewComplexityClassifier()
	r := c.Classify(newReq(userMsg("你好")))
	assert.Equal(t, TierSimple, r.Tier, "greeting should be SIMPLE, got %+v", r)
}

func TestClassify_SimpleQuestion(t *testing.T) {
	c := NewComplexityClassifier()
	r := c.Classify(newReq(userMsg("什么是 Python")))
	assert.Equal(t, TierSimple, r.Tier, "simple question should be SIMPLE, got %+v", r)
}

func TestClassify_ReasoningMarkers_ForceReasoning(t *testing.T) {
	c := NewComplexityClassifier()
	r := c.Classify(newReq(userMsg("请逐步分析这段代码的问题")))
	assert.Equal(t, TierReasoning, r.Tier, "2+ reasoning markers should force REASONING, got %+v", r)
	assert.GreaterOrEqual(t, r.ReasoningMarkers, 2)
}

func TestClassify_ReasoningEffort_High(t *testing.T) {
	c := NewComplexityClassifier()
	req := newReq(userMsg("hello"))
	req.Reasoning = &core.Reasoning{Effort: "high"}
	r := c.Classify(req)
	assert.Equal(t, TierReasoning, r.Tier, "effort=high should force REASONING, got %+v", r)
}

func TestClassify_ReasoningEffort_Medium(t *testing.T) {
	c := NewComplexityClassifier()
	req := newReq(userMsg("hello"))
	req.Reasoning = &core.Reasoning{Effort: "medium"}
	r := c.Classify(req)
	if tierIndex(r.Tier) < tierIndex(TierComplex) {
		t.Errorf("effort=medium should result in COMPLEX or higher, got %s", r.Tier)
	}
}

func TestClassify_Tools_FloorMedium(t *testing.T) {
	c := NewComplexityClassifier()
	req := &core.ChatRequest{
		Messages: []core.Message{userMsg("hello")},
		Tools:    []map[string]any{{"type": "function"}},
	}
	r := c.Classify(req)
	if tierIndex(r.Tier) < tierIndex(TierMedium) {
		t.Errorf("tools should floor MEDIUM, got %s", r.Tier)
	}
}

func TestClassify_Tools_FloorComplex(t *testing.T) {
	c := NewComplexityClassifier()
	req := &core.ChatRequest{
		Messages: []core.Message{userMsg("hello")},
		Tools: []map[string]any{
			{"name": "t1"}, {"name": "t2"}, {"name": "t3"},
			{"name": "t4"}, {"name": "t5"},
		},
	}
	r := c.Classify(req)
	if tierIndex(r.Tier) < tierIndex(TierComplex) {
		t.Errorf("5+ tools should floor COMPLEX, got %s", r.Tier)
	}
}

func TestClassify_BreadthKeywords_FloorMedium(t *testing.T) {
	c := NewComplexityClassifier()
	r := c.Classify(newReq(userMsg("列出所有编程语言的优缺点")))
	if tierIndex(r.Tier) < tierIndex(TierMedium) {
		t.Errorf("breadth keywords should floor MEDIUM, got %s", r.Tier)
	}
}

func TestClassify_ComplexArchitecture(t *testing.T) {
	c := NewComplexityClassifier()
	text := "请帮我设计一个包含微服务和分布式架构的系统，要支持负载均衡和并发处理，并考虑加密和限流"
	r := c.Classify(newReq(userMsg(text)))
	if r.Tier == TierSimple {
		t.Errorf("architectural query should not be SIMPLE, got %s (score=%.3f, detail=%s)",
			r.Tier, r.Score, r.Detail)
	}
}

func TestClassify_FollowUp_Inherit(t *testing.T) {
	c := NewComplexityClassifier()
	complexMsg := userMsg("请帮我设计一个包含微服务和分布式架构的系统方案")
	r := c.Classify(newReq(complexMsg, assistantMsg("好的,我建议..."), userMsg("继续")))
	if tierIndex(r.Tier) < tierIndex(TierMedium) {
		t.Errorf("follow-up should inherit MEDIUM+ from previous, got %s (detail=%s)", r.Tier, r.Detail)
	}
}

func TestClassify_FollowUp_NotTriggeredForLongText(t *testing.T) {
	c := NewComplexityClassifier()
	longText := "继续" + string(make([]rune, 100))
	r := c.Classify(newReq(userMsg("你好"), assistantMsg("hi"), userMsg(longText)))
	assert.Equal(t, TierSimple, r.Tier, "long follow-up should not inherit, got %+v", r)
}

func TestClassify_SystemPrompt_Influence(t *testing.T) {
	c := NewComplexityClassifier()
	withSystem := newReq(
		sysMsg("你是一位资深架构师，请用复杂的微服务架构分析每个分布式系统案例"),
		userMsg("看看这个"),
	)
	withoutSystem := newReq(userMsg("看看这个"))

	r1 := c.Classify(withSystem)
	r2 := c.Classify(withoutSystem)
	if r1.Score <= r2.Score {
		t.Errorf("system prompt should boost score: with=%.3f without=%.3f", r1.Score, r2.Score)
	}
}

func TestClassify_SingleModel_ReturnsEarly(t *testing.T) {
	rt := NewRouteTable()
	hs := NewHealthStore(3, 3, 3, 0)
	router := NewRouter(rt, hs)
	rt.ReplaceAll(&ControlPlaneConfig{
		Groups: []GroupConfig{
			{ID: 1, Name: "g", Strategy: StrategyAuto, Enabled: true},
		},
		Models: []GroupModel{
			{ID: 1, GroupID: 1, ModelID: 10, ModelName: "only-model", ComplexityTier: "MEDIUM"},
		},
	})
	d, err := router.Route("g", newReq(userMsg("hello")))
	assert.NoError(t, err)
	assert.Equal(t, "only-model", d.ModelName)
	assert.Equal(t, "single", d.ComplexityTier)
}

func TestRouteAuto_BiasUp_WhenTierEmpty(t *testing.T) {
	rt := NewRouteTable()
	hs := NewHealthStore(3, 3, 3, 0)
	router := NewRouter(rt, hs)
	rt.ReplaceAll(&ControlPlaneConfig{
		Groups: []GroupConfig{
			{ID: 1, Name: "g", Strategy: StrategyAuto, Enabled: true},
		},
		Models: []GroupModel{
			{ID: 1, GroupID: 1, ModelID: 10, ModelName: "complex-model", ComplexityTier: "COMPLEX"},
			{ID: 2, GroupID: 1, ModelID: 11, ModelName: "reasoning-model", ComplexityTier: "REASONING"},
		},
	})

	d, err := router.Route("g", newReq(userMsg("你好")))
	assert.NoError(t, err)
	assert.True(t, d.BiasUpApplied, "bias-up should be true when SIMPLE tier has no models")
	if d.ModelName != "complex-model" && d.ModelName != "reasoning-model" {
		t.Errorf("should pick a complex or reasoning model, got %s", d.ModelName)
	}
}

func TestRouteAuto_FallbackToAll(t *testing.T) {
	rt := NewRouteTable()
	hs := NewHealthStore(3, 3, 3, 0)
	router := NewRouter(rt, hs)
	rt.ReplaceAll(&ControlPlaneConfig{
		Groups: []GroupConfig{
			{ID: 1, Name: "g", Strategy: StrategyAuto, Enabled: true},
		},
		Models: []GroupModel{
			{ID: 1, GroupID: 1, ModelID: 10, ModelName: "any-model", ComplexityTier: "REASONING"},
		},
	})

	d, err := router.Route("g", newReq(userMsg("你好")))
	assert.NoError(t, err)
	assert.Equal(t, "any-model", d.ModelName)
}

func TestRouteAuto_NormalTier(t *testing.T) {
	rt := NewRouteTable()
	hs := NewHealthStore(3, 3, 3, 0)
	router := NewRouter(rt, hs)
	rt.ReplaceAll(&ControlPlaneConfig{
		Groups: []GroupConfig{
			{ID: 1, Name: "g", Strategy: StrategyAuto, Enabled: true},
		},
		Models: []GroupModel{
			{ID: 1, GroupID: 1, ModelID: 10, ModelName: "simple-model", ComplexityTier: "SIMPLE"},
			{ID: 2, GroupID: 1, ModelID: 11, ModelName: "complex-model", ComplexityTier: "COMPLEX"},
		},
	})

	d, err := router.Route("g", newReq(userMsg("你好")))
	assert.NoError(t, err)
	assert.Equal(t, "simple-model", d.ModelName)
	assert.Equal(t, "SIMPLE", d.ComplexityTier)
	assert.False(t, d.BiasUpApplied)
}

func TestEstimateTokens(t *testing.T) {
	assert.Equal(t, 0, estimateTokens(""))
	assert.Equal(t, 1, estimateTokens("hello"))
	assert.Equal(t, 4, estimateTokens("hello world test here"))
	cjk := estimateTokens("你好世界")
	assert.Equal(t, 4, cjk, "4 CJK chars should yield 4 tokens")
}

func TestMaxTier(t *testing.T) {
	assert.Equal(t, TierReasoning, maxTier(TierSimple, TierReasoning))
	assert.Equal(t, TierComplex, maxTier(TierComplex, TierMedium))
	assert.Equal(t, TierSimple, maxTier(TierSimple, TierSimple))
}

func TestTierIndex(t *testing.T) {
	assert.Equal(t, 0, tierIndex(TierSimple))
	assert.Equal(t, 1, tierIndex(TierMedium))
	assert.Equal(t, 2, tierIndex(TierComplex))
	assert.Equal(t, 3, tierIndex(TierReasoning))
}

func TestScoreToTier(t *testing.T) {
	assert.Equal(t, TierSimple, scoreToTier(0))
	assert.Equal(t, TierSimple, scoreToTier(0.14))
	assert.Equal(t, TierMedium, scoreToTier(0.15))
	assert.Equal(t, TierMedium, scoreToTier(0.34))
	assert.Equal(t, TierComplex, scoreToTier(0.35))
	assert.Equal(t, TierComplex, scoreToTier(0.59))
	assert.Equal(t, TierReasoning, scoreToTier(0.60))
	assert.Equal(t, TierReasoning, scoreToTier(1.0))
}
