package bedrock

import (
	"encoding/json"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/llm-gateway/internal/core"
)

func TestParamSupportForModel(t *testing.T) {
	cases := []struct {
		model       string
		temperature bool
		topP        bool
		minTokens   int32
	}{
		// OpenAI GPT 系列（含跨区域推理配置文件前缀与 provider 前缀）
		{"global.openai.gpt-5.6-sol", false, false, 16},
		{"us.openai.gpt-5.6-luna", false, false, 16},
		{"openai.gpt-6-astra", false, false, 16},
		{"bedrock/openai.gpt-5.6-terra", false, false, 16},
		{"BEDROCK/GLOBAL.OPENAI.GPT-5.6-SOL", false, false, 16},
		// xAI Grok 系列
		{"global.xai.grok-4.6", false, false, 16},
		{"us.xai.grok-4.6", false, false, 16},
		{"xai.grok-4.6", false, false, 16},
		// gpt-oss 支持 temperature / top_p，且没有 16 的下限
		{"openai.gpt-oss-120b-1:0", true, true, 1},
		{"openai.gpt-oss-20b-1:0", true, true, 1},
		{"us.openai.gpt-oss-120b-1:0", true, true, 1},
		// 其他厂商保持放开
		{"global.anthropic.claude-sonnet-4-6", true, true, 1},
		{"us.anthropic.claude-opus-4-6-v1", true, true, 1},
		{"amazon.nova-pro-v1:0", true, true, 1},
		{"deepseek.v3.2", true, true, 1},
		{"", true, true, 1},
	}

	for _, tc := range cases {
		got := paramSupportForModel(tc.model)
		if got.Temperature != tc.temperature || got.TopP != tc.topP || got.MinMaxTokens != tc.minTokens {
			t.Errorf("paramSupportForModel(%q) = %+v, want temperature=%v topP=%v minMaxTokens=%d",
				tc.model, got, tc.temperature, tc.topP, tc.minTokens)
		}
	}
}

func TestNormalizeBedrockModelID(t *testing.T) {
	cases := map[string]string{
		"global.openai.gpt-5.6-sol":          "openai.gpt-5.6-sol",
		"us.openai.gpt-6-astra":              "openai.gpt-6-astra",
		"us-gov.openai.gpt-oss-120b-1:0":     "openai.gpt-oss-120b-1:0",
		"openai.gpt-5.6-sol":                 "openai.gpt-5.6-sol",
		"anthropic.claude-sonnet-4-6":        "anthropic.claude-sonnet-4-6",
		"global.anthropic.claude-sonnet-4-6": "anthropic.claude-sonnet-4-6",
		"bedrock/global.xai.grok-4.6":        "xai.grok-4.6",
		"  GLOBAL.OpenAI.gpt-5.6-luna  ":     "openai.gpt-5.6-luna",
	}
	for in, want := range cases {
		if got := normalizeBedrockModelID(in); got != want {
			t.Errorf("normalizeBedrockModelID(%q) = %q, want %q", in, got, want)
		}
	}
}

// newTextRequest 构造一个只带一条用户文本的消息请求。
func newTextRequest(model string, temperature *float64, maxTokens *int, topP *float64) *core.ChatRequest {
	req := &core.ChatRequest{
		Model:       model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Messages: []core.Message{
			{Role: "user", Content: "hi"},
		},
	}
	if topP != nil {
		req.ExtraFields = core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"top_p": json.RawMessage("0.9"),
		})
		_ = topP
	}
	return req
}

func TestBuildConverseParts_DropsUnsupportedInferenceParams(t *testing.T) {
	temp := 0.7
	maxTokens := 8
	topP := 0.9

	p := &Provider{}

	cases := []struct {
		model           string
		wantTemperature bool
		wantTopP        bool
		wantMaxTokens   int32
	}{
		// gpt-5.6 / gpt-6 / grok：丢弃 temperature、top_p，并把 max_tokens 抬到下限
		{"global.openai.gpt-5.6-sol", false, false, 16},
		{"us.openai.gpt-6-astra", false, false, 16},
		{"global.xai.grok-4.6", false, false, 16},
		// claude / gpt-oss：原样透传
		{"global.anthropic.claude-sonnet-4-6", true, true, 8},
		{"openai.gpt-oss-120b-1:0", true, true, 8},
	}

	for _, tc := range cases {
		parts, err := p.buildConverseParts(newTextRequest(tc.model, &temp, &maxTokens, &topP))
		if err != nil {
			t.Fatalf("buildConverseParts(%q): %v", tc.model, err)
		}
		if parts.infCfg == nil {
			t.Fatalf("buildConverseParts(%q): inference config is nil", tc.model)
		}
		gotTemp := parts.infCfg.Temperature != nil
		gotTopP := parts.infCfg.TopP != nil
		if gotTemp != tc.wantTemperature {
			t.Errorf("%s: temperature present = %v, want %v", tc.model, gotTemp, tc.wantTemperature)
		}
		if gotTopP != tc.wantTopP {
			t.Errorf("%s: top_p present = %v, want %v", tc.model, gotTopP, tc.wantTopP)
		}
		if parts.infCfg.MaxTokens == nil {
			t.Fatalf("%s: max tokens is nil, want %d", tc.model, tc.wantMaxTokens)
		}
		if *parts.infCfg.MaxTokens != tc.wantMaxTokens {
			t.Errorf("%s: max tokens = %d, want %d", tc.model, *parts.infCfg.MaxTokens, tc.wantMaxTokens)
		}
	}
}

func TestBuildConverseParts_KeepsLargeMaxTokens(t *testing.T) {
	maxTokens := 4096
	p := &Provider{}

	parts, err := p.buildConverseParts(newTextRequest("global.openai.gpt-5.6-sol", nil, &maxTokens, nil))
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}
	if parts.infCfg == nil || parts.infCfg.MaxTokens == nil {
		t.Fatal("inference config / max tokens is nil")
	}
	// 高于下限的 max_tokens 不能被改写
	if *parts.infCfg.MaxTokens != 4096 {
		t.Errorf("max tokens = %d, want 4096", *parts.infCfg.MaxTokens)
	}
	// 未传 temperature / top_p 时不应凭空补上
	if parts.infCfg.Temperature != nil || parts.infCfg.TopP != nil {
		t.Errorf("temperature/top_p should stay unset when the caller did not send them")
	}
}

func TestModelSupportsPromptCaching(t *testing.T) {
	cases := map[string]bool{
		"global.anthropic.claude-sonnet-4-6":       true,
		"us.anthropic.claude-opus-4-6-v1":          true,
		"anthropic.claude-haiku-4-5-20251001-v1:0": true,
		"global.openai.gpt-5.6-sol":                false,
		"openai.gpt-oss-120b-1:0":                  false,
		"global.xai.grok-4.6":                      false,
		"deepseek.v3.2":                            false,
		"amazon.nova-2-lite-v1:0":                  false,
	}
	for in, want := range cases {
		if got := modelSupportsPromptCaching(in); got != want {
			t.Errorf("modelSupportsPromptCaching(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStripCachePoints_RemovesAllPartitions(t *testing.T) {
	cp := func() brtypes.CachePointBlock {
		return brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}
	}
	parts := &converseParts{
		system: []brtypes.SystemContentBlock{
			&brtypes.SystemContentBlockMemberText{Value: "sys"},
			&brtypes.SystemContentBlockMemberCachePoint{Value: cp()},
		},
		messages: []brtypes.Message{
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "hi"},
				&brtypes.ContentBlockMemberCachePoint{Value: cp()},
			}},
		},
		toolCfg: &brtypes.ToolConfiguration{
			Tools: []brtypes.Tool{
				&brtypes.ToolMemberToolSpec{Value: brtypes.ToolSpecification{Name: awssdk.String("t")}},
				&brtypes.ToolMemberCachePoint{Value: cp()},
			},
		},
	}

	stripCachePoints(parts)

	total := countSystemCachePoints(parts.system) + countToolCachePoints(parts.toolCfg) + countMessageCachePoints(parts.messages)
	if total != 0 {
		t.Fatalf("expected 0 CachePoints after strip, got %d", total)
	}
	// 非 CachePoint 内容不能被误删
	if len(parts.toolCfg.Tools) != 1 || len(parts.system) != 1 || len(parts.messages[0].Content) != 1 {
		t.Fatalf("stripCachePoints removed non-CachePoint blocks: tools=%d system=%d content=%d",
			len(parts.toolCfg.Tools), len(parts.system), len(parts.messages[0].Content))
	}
}

// 非 Anthropic 模型带工具时必须清掉自动注入的 CachePoint，否则上游直接 403；
// Anthropic 模型则应保留缓存断点。
func TestBuildConverseParts_CachePointsOnlyForAnthropic(t *testing.T) {
	tools := []map[string]any{{
		"type": "function",
		"function": map[string]any{
			"name":       "get_weather",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}}
	p := &Provider{}

	for _, model := range []string{"global.openai.gpt-5.6-sol", "global.xai.grok-4.6", "openai.gpt-oss-120b-1:0"} {
		parts, err := p.buildConverseParts(&core.ChatRequest{
			Model:    model,
			Messages: []core.Message{{Role: "user", Content: "hi"}},
			Tools:    tools,
		})
		if err != nil {
			t.Fatalf("buildConverseParts(%q): %v", model, err)
		}
		if n := countToolCachePoints(parts.toolCfg); n != 0 {
			t.Errorf("%s: expected no tool CachePoint, got %d", model, n)
		}
		if n := countMessageCachePoints(parts.messages); n != 0 {
			t.Errorf("%s: expected no message CachePoint, got %d", model, n)
		}
	}

	parts, err := p.buildConverseParts(&core.ChatRequest{
		Model:    "global.anthropic.claude-sonnet-4-6",
		Messages: []core.Message{{Role: "user", Content: "hi"}},
		Tools:    tools,
	})
	if err != nil {
		t.Fatalf("buildConverseParts(claude): %v", err)
	}
	if n := countToolCachePoints(parts.toolCfg); n == 0 {
		t.Errorf("claude: expected an auto-injected tool CachePoint, got none")
	}
}
