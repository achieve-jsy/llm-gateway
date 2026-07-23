package gemini

import (
	"fmt"
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

// pairingViolations 遍历转换后的 contents，检查每个含 functionCall 的
// role:model turn 后面紧跟的 role:user turn 里，functionResponse 的数量
// 与 id/name 是否与之逐一配对。返回人类可读的违规描述。
//
// Gemini / Vertex 对该配对是强校验：数量不等或 id 不匹配都会直接返回
// 400 INVALID_ARGUMENT（"Request contains an invalid argument."）。
func pairingViolations(contents []geminiContent) []string {
	var out []string
	for i, content := range contents {
		calls := make([]*geminiFunctionCall, 0, len(content.Parts))
		for _, part := range content.Parts {
			if fc := part.functionCall(); fc != nil {
				calls = append(calls, fc)
			}
		}
		if content.Role != "model" || len(calls) == 0 {
			continue
		}
		var responses []*geminiFunctionResponse
		if i+1 < len(contents) && contents[i+1].Role == "user" {
			for _, part := range contents[i+1].Parts {
				if part.FunctionResponse != nil {
					responses = append(responses, part.FunctionResponse)
				}
			}
		}
		if len(responses) != len(calls) {
			out = append(out, fmt.Sprintf(
				"turn %d: %d functionCall(s) 但只有 %d functionResponse(s)", i, len(calls), len(responses)))
			continue
		}
		for j := range calls {
			if responses[j].ID != calls[j].ID || responses[j].Name != calls[j].Name {
				out = append(out, fmt.Sprintf(
					"turn %d part %d: functionCall(id=%q name=%q) 与 functionResponse(id=%q name=%q) 不匹配",
					i, j, calls[j].ID, calls[j].Name, responses[j].ID, responses[j].Name))
			}
		}
	}
	return out
}

// TestConvertChatRequestToGemini_MultipleToolResponseGroups 回归测试：历史上
// 存在「多组」独立的 tool 结果时，每一组都必须保持自己的 functionResponse。
//
// 曾经的缺陷：flushToolResponses 用 toolResponseParts[:0] 复位，而复位后的切片
// 与已 append 进 out.Contents 的切片共享同一底层数组，cap 保留使得下一次
// append 就地覆盖已发出的 functionResponse。结果是历史里较早的 tool 结果被最后
// 一组 tool 结果顶替，functionCall/functionResponse 配对错乱，凡是「多轮对话 +
// 多次工具调用」的 Vertex/Gemini 请求必然 400 INVALID_ARGUMENT。
func TestConvertChatRequestToGemini_MultipleToolResponseGroups(t *testing.T) {
	req := &core.ChatRequest{
		Model: "gemini-3.7-flash",
		Messages: []core.Message{
			{Role: "user", Content: "start"},
			// 第一组：2 个并行 functionCall + 2 个 functionResponse
			{Role: "assistant", ToolCalls: []core.ToolCall{
				{ID: "call_a1", Type: "function", Function: core.FunctionCall{Name: "read", Arguments: `{"path":"a"}`}},
				{ID: "call_a2", Type: "function", Function: core.FunctionCall{Name: "read", Arguments: `{"path":"b"}`}},
			}},
			{Role: "tool", ToolCallID: "call_a1", Content: `{"result":"A1"}`},
			{Role: "tool", ToolCallID: "call_a2", Content: `{"result":"A2"}`},
			{Role: "assistant", Content: "thinking"},
			// 第二组：1 个 functionCall + 1 个 functionResponse（数量与第一组不同）
			{Role: "assistant", ToolCalls: []core.ToolCall{
				{ID: "call_b1", Type: "function", Function: core.FunctionCall{Name: "write", Arguments: `{"path":"c"}`}},
			}},
			{Role: "tool", ToolCallID: "call_b1", Content: `{"result":"B1"}`},
			{Role: "assistant", Content: "done"},
		},
	}

	converted, err := convertChatRequestToGemini(req)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if violations := pairingViolations(converted.Contents); len(violations) > 0 {
		t.Fatalf("functionCall/functionResponse 配对损坏:\n  %s\ncontents=%s",
			joinViolations(violations), describeContents(converted.Contents))
	}

	// 精确断言第一组 tool 结果没有被第二组覆盖。
	var firstUser *geminiContent
	for i := range converted.Contents {
		if converted.Contents[i].Role == "user" && len(converted.Contents[i].Parts) > 0 &&
			converted.Contents[i].Parts[0].FunctionResponse != nil {
			firstUser = &converted.Contents[i]
			break
		}
	}
	if firstUser == nil {
		t.Fatalf("未找到含 functionResponse 的 user turn: %s", describeContents(converted.Contents))
	}
	if len(firstUser.Parts) != 2 {
		t.Fatalf("第一组应有 2 个 functionResponse，实得 %d: %s", len(firstUser.Parts), describeContents(converted.Contents))
	}
	for i, want := range []string{"call_a1", "call_a2"} {
		if got := firstUser.Parts[i].FunctionResponse.ID; got != want {
			t.Fatalf("第一组 part %d 的 functionResponse.id = %q，期望 %q（被后续 tool 组覆盖）: %s",
				i, got, want, describeContents(converted.Contents))
		}
	}
}

// TestConvertChatRequestToGemini_ToolResponsesShareNoBacking 直接验证 flush 之后
// 每个 user turn 的 parts 互不共享底层数组：改一个不应影响另一个。
func TestConvertChatRequestToGemini_ToolResponsesShareNoBacking(t *testing.T) {
	req := &core.ChatRequest{
		Model: "gemini-3.7-flash",
		Messages: []core.Message{
			{Role: "user", Content: "start"},
			{Role: "assistant", ToolCalls: []core.ToolCall{
				{ID: "call_1", Type: "function", Function: core.FunctionCall{Name: "f1", Arguments: `{}`}},
				{ID: "call_2", Type: "function", Function: core.FunctionCall{Name: "f2", Arguments: `{}`}},
			}},
			{Role: "tool", ToolCallID: "call_1", Content: `{"r":1}`},
			{Role: "tool", ToolCallID: "call_2", Content: `{"r":2}`},
			{Role: "assistant", Content: "next"},
			{Role: "assistant", ToolCalls: []core.ToolCall{
				{ID: "call_3", Type: "function", Function: core.FunctionCall{Name: "f3", Arguments: `{}`}},
			}},
			{Role: "tool", ToolCallID: "call_3", Content: `{"r":3}`},
		},
	}

	converted, err := convertChatRequestToGemini(req)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	ids := make([]string, 0, 2)
	for _, content := range converted.Contents {
		if content.Role == "user" && len(content.Parts) > 0 && content.Parts[0].FunctionResponse != nil {
			ids = append(ids, content.Parts[0].FunctionResponse.ID)
		}
	}
	if len(ids) != 2 || ids[0] != "call_1" || ids[1] != "call_3" {
		t.Fatalf("两组 tool 结果应分别为 call_1 / call_3，实得 %v: %s", ids, describeContents(converted.Contents))
	}
}

// TestConvertChatRequestToGemini_ReusedToolCallIDBecomesUnique 回归测试：
// 客户端跨轮复用同一个 tool_call_id 且函数名不同时，转换结果里同一个 id 不能同时
// 绑定两个函数名，对应的 functionResponse 也必须跟着改写。
//
// 真实故障：DSH 的会话里 call_53246 先用给 read、几十轮后又用给 edit。上游
// Vertex 对「同一个 functionCall.id 绑定两个不同 name」的请求直接返回
// 400 INVALID_ARGUMENT（"Request contains an invalid argument."），表现为
// 「多轮对话之后偶发报错」。上线前后用同一份真实请求体做过 6 组对照实验，
// 唯一变量就是这里的 id。
func TestConvertChatRequestToGemini_ReusedToolCallIDBecomesUnique(t *testing.T) {
	req := &core.ChatRequest{
		Model: "gemini-3.7-flash",
		Messages: []core.Message{
			{Role: "user", Content: "start"},
			{Role: "assistant", ToolCalls: []core.ToolCall{
				{ID: "call_53246", Type: "function", Function: core.FunctionCall{Name: "read", Arguments: `{}`}},
			}},
			{Role: "tool", ToolCallID: "call_53246", Content: `{"r":"read"}`},
			{Role: "user", Content: "继续"},
			{Role: "assistant", ToolCalls: []core.ToolCall{
				{ID: "call_53246", Type: "function", Function: core.FunctionCall{Name: "edit", Arguments: `{}`}},
			}},
			{Role: "tool", ToolCallID: "call_53246", Content: `{"r":"edit"}`},
		},
	}

	converted, err := convertChatRequestToGemini(req)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if v := pairingViolations(converted.Contents); len(v) > 0 {
		t.Fatalf("functionCall/functionResponse 配对错误:\n  %s\n%s", joinViolations(v), describeContents(converted.Contents))
	}

	idNames := make(map[string]map[string]bool)
	for _, content := range converted.Contents {
		for _, part := range content.Parts {
			if fc := part.functionCall(); fc != nil {
				if idNames[fc.ID] == nil {
					idNames[fc.ID] = make(map[string]bool)
				}
				idNames[fc.ID][fc.Name] = true
			}
		}
	}
	for id, names := range idNames {
		if len(names) > 1 {
			t.Fatalf("id %q 同时绑定了多个函数名 %v，上游会返回 400:\n%s", id, names, describeContents(converted.Contents))
		}
	}
	if len(idNames) != 2 {
		t.Fatalf("重用的 id 应被改写成两个不同 id，实得 %d 个: %s", len(idNames), describeContents(converted.Contents))
	}
}

func joinViolations(violations []string) string {
	out := ""
	for i, v := range violations {
		if i > 0 {
			out += "\n  "
		}
		out += v
	}
	return out
}

func describeContents(contents []geminiContent) string {
	out := ""
	for i, content := range contents {
		out += fmt.Sprintf("\n  [%d] %s parts=%d", i, content.Role, len(content.Parts))
		for _, part := range content.Parts {
			switch {
			case part.functionCall() != nil:
				out += fmt.Sprintf(" fc(%s/%s)", part.functionCall().ID, part.functionCall().Name)
			case part.FunctionResponse != nil:
				out += fmt.Sprintf(" fr(%s/%s)", part.FunctionResponse.ID, part.FunctionResponse.Name)
			default:
				out += " text"
			}
		}
	}
	return out
}
