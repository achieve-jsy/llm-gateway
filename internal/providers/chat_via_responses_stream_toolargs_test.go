package providers

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 本文件聚焦 Responses → Chat 流式转换里 function_call 的 arguments 下发。
//
// 背景缺陷：不同上游对 function_call.arguments 的出现位置不一致：
//   - 常规：response.output_item.added 里 arguments 为空，正文全部走
//     response.function_call_arguments.delta 分片；
//   - 首帧即完整：added 里 arguments 就已经是完整 JSON（无任何 delta）；
//   - 只在结束帧：added 为空、也没有 delta，完整参数只出现在
//     response.output_item.done 或 response.completed 的 response.output[*].arguments。
// 转换器曾把 added 帧的 arguments 硬编码成空串、且不回填 done/completed 里的参数，
// 导致后两种形态下下游 tool_call.arguments 永久为空。
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// 事件构造器：用 map + json.Marshal 生成事件，避免手写 JSON 转义出错。
// ---------------------------------------------------------------------------

// fcItem 描述一个 function_call item；Args 为 nil 表示字段缺失（而非空串）。
type fcItem struct {
	ID      string
	CallID  string
	Name    string
	Args    *string
	ArgsRaw any // 非 nil 时直接作为 arguments 的值下发（模拟对象/数组形态）
}

func (it fcItem) toMap() map[string]any {
	m := map[string]any{
		"type":    "function_call",
		"id":      it.ID,
		"call_id": it.CallID,
		"name":    it.Name,
	}
	if it.ArgsRaw != nil {
		m["arguments"] = it.ArgsRaw
	} else if it.Args != nil {
		m["arguments"] = *it.Args
	}
	return m
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	body, err := json.Marshal(v)
	require.NoError(t, err)
	return string(body)
}

// addedEvent 构造 response.output_item.added。
func addedEvent(t *testing.T, it fcItem) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"type": "response.output_item.added",
		"item": it.toMap(),
	})
}

// doneEvent 构造 response.output_item.done。
func doneEvent(t *testing.T, it fcItem) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"type": "response.output_item.done",
		"item": it.toMap(),
	})
}

// argsDeltaEvent 构造 response.function_call_arguments.delta。
func argsDeltaEvent(t *testing.T, itemID, delta string) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"type":    "response.function_call_arguments.delta",
		"item_id": itemID,
		"delta":   delta,
	})
}

// completedEvent 构造 response.completed，output 里按顺序放 function_call item。
func completedEvent(t *testing.T, respID string, items ...fcItem) string {
	t.Helper()
	output := make([]any, 0, len(items))
	for _, it := range items {
		output = append(output, it.toMap())
	}
	return mustJSON(t, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     respID,
			"model":  "test-model",
			"status": "completed",
			"output": output,
		},
	})
}

// addedEventWithIndex / doneEventWithIndex 构造带顶层 output_index 的事件，
// 用于刻画「item 既无 id 也无 call_id、只能靠 output_index 定位」的兼容实现。
func addedEventWithIndex(t *testing.T, it fcItem, outputIndex int) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"type":         "response.output_item.added",
		"output_index": outputIndex,
		"item":         it.toMap(),
	})
}

// argsDeltaEventAt 构造带 output_index 的 response.function_call_arguments.delta。
func argsDeltaEventAt(t *testing.T, itemID, delta string, outputIndex int) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"type":         "response.function_call_arguments.delta",
		"item_id":      itemID,
		"output_index": outputIndex,
		"delta":        delta,
	})
}

// argsDoneEvent 构造 response.function_call_arguments.done。
// 真实 OpenAI 只给 item_id + arguments，但部分兼容实现会额外带上 name / call_id，
// 传空字符串表示「该字段不出现」。
func argsDoneEvent(t *testing.T, itemID, callID, name string, args *string, outputIndex *int) string {
	t.Helper()
	evt := map[string]any{
		"type":    "response.function_call_arguments.done",
		"item_id": itemID,
	}
	if callID != "" {
		evt["call_id"] = callID
	}
	if name != "" {
		evt["name"] = name
	}
	if args != nil {
		evt["arguments"] = *args
	}
	if outputIndex != nil {
		evt["output_index"] = *outputIndex
	}
	return mustJSON(t, evt)
}

// ---------------------------------------------------------------------------
// 下游分块的读取与聚合
// ---------------------------------------------------------------------------

// runResponsesStream 把给定的 Responses 事件拼成 SSE 流喂给转换器，
// 读出全部下游 Chat Completions 分块。
func runResponsesStream(t *testing.T, events ...string) []map[string]any {
	t.Helper()
	var sb strings.Builder
	for _, ev := range events {
		sb.WriteString("data: ")
		sb.WriteString(ev)
		sb.WriteString("\n\n")
	}
	conv := NewResponsesToChatStreamConverter(
		io.NopCloser(strings.NewReader(sb.String())), "test-model", false)
	raw, err := io.ReadAll(conv)
	require.NoError(t, err)
	return parseChatChunks(t, string(raw))
}

// parseChatChunks 逐行解析 SSE，只保留 JSON 分块（跳过 [DONE] 与空行）。
func parseChatChunks(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk map[string]any
		require.NoError(t, json.Unmarshal([]byte(data), &chunk), "下游分块不是合法 JSON: %s", data)
		out = append(out, chunk)
	}
	return out
}

// assembledToolCall 是按 index 聚合后的下游 tool_call 视图。
type assembledToolCall struct {
	ID        string
	Name      string
	Arguments string
	ArgFrames int // 携带 arguments 字段的分块数量（用于检查是否重复下发）
}

// assembleToolCalls 按 tool_calls[].index 聚合下游分块里的 arguments（逐字节拼接），
// 同时返回最后一个非空 finish_reason。
func assembleToolCalls(t *testing.T, chunks []map[string]any) (map[int]*assembledToolCall, string) {
	t.Helper()
	calls := map[int]*assembledToolCall{}
	finishReason := ""
	for _, chunk := range chunks {
		choices, ok := chunk["choices"].([]any)
		require.True(t, ok, "分块缺少 choices: %+v", chunk)
		if len(choices) == 0 {
			continue // usage-only 分块
		}
		choice, ok := choices[0].(map[string]any)
		require.True(t, ok)
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			finishReason = fr
		}
		delta, ok := choice["delta"].(map[string]any)
		require.True(t, ok)
		rawCalls, ok := delta["tool_calls"].([]any)
		if !ok {
			continue
		}
		for _, rawCall := range rawCalls {
			call, ok := rawCall.(map[string]any)
			require.True(t, ok)
			idxFloat, ok := call["index"].(float64)
			require.True(t, ok, "tool_call 缺少 index: %+v", call)
			idx := int(idxFloat)
			if calls[idx] == nil {
				calls[idx] = &assembledToolCall{}
			}
			state := calls[idx]
			if id, ok := call["id"].(string); ok && id != "" {
				state.ID = id
			}
			fn, ok := call["function"].(map[string]any)
			if !ok {
				continue
			}
			if name, ok := fn["name"].(string); ok && name != "" {
				state.Name = name
			}
			if args, ok := fn["arguments"].(string); ok {
				state.Arguments += args
				state.ArgFrames++
			}
		}
	}
	return calls, finishReason
}

// requireArgsJSONEqual 断言拼出来的 arguments 是合法 JSON 且与 want 对象级相等。
func requireArgsJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	var gotObj, wantObj any
	require.NoError(t, json.Unmarshal([]byte(got), &gotObj), "arguments 不是合法 JSON: %q", got)
	require.NoError(t, json.Unmarshal([]byte(want), &wantObj))
	assert.Equal(t, wantObj, gotObj, "arguments 内容不一致，原始值: %q", got)
}

// ---------------------------------------------------------------------------
// 用例 A~F
// ---------------------------------------------------------------------------

// 用例 A：added 事件的 item.arguments 即完整参数，后续没有任何 delta ——
// 下游必须拿到完整参数（首帧参数不能假定为空）。
func TestResponsesStreamToolArgsInAdded(t *testing.T) {
	weather := `{"city":"上海"}`
	chunks := runResponsesStream(t,
		addedEvent(t, fcItem{ID: "fc_a", CallID: "call_a", Name: "get_weather", Args: &weather}),
		completedEvent(t, "resp_a", fcItem{ID: "fc_a", CallID: "call_a", Name: "get_weather", Args: &weather}),
	)
	calls, finishReason := assembleToolCalls(t, chunks)
	require.Contains(t, calls, 0)
	assert.Equal(t, "call_a", calls[0].ID)
	assert.Equal(t, "get_weather", calls[0].Name)
	assert.Equal(t, weather, calls[0].Arguments, "added 帧必须原样下发完整参数")
	requireArgsJSONEqual(t, calls[0].Arguments, weather)
	assert.Equal(t, 1, calls[0].ArgFrames, "completed 不能重复补发已有参数的 tool_call")
	assert.Equal(t, "tool_calls", finishReason)
}

// 用例 B（回归）：added 里 arguments 为空、参数通过 function_call_arguments.delta
// 分片下发 —— 拼接结果必须仍然完整，且 completed 里的完整参数不能重复补一帧。
func TestResponsesStreamToolArgsViaDelta(t *testing.T) {
	empty := ""
	full := `{"city":"北京"}`
	chunks := runResponsesStream(t,
		addedEvent(t, fcItem{ID: "fc_b", CallID: "call_b", Name: "get_weather", Args: &empty}),
		argsDeltaEvent(t, "fc_b", `{"city":"`),
		argsDeltaEvent(t, "fc_b", `北京"}`),
		completedEvent(t, "resp_b", fcItem{ID: "fc_b", CallID: "call_b", Name: "get_weather", Args: &full}),
	)
	calls, finishReason := assembleToolCalls(t, chunks)
	require.Contains(t, calls, 0)
	assert.Equal(t, "call_b", calls[0].ID)
	assert.Equal(t, full, calls[0].Arguments)
	requireArgsJSONEqual(t, calls[0].Arguments, full)
	assert.Equal(t, "tool_calls", finishReason)
}

// 用例 C：added 为空、全程无 delta，完整参数只出现在 response.completed 的
// response.output[*].arguments —— 必须回填，否则下游参数永久为空。
func TestResponsesStreamToolArgsOnlyInCompleted(t *testing.T) {
	empty := ""
	full := `{"path":"/tmp/a.txt"}`
	chunks := runResponsesStream(t,
		addedEvent(t, fcItem{ID: "fc_c", CallID: "call_c", Name: "read_file", Args: &empty}),
		completedEvent(t, "resp_c", fcItem{ID: "fc_c", CallID: "call_c", Name: "read_file", Args: &full}),
	)
	calls, finishReason := assembleToolCalls(t, chunks)
	require.Contains(t, calls, 0)
	assert.Equal(t, "call_c", calls[0].ID)
	assert.Equal(t, "read_file", calls[0].Name)
	assert.Equal(t, full, calls[0].Arguments, "completed 里的完整参数必须回填")
	requireArgsJSONEqual(t, calls[0].Arguments, full)
	assert.Equal(t, "tool_calls", finishReason)
}

// 用例 D：added 为空、无 delta，完整参数出现在 response.output_item.done。
func TestResponsesStreamToolArgsInOutputItemDone(t *testing.T) {
	t.Run("added 之后再 done", func(t *testing.T) {
		empty := ""
		full := `{"q":"golang"}`
		chunks := runResponsesStream(t,
			addedEvent(t, fcItem{ID: "fc_d", CallID: "call_d", Name: "search", Args: &empty}),
			doneEvent(t, fcItem{ID: "fc_d", CallID: "call_d", Name: "search", Args: &full}),
			completedEvent(t, "resp_d", fcItem{ID: "fc_d", CallID: "call_d", Name: "search", Args: &full}),
		)
		calls, finishReason := assembleToolCalls(t, chunks)
		require.Contains(t, calls, 0)
		assert.Equal(t, "call_d", calls[0].ID)
		assert.Equal(t, full, calls[0].Arguments, "output_item.done 里的完整参数必须落地")
		requireArgsJSONEqual(t, calls[0].Arguments, full)
		assert.Equal(t, "tool_calls", finishReason)
	})

	t.Run("只有 done（缺失 added 的兼容实现）", func(t *testing.T) {
		full := `{"q":"redis"}`
		chunks := runResponsesStream(t,
			doneEvent(t, fcItem{ID: "fc_d2", CallID: "call_d2", Name: "search", Args: &full}),
			completedEvent(t, "resp_d2", fcItem{ID: "fc_d2", CallID: "call_d2", Name: "search", Args: &full}),
		)
		calls, finishReason := assembleToolCalls(t, chunks)
		require.Contains(t, calls, 0)
		assert.Equal(t, "call_d2", calls[0].ID, "未 announce 的 tool_call 也要补上 id")
		assert.Equal(t, "search", calls[0].Name, "未 announce 的 tool_call 也要补上 name")
		assert.Equal(t, full, calls[0].Arguments)
		requireArgsJSONEqual(t, calls[0].Arguments, full)
		assert.Equal(t, "tool_calls", finishReason)
	})
}

// 用例 E：并行多个 function_call 交错下发，每个 index 只收到自己的参数，不串台；
// 混合「added 即完整」与「delta 分片」两种形态。
func TestResponsesStreamToolArgsParallel(t *testing.T) {
	empty := ""
	weather := `{"city":"上海"}`
	file := `{"path":"/etc/hosts"}`
	chunks := runResponsesStream(t,
		addedEvent(t, fcItem{ID: "fc_p0", CallID: "call_p0", Name: "get_weather", Args: &weather}),
		addedEvent(t, fcItem{ID: "fc_p1", CallID: "call_p1", Name: "read_file", Args: &empty}),
		argsDeltaEvent(t, "fc_p1", `{"path":"`),
		argsDeltaEvent(t, "fc_p1", `/etc/hosts"}`),
		completedEvent(t, "resp_p",
			fcItem{ID: "fc_p0", CallID: "call_p0", Name: "get_weather", Args: &weather},
			fcItem{ID: "fc_p1", CallID: "call_p1", Name: "read_file", Args: &file},
		),
	)
	calls, finishReason := assembleToolCalls(t, chunks)
	require.Len(t, calls, 2)
	require.Contains(t, calls, 0)
	require.Contains(t, calls, 1)

	assert.Equal(t, "call_p0", calls[0].ID)
	assert.Equal(t, "get_weather", calls[0].Name)
	assert.Equal(t, weather, calls[0].Arguments)

	assert.Equal(t, "call_p1", calls[1].ID)
	assert.Equal(t, "read_file", calls[1].Name)
	assert.Equal(t, file, calls[1].Arguments)

	assert.Equal(t, "tool_calls", finishReason)
}

// 用例 F：空参数工具（arguments 缺失或空串）—— 不能产生非法 JSON，
// finish_reason 仍是 tool_calls。
func TestResponsesStreamToolArgsEmpty(t *testing.T) {
	t.Run("arguments 字段缺失", func(t *testing.T) {
		chunks := runResponsesStream(t,
			addedEvent(t, fcItem{ID: "fc_e1", CallID: "call_e1", Name: "list_dir"}),
			completedEvent(t, "resp_e1", fcItem{ID: "fc_e1", CallID: "call_e1", Name: "list_dir"}),
		)
		calls, finishReason := assembleToolCalls(t, chunks)
		require.Contains(t, calls, 0)
		assert.Equal(t, "call_e1", calls[0].ID)
		assert.Equal(t, "tool_calls", finishReason)
		var args any
		require.NoError(t, json.Unmarshal([]byte(calls[0].Arguments), &args),
			"空参数工具的 arguments 必须是合法 JSON，实际: %q", calls[0].Arguments)
		assert.Equal(t, map[string]any{}, args)
	})

	t.Run("arguments 显式空串", func(t *testing.T) {
		empty := ""
		chunks := runResponsesStream(t,
			addedEvent(t, fcItem{ID: "fc_e2", CallID: "call_e2", Name: "list_dir", Args: &empty}),
			doneEvent(t, fcItem{ID: "fc_e2", CallID: "call_e2", Name: "list_dir", Args: &empty}),
			completedEvent(t, "resp_e2", fcItem{ID: "fc_e2", CallID: "call_e2", Name: "list_dir", Args: &empty}),
		)
		calls, finishReason := assembleToolCalls(t, chunks)
		require.Contains(t, calls, 0)
		assert.Equal(t, "tool_calls", finishReason)
		var args any
		require.NoError(t, json.Unmarshal([]byte(calls[0].Arguments), &args),
			"空参数工具的 arguments 必须是合法 JSON，实际: %q", calls[0].Arguments)
		assert.Equal(t, map[string]any{}, args)
	})
}

// ---------------------------------------------------------------------------
// 评审补充用例 G~K：占位 "{}"、done 事件身份、缺失 added、非字符串参数、
// 无 id/call_id 的 item（只能靠 output_index 定位）。
// ---------------------------------------------------------------------------

// 用例 G：added 里给的是仓库既有的 "{}" 占位（PlaceholderObject 约定）——
// 后续仍有 delta 时，下游不能拼成 "{}{...}"（非法 JSON）。
func TestResponsesStreamToolArgsPlaceholderObject(t *testing.T) {
	placeholder := "{}"
	full := `{"command":"ls"}`
	chunks := runResponsesStream(t,
		addedEvent(t, fcItem{ID: "fc_g", CallID: "call_g", Name: "run", Args: &placeholder}),
		argsDeltaEvent(t, "fc_g", `{"command":`),
		argsDeltaEvent(t, "fc_g", `"ls"}`),
		completedEvent(t, "resp_g", fcItem{ID: "fc_g", CallID: "call_g", Name: "run", Args: &full}),
	)
	calls, finishReason := assembleToolCalls(t, chunks)
	require.Contains(t, calls, 0)
	assert.Equal(t, "call_g", calls[0].ID)
	assert.Equal(t, full, calls[0].Arguments, "added 的 \" {}\" 占位不能拼在真参数前面")
	requireArgsJSONEqual(t, calls[0].Arguments, full)
	assert.Equal(t, "tool_calls", finishReason)
}

// 用例 G2：added/done/completed 三处都只有 "{}" 占位、没有真参数 ——
// 最终必须是合法 JSON 的空对象（且不能重复）。
func TestResponsesStreamToolArgsPlaceholderObjectOnly(t *testing.T) {
	placeholder := "{}"
	chunks := runResponsesStream(t,
		addedEvent(t, fcItem{ID: "fc_g2", CallID: "call_g2", Name: "noop", Args: &placeholder}),
		doneEvent(t, fcItem{ID: "fc_g2", CallID: "call_g2", Name: "noop", Args: &placeholder}),
		completedEvent(t, "resp_g2", fcItem{ID: "fc_g2", CallID: "call_g2", Name: "noop", Args: &placeholder}),
	)
	calls, finishReason := assembleToolCalls(t, chunks)
	require.Contains(t, calls, 0)
	assert.Equal(t, "{}", calls[0].Arguments)
	requireArgsJSONEqual(t, calls[0].Arguments, "{}")
	assert.Equal(t, "tool_calls", finishReason)
}

// 用例 H：只有 response.function_call_arguments.done（事件自带 name/call_id），
// 没有 added、没有 delta、也没有 completed —— id/name/args 必须齐全。
func TestResponsesStreamToolArgsDoneEventOnly(t *testing.T) {
	args := `{"a":1}`
	chunks := runResponsesStream(t,
		argsDoneEvent(t, "fc_h", "call_h", "run_cmd", &args, nil),
	)
	calls, _ := assembleToolCalls(t, chunks)
	require.Contains(t, calls, 0)
	assert.Equal(t, "call_h", calls[0].ID)
	assert.Equal(t, "run_cmd", calls[0].Name)
	assert.Equal(t, args, calls[0].Arguments)
	requireArgsJSONEqual(t, calls[0].Arguments, args)
}

// 用例 H2：arguments.done 不带 name/call_id（真实 OpenAI 形态），
// 身份信息由后续 response.completed 的 output 补齐，参数不能重复。
func TestResponsesStreamToolArgsDoneEventIdentityFromCompleted(t *testing.T) {
	args := `{"a":2}`
	chunks := runResponsesStream(t,
		argsDoneEvent(t, "fc_h2", "", "", &args, nil),
		completedEvent(t, "resp_h2", fcItem{ID: "fc_h2", CallID: "call_h2", Name: "run_cmd", Args: &args}),
	)
	calls, finishReason := assembleToolCalls(t, chunks)
	require.Contains(t, calls, 0)
	assert.Equal(t, "call_h2", calls[0].ID, "completed 必须补上缺失的 id")
	assert.Equal(t, "run_cmd", calls[0].Name, "completed 必须补上缺失的 name")
	assert.Equal(t, args, calls[0].Arguments, "已发过参数时不能再补一帧")
	requireArgsJSONEqual(t, calls[0].Arguments, args)
	assert.Equal(t, "tool_calls", finishReason)
}

// 用例 I：完全没有 added 事件，只有 arguments.delta —— 参数先落地，
// 下游 tool_call 的 id/name 必须由 completed 补齐（不能恒为空）。
func TestResponsesStreamToolArgsNoAddedEvent(t *testing.T) {
	full := `{"path":"/etc/hosts"}`
	chunks := runResponsesStream(t,
		argsDeltaEvent(t, "fc_i", `{"path":`),
		argsDeltaEvent(t, "fc_i", `"/etc/hosts"}`),
		completedEvent(t, "resp_i", fcItem{ID: "fc_i", CallID: "call_i", Name: "read_file", Args: &full}),
	)
	calls, finishReason := assembleToolCalls(t, chunks)
	require.Contains(t, calls, 0)
	assert.Equal(t, "call_i", calls[0].ID, "缺失 added 时 id 必须由 completed 补齐")
	assert.Equal(t, "read_file", calls[0].Name, "缺失 added 时 name 必须由 completed 补齐")
	assert.Equal(t, full, calls[0].Arguments)
	requireArgsJSONEqual(t, calls[0].Arguments, full)
	assert.Equal(t, "tool_calls", finishReason)
}

// 用例 J：item.arguments 是对象/数组（非字符串）—— 序列化分支必须生效。
func TestResponsesStreamToolArgsNonStringArguments(t *testing.T) {
	t.Run("对象", func(t *testing.T) {
		full := `{"path":"/tmp/x"}`
		chunks := runResponsesStream(t,
			addedEvent(t, fcItem{ID: "fc_j1", CallID: "call_j1", Name: "read_file", ArgsRaw: map[string]any{"path": "/tmp/x"}}),
			completedEvent(t, "resp_j1", fcItem{ID: "fc_j1", CallID: "call_j1", Name: "read_file", ArgsRaw: map[string]any{"path": "/tmp/x"}}),
		)
		calls, finishReason := assembleToolCalls(t, chunks)
		require.Contains(t, calls, 0)
		assert.Equal(t, full, calls[0].Arguments)
		requireArgsJSONEqual(t, calls[0].Arguments, full)
		assert.Equal(t, "tool_calls", finishReason)
	})

	t.Run("数组", func(t *testing.T) {
		full := `["a","b"]`
		chunks := runResponsesStream(t,
			addedEvent(t, fcItem{ID: "fc_j2", CallID: "call_j2", Name: "batch", ArgsRaw: []any{"a", "b"}}),
			completedEvent(t, "resp_j2", fcItem{ID: "fc_j2", CallID: "call_j2", Name: "batch", ArgsRaw: []any{"a", "b"}}),
		)
		calls, finishReason := assembleToolCalls(t, chunks)
		require.Contains(t, calls, 0)
		assert.Equal(t, full, calls[0].Arguments)
		requireArgsJSONEqual(t, calls[0].Arguments, full)
		assert.Equal(t, "tool_calls", finishReason)
	})
}

// 用例 K：item 既无 id 也无 call_id（只有 output_index + name）——
// added 的 name 不能整块丢掉，且参数不能串到并行的另一个 tool_call 上。
func TestResponsesStreamToolArgsNoItemID(t *testing.T) {
	noIDArgs := `{"x":1}`
	normalArgs := `{"y":2}`
	chunks := runResponsesStream(t,
		addedEventWithIndex(t, fcItem{Name: "no_id_tool"}, 0),
		addedEventWithIndex(t, fcItem{ID: "fc_k1", CallID: "call_k1", Name: "normal_tool"}, 1),
		argsDeltaEventAt(t, "", `{"x":1}`, 0),
		argsDeltaEvent(t, "fc_k1", `{"y":2}`),
		completedEvent(t, "resp_k",
			fcItem{Name: "no_id_tool", Args: &noIDArgs},
			fcItem{ID: "fc_k1", CallID: "call_k1", Name: "normal_tool", Args: &normalArgs},
		),
	)
	calls, finishReason := assembleToolCalls(t, chunks)
	require.Len(t, calls, 2)
	assert.Equal(t, "no_id_tool", calls[0].Name, "无 id/call_id 时 name 不能丢")
	assert.Equal(t, noIDArgs, calls[0].Arguments)
	assert.Equal(t, "call_k1", calls[1].ID)
	assert.Equal(t, "normal_tool", calls[1].Name)
	assert.Equal(t, normalArgs, calls[1].Arguments, "参数不能串到别的 tool_call 上")
	assert.Equal(t, "tool_calls", finishReason)
}

// TestResponsesStreamToolArgsNameArrivesLater 覆盖"added 里没有 name、name 只在
// output_item.done / completed 才出现"的兼容实现：下游 tool_call 必须最终拿到 name，
// 不能被 added 那一帧用空 name 把身份钉死。
func TestResponsesStreamToolArgsNameArrivesLater(t *testing.T) {
	args := `{"q":"x"}`
	chunks := runResponsesStream(t,
		addedEvent(t, fcItem{ID: "fc_n", CallID: "call_n"}),
		doneEvent(t, fcItem{ID: "fc_n", CallID: "call_n", Name: "late_tool", Args: &args}),
		completedEvent(t, "resp_n", fcItem{ID: "fc_n", CallID: "call_n", Name: "late_tool", Args: &args}),
	)
	calls, finishReason := assembleToolCalls(t, chunks)
	require.Len(t, calls, 1)
	assert.Equal(t, "late_tool", calls[0].Name, "name 晚到时必须补上")
	assert.Equal(t, "call_n", calls[0].ID)
	requireArgsJSONEqual(t, calls[0].Arguments, args)
	assert.Equal(t, "tool_calls", finishReason)
}

// TestResponsesStreamToolArgsSynthesizesIDWhenNoIdent 覆盖 id/call_id 全缺、只有
// output_index 可定位的兼容实现：下游 tool_call 不能拿到空 id（空 id 会破坏
// tool_use/tool_result 的配对），必须合成一个稳定 id。
func TestResponsesStreamToolArgsSynthesizesIDWhenNoIdent(t *testing.T) {
	args := `{"x":1}`
	chunks := runResponsesStream(t,
		addedEventWithIndex(t, fcItem{Name: "no_ident_tool", Args: &args}, 0),
		completedEvent(t, "resp_s", fcItem{Name: "no_ident_tool", Args: &args}),
	)
	calls, _ := assembleToolCalls(t, chunks)
	require.Len(t, calls, 1)
	assert.NotEmpty(t, calls[0].ID, "id 全缺时必须合成兜底 id")
	assert.Equal(t, "no_ident_tool", calls[0].Name)
	requireArgsJSONEqual(t, calls[0].Arguments, args)
}
