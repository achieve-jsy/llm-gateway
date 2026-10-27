package providers_test

// Responses → Chat 流式转换的组件级回归：验证 function_call 的参数在下游 chunk 里
// 拼接后是**合法 JSON**。
//
// 背景缺陷：不同上游把完整 arguments 放在不同事件里——
//   (a) response.output_item.added 里就给全量参数（无任何 delta）；
//   (b) 传统分片：added 为空 + 多个 response.function_call_arguments.delta；
//   (c) added 里是裸 "{}" 占位（本仓库 anthropic 通道把 {} 当 PlaceholderObject），
//       真参数随后由 delta 下发。
// 修复前转换器把 added 的 arguments 硬编码成空串、且不解析 done/completed，导致 (a) 永久空参数；
// 若把占位 "{}" 原样透传，(c) 会拼成 "{}{\"command\":\"ls\"}" 这种非法 JSON。

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// streamedToolCall 是把下游 OpenAI 风格 chunk 按 index 累加后的结果。
type streamedToolCall struct {
	ID   string
	Name string
	Args string // 所有 arguments 分片拼接
}

// assembleStreamedToolCalls 解析转换器输出，按 tool_calls[].index 累加 id/name/arguments，
// 并返回最后一个非空 finish_reason。
func assembleStreamedToolCalls(t *testing.T, body string) (map[int]*streamedToolCall, string) {
	t.Helper()
	calls := map[int]*streamedToolCall{}
	finish := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		require.NoErrorf(t, json.Unmarshal([]byte(payload), &chunk), "chunk 不是合法 JSON: %s", payload)
		for _, c := range chunk.Choices {
			if c.FinishReason != "" {
				finish = c.FinishReason
			}
			for _, tc := range c.Delta.ToolCalls {
				cur := calls[tc.Index]
				if cur == nil {
					cur = &streamedToolCall{}
					calls[tc.Index] = cur
				}
				if tc.ID != "" {
					cur.ID = tc.ID
				}
				if tc.Function.Name != "" {
					cur.Name = tc.Function.Name
				}
				cur.Args += tc.Function.Arguments
			}
		}
	}
	return calls, finish
}

// runResponsesStream 把若干 SSE 行喂给转换器，返回原始输出。
func runResponsesStream(t *testing.T, lines ...string) string {
	t.Helper()
	upstream := io.NopCloser(strings.NewReader(strings.Join(lines, "\n")))
	conv := providers.NewResponsesToChatStreamConverter(upstream, "gpt-5.5", true)
	defer conv.Close()
	body, err := io.ReadAll(conv)
	require.NoError(t, err)
	return string(body)
}

// requireJSONObject 断言 raw 是合法 JSON 对象并返回解析结果。
func requireJSONObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var obj map[string]any
	require.NoErrorf(t, json.Unmarshal([]byte(raw), &obj), `arguments 不是合法 JSON 对象: %q`, raw)
	return obj
}

// TestComponentResponsesToolArgsInAdded 场景 (a)：added 里就是全量参数，没有任何 delta。
func TestComponentResponsesToolArgsInAdded(t *testing.T) {
	body := runResponsesStream(t,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"Bash","arguments":"{\"command\":\"git status\"}"}}`,
		``,
		// completed 的 output item 刻意**不带** arguments：模拟"参数只在 added 里给"的上游，
		// 这样断言才真正绑定 added 路径（否则会被 completed 回填兜住，测不出修复）。
		`data: {"type":"response.completed","response":{"id":"resp_a","model":"gpt-5.5","output":[{"type":"function_call","id":"fc_a","call_id":"call_a","name":"Bash"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		``,
		`data: [DONE]`,
	)
	calls, finish := assembleStreamedToolCalls(t, body)
	require.Len(t, calls, 1)
	call := calls[0]
	assert.Equal(t, "call_a", call.ID)
	assert.Equal(t, "Bash", call.Name)
	obj := requireJSONObject(t, call.Args)
	assert.Equal(t, "git status", obj["command"])
	assert.Equal(t, "tool_calls", finish)
}

// TestComponentResponsesToolArgsViaDelta 场景 (b)：added 为空 + 两段 delta + completed。
func TestComponentResponsesToolArgsViaDelta(t *testing.T) {
	body := runResponsesStream(t,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_b","call_id":"call_b","name":"Bash","arguments":""}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_b","output_index":0,"delta":"{\"command\":"}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_b","output_index":0,"delta":"\"ls\"}"}`,
		``,
		// 同上：参数完全由 delta 分片提供，completed 不带 arguments。
		`data: {"type":"response.completed","response":{"id":"resp_b","model":"gpt-5.5","output":[{"type":"function_call","id":"fc_b","call_id":"call_b","name":"Bash"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		``,
		`data: [DONE]`,
	)
	calls, finish := assembleStreamedToolCalls(t, body)
	require.Len(t, calls, 1)
	call := calls[0]
	assert.Equal(t, "call_b", call.ID)
	assert.Equal(t, "Bash", call.Name)
	assert.Equal(t, `{"command":"ls"}`, call.Args, "分片拼接结果必须逐字节等于原参数")
	obj := requireJSONObject(t, call.Args)
	assert.Equal(t, "ls", obj["command"])
	assert.Equal(t, "tool_calls", finish)
}

// TestComponentResponsesToolArgsPlaceholderObject 场景 (c)：added 里是裸 "{}" 占位，
// 真参数随后由 delta 下发。占位必须被丢弃，否则会拼成 `{}{"command":"ls"}`。
func TestComponentResponsesToolArgsPlaceholderObject(t *testing.T) {
	body := runResponsesStream(t,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_c","call_id":"call_c","name":"Bash","arguments":"{}"}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_c","output_index":0,"delta":"{\"command\":\"ls\"}"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_c","model":"gpt-5.5","output":[{"type":"function_call","id":"fc_c","call_id":"call_c","name":"Bash","arguments":"{\"command\":\"ls\"}"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		``,
		`data: [DONE]`,
	)
	calls, finish := assembleStreamedToolCalls(t, body)
	require.Len(t, calls, 1)
	call := calls[0]
	assert.NotContains(t, call.Args, "{}{", `占位 "{}" 不能被拼进真参数：%q`, call.Args)
	assert.Equal(t, `{"command":"ls"}`, call.Args)
	obj := requireJSONObject(t, call.Args)
	assert.Equal(t, "ls", obj["command"])
	assert.Equal(t, "tool_calls", finish)
}
