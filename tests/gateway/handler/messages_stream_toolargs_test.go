package handler_test

// 端到端回归：HTTP 层 /v1/messages（Anthropic 协议，stream=true）经网关转换后，客户端必须收到
// 完整的 tool_use.input。2026-10-20 现网事故就是这条链路上"首块即带全量参数"的上游被丢了参数，
// 单测（internal/gateway/handler）覆盖转换器，本用例覆盖 handler 接线 + SSE 头 + 收尾。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// incidentOrchestrator 回放 SGLang 0.5.22 deepseekv41 工具解析器的原始流式输出：
// 一次工具调用的 id / name / 全量 arguments 全部在同一个分块里。
type incidentOrchestrator struct{}

func (incidentOrchestrator) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return &core.ChatResponse{
		ID: "chatcmpl-incident", Object: "chat.completion", Model: req.Model,
		Choices: []core.Choice{{Index: 0, FinishReason: "stop",
			Message: core.ResponseMessage{Role: "assistant", Content: "ok"}}},
	}, nil
}

func (incidentOrchestrator) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	sse := "" +
		`data: {"id":"chatcmpl-incident","object":"chat.completion.chunk","model":"bf-glm-5.3-flash","choices":[{"index":0,"delta":{"role":"assistant","content":"I will run it."}}]}` + "\n\n" +
		`data: {"id":"chatcmpl-incident","object":"chat.completion.chunk","model":"bf-glm-5.3-flash","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_9071","index":0,"type":"function","function":{"name":"Bash","arguments":"{\"command\": \"git status\", \"description\": \"show tree\"}"}}]}}]}` + "\n\n" +
		`data: {"id":"chatcmpl-incident","object":"chat.completion.chunk","model":"bf-glm-5.3-flash","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	return io.NopCloser(strings.NewReader(sse)), nil
}

func TestMessagesHandler_StreamToolUseKeepsFirstChunkArguments(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"bf-glm-5.3-flash"})
	sch := &fakeMessagesScheduler{decision: &scheduler.RouteDecision{
		GroupName:    "bf-glm-5.3-flash",
		ModelID:      "1",
		ModelName:    "bf-glm-5.3-flash",
		ProviderName: "h1004",
	}}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)

	router := gin.New()
	h := handler.NewMessagesHandlerWithGroup(incidentOrchestrator{}, gd)
	router.POST("/v1/messages", h.HandleMessages)

	body := `{"model":"bf-glm-5.3-flash","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"run git status"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	out := w.Body.String()

	// 必须是 Anthropic SSE：有 message_start / content_block_start(tool_use) / content_block_stop
	require.Contains(t, out, "event: message_start")
	require.Contains(t, out, "\"type\":\"tool_use\"")

	// 收集 input_json_delta 片段并按 index 归属到 tool_use 块。
	var toolIDs []string
	partial := map[float64]string{}
	for _, block := range strings.Split(out, "\n\n") {
		var data string
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "data:") {
				data = strings.TrimSpace(line[5:])
				break
			}
		}
		if data == "" || data == "[DONE]" {
			continue
		}
		var evt map[string]any
		require.NoError(t, json.Unmarshal([]byte(data), &evt), "bad event: %s", data)
		switch evt["type"] {
		case "content_block_start":
			cb, _ := evt["content_block"].(map[string]any)
			if cb != nil && cb["type"] == "tool_use" {
				if id, _ := cb["id"].(string); id != "" {
					toolIDs = append(toolIDs, id)
				}
				idx, _ := evt["index"].(float64)
				partial[idx] = ""
			}
		case "content_block_delta":
			d, _ := evt["delta"].(map[string]any)
			if d == nil || d["type"] != "input_json_delta" {
				continue
			}
			idx, _ := evt["index"].(float64)
			p, _ := d["partial_json"].(string)
			partial[idx] += p
		}
	}

	require.Equal(t, []string{"call_9071"}, toolIDs, "必须恰好一个 tool_use 块且 id 透传")
	require.Len(t, partial, 1)
	for idx, raw := range partial {
		require.NotEmpty(t, raw, "tool_use 块(index=%v) 没有任何 input_json_delta（现网事故形态）", idx)
		var got map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &got), "参数拼接结果不是合法 JSON: %q", raw)
		assert.Equal(t, "git status", got["command"])
		assert.Equal(t, "show tree", got["description"])
	}

	// 事件序：message_delta(stop_reason=tool_use) 必须在 message_stop 之前。
	assert.Less(t, strings.Index(out, "event: message_delta"), strings.Index(out, "event: message_stop"))
	assert.Contains(t, out, `"stop_reason":"tool_use"`)
}
