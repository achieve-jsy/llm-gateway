package redact

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// Responses 的 input_text part 是最容易被静默漏掉的一处：既有 ProtectContent 的
// []any 分支只认 type=="text"，input_text 会被整块跳过（fail-open）。这里钉住它。
func TestProtectResponsesInput(t *testing.T) {
	e, err := New(Config{Mode: ModeMask, RulesOnly: true, MaxInputBytes: 1 << 20})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := e.NewSession()
	defer s.Close()
	ctx := context.Background()

	in := []core.ResponsesInputElement{
		{
			Type: "message", Role: "user",
			Content: []any{map[string]any{"type": "input_text", "text": "key=" + synthToken}},
		},
		{
			Type: "function_call", CallID: "call_1", Name: "Bash",
			Arguments: `{"key":"` + synthToken + `"}`,
		},
		{
			Type: "function_call_output", CallID: "call_1",
			Output: "工具输出里也有 " + synthToken,
		},
	}

	out, err := s.ProtectResponsesInput(ctx, in)
	if err != nil {
		t.Fatalf("ProtectResponsesInput: %v", err)
	}
	got, ok := out.([]core.ResponsesInputElement)
	if !ok {
		t.Fatalf("形态被改坏了: %T", out)
	}

	msgJSON := mustJSON(t, got[0].Content)
	if strings.Contains(msgJSON, synthToken) {
		t.Fatalf("input_text 漏脱敏: %s", msgJSON)
	}
	if !strings.Contains(msgJSON, "{{") {
		t.Fatalf("未产生占位符: %s", msgJSON)
	}
	if strings.Contains(got[2].Output, synthToken) {
		t.Fatalf("function_call_output 漏脱敏: %s", got[2].Output)
	}
	// function_call 是工具协议字段：call_id/name/arguments 一律不动（改了模型会对不上号）
	if got[1].CallID != "call_1" || got[1].Name != "Bash" || !strings.Contains(got[1].Arguments, synthToken) {
		t.Fatalf("function_call 协议字段被改动: %+v", got[1])
	}

	// 纯 string 形态同样处理
	plain, err := s.ProtectResponsesInput(ctx, "token="+synthToken)
	if err != nil {
		t.Fatalf("ProtectResponsesInput(string): %v", err)
	}
	masked, _ := plain.(string)
	if !strings.Contains(masked, "{{") {
		t.Fatalf("string 形态未脱敏: %v", plain)
	}

	// 未知形态原样返回，不猜结构
	kept, err := s.ProtectResponsesInput(ctx, map[string]any{"foo": "bar"})
	if err != nil {
		t.Fatalf("ProtectResponsesInput(map): %v", err)
	}
	if m, ok := kept.(map[string]any); !ok || m["foo"] != "bar" {
		t.Fatalf("未知形态被改写: %#v", kept)
	}

	// 响应侧还原：Content 是强类型 []core.ContentPart
	ph := placeholderOf(masked)
	if ph == "" {
		t.Fatalf("没拿到占位符: %q", masked)
	}
	resp := &core.ResponsesResponse{Output: []core.ResponsesOutputItem{{
		Type: "message", Role: "assistant",
		Content: []core.ResponsesContentItem{{Type: "output_text", Text: "模型说 " + ph}},
	}}}
	s.RestoreResponses(resp)
	if got := mustJSON(t, resp); !strings.Contains(got, synthToken) || strings.Contains(got, "{{") {
		t.Fatalf("响应未还原: %s", got)
	}
}
