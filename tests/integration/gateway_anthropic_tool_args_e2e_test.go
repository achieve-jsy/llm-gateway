package integration

// 进程内全链路 E2E：不依赖 Docker / Postgres / Redis（make test 会跑到）。
//
// 链路（除标注外均为生产实现）：
//   真实 http.Client（流式读 SSE）
//     → gin 引擎 + handler.MessagesHandler        ← 真实
//     → orchestrator.ChatOrchestrator             ← 真实
//     → providers.Router（按 model 解析 provider）← 真实
//     → providers/openai.CompatibleProvider       ← 真实（含 SSE 转发）
//     → httptest 假上游                           ← 测试替身（回放 2026-10-20 事故 SSE）
//   客户端侧用 anthropic-sdk-go 的累加语义还原 tool_use.input。
//
// 只有两处替身：① 假上游（生产是 SGLang）；② 调度器（生产决策来自 Redis 快照）。
// 断言的是事故症状本身：input 里必须有 command，否则就是那次
// `InputValidationError: The required parameter `command` is missing`。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const e2eModelName = "bf-glm-5.3-flash"

// incidentToolArgsObject 是事故现场那一坨参数：SGLang 把 id / name / 全量 arguments 放在
// 同一个 tool_calls 元素里，且 arguments 里带空格（真实日志就是这样）。
var incidentToolArgsObject = map[string]any{
	"command":     "git status",
	"description": "show tree",
}

// upstreamChunk 是 OpenAI 流式分块（只声明本用例关心的字段）。
//
// 这里用结构体 + json.Marshal 生成 SSE 帧，而不是手写字符串：arguments 是**转义过的
// JSON 字符串**，手写极易漏反斜杠（本用例第一版就写坏了，导致上游帧非法、被转换器当成
// 分片拼到下一帧上，症状看起来像"参数丢了"——排查时浪费了一轮）。
type upstreamChunk struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Model   string           `json:"model"`
	Choices []upstreamChoice `json:"choices"`
	Usage   map[string]any   `json:"usage,omitempty"`
}

type upstreamChoice struct {
	Index        int            `json:"index"`
	Delta        map[string]any `json:"delta"`
	FinishReason string         `json:"finish_reason,omitempty"`
}

// incidentUpstreamSSE 逐帧回放 SGLang 0.5.22 deepseekv41 解析器的流式输出：
// 文本一帧 → 工具调用（id/name/全量参数同帧）→ finish_reason → 末尾 usage。
func incidentUpstreamSSE(t *testing.T) []string {
	t.Helper()

	rawArgs, err := json.Marshal(incidentToolArgsObject)
	require.NoError(t, err)

	frames := []upstreamChunk{
		{
			ID: "chatcmpl-e2e", Object: "chat.completion.chunk", Model: e2eModelName,
			Choices: []upstreamChoice{{Index: 0, Delta: map[string]any{
				"role": "assistant", "content": "I will run it.",
			}}},
		},
		{
			ID: "chatcmpl-e2e", Object: "chat.completion.chunk", Model: e2eModelName,
			Choices: []upstreamChoice{{Index: 0, Delta: map[string]any{
				"tool_calls": []any{map[string]any{
					"id": "call_sglang_1", "index": 0, "type": "function",
					"function": map[string]any{
						"name": "Bash",
						// 关键：id、name、**全量 arguments** 都在这一帧里。
						"arguments": string(rawArgs),
					},
				}},
			}}},
		},
		{
			ID: "chatcmpl-e2e", Object: "chat.completion.chunk", Model: e2eModelName,
			Choices: []upstreamChoice{{Index: 0, Delta: map[string]any{}, FinishReason: "tool_calls"}},
		},
		{
			ID: "chatcmpl-e2e", Object: "chat.completion.chunk", Model: e2eModelName,
			Choices: []upstreamChoice{},
			Usage:   map[string]any{"prompt_tokens": 42, "completion_tokens": 17, "total_tokens": 59},
		},
	}

	out := make([]string, 0, len(frames))
	for _, frame := range frames {
		encoded, err := json.Marshal(frame)
		require.NoError(t, err)
		out = append(out, string(encoded))
	}
	return out
}

// e2eScheduler 只做路由决定（生产里这一步查 Redis 快照 + 健康过滤）。
type e2eScheduler struct{ decision *scheduler.RouteDecision }

func (s *e2eScheduler) Route(_ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return s.decision, nil
}

func (s *e2eScheduler) Failover(_, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, model.ErrNoOtherModelAvailable
}

func (s *e2eScheduler) FailoverExcluding(_ string, _ []string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, model.ErrNoOtherModelAvailable
}

func (s *e2eScheduler) GetGroupConfig(_ string) *scheduler.GroupRoutingConfig { return nil }

func (s *e2eScheduler) NotifyFailure(_ string) {}

func (s *e2eScheduler) SetCooldown(_ string, _ time.Duration) {}

func (s *e2eScheduler) PersistSticky(_ context.Context, _, _ string, _ *scheduler.RouteDecision) {}

func (s *e2eScheduler) UpdateSticky(_ context.Context, _, _, _, _ string) bool { return false }

// streamedToolUse 是按 Anthropic SDK 累加语义还原出来的一次工具调用。
type streamedToolUse struct {
	ID      string
	Name    string
	ArgsRaw string
}

// readAnthropicToolUses 逐行解析 Anthropic SSE：
// content_block_start(tool_use) 收 id/name → content_block_delta(input_json_delta) 累加
// partial_json → 返回每个块的拼接结果与 message_delta 的 stop_reason。
// 这与 anthropic-sdk-typescript 的 #accumulateMessage 行为一致（缺 partial_json 就是空 input）。
func readAnthropicToolUses(t *testing.T, body io.Reader) ([]streamedToolUse, []int, string) {
	t.Helper()
	type acc struct {
		id, name string
		args     strings.Builder
	}
	opened := map[int]*acc{}
	var order []int
	stopReason := ""

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var frame struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
		}
		require.NoErrorf(t, json.Unmarshal([]byte(payload), &frame), "SSE 帧不是合法 JSON: %s", payload)
		switch frame.Type {
		case "content_block_start":
			if frame.ContentBlock.Type == "tool_use" {
				opened[frame.Index] = &acc{id: frame.ContentBlock.ID, name: frame.ContentBlock.Name}
				order = append(order, frame.Index)
			}
		case "content_block_delta":
			if frame.Delta.Type == "input_json_delta" {
				if a := opened[frame.Index]; a != nil {
					a.args.WriteString(frame.Delta.PartialJSON)
				}
			}
		case "message_delta":
			stopReason = frame.Delta.StopReason
		}
	}
	require.NoError(t, scanner.Err(), "读 SSE 流失败")

	uses := make([]streamedToolUse, 0, len(order))
	for _, idx := range order {
		a := opened[idx]
		uses = append(uses, streamedToolUse{ID: a.id, Name: a.name, ArgsRaw: a.args.String()})
	}
	return uses, order, stopReason
}

// startGatewayE2E 起一条「假上游 + 真实 gateway 链路」，返回 gateway 的 base URL。
func startGatewayE2E(t *testing.T) (gatewayURL string) {
	t.Helper()

	// ── 假上游：/v1/models（供 registry 真实拉取）+ /v1/chat/completions（回放事故 SSE）
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"`+e2eModelName+`","object":"model"}]}`)
		case "/v1/chat/completions":
			// 网关必须把 Anthropic 请求翻成 OpenAI 请求：流式 + 带 tools。
			var req core.ChatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(req.Tools) == 0 {
				http.Error(w, "上游没收到 tools：Anthropic→OpenAI 的工具转换丢了", http.StatusBadRequest)
				return
			}
			if !req.Stream {
				// 非流式：返回一次性 completion，tool_calls 里同样是全量 arguments。
				rawArgs, _ := json.Marshal(incidentToolArgsObject)
				resp := map[string]any{
					"id": "chatcmpl-e2e", "object": "chat.completion", "model": e2eModelName,
					"choices": []any{map[string]any{
						"index": 0, "finish_reason": "tool_calls",
						"message": map[string]any{
							"role":    "assistant",
							"content": "I will run it.",
							"tool_calls": []any{map[string]any{
								"id": "call_sglang_1", "type": "function",
								"function": map[string]any{"name": "Bash", "arguments": string(rawArgs)},
							}},
						},
					}},
					"usage": map[string]any{"prompt_tokens": 42, "completion_tokens": 17, "total_tokens": 59},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher, _ := w.(http.Flusher)
			for _, frame := range incidentUpstreamSSE(t) {
				fmt.Fprintf(w, "data: %s\n\n", frame)
				if flusher != nil {
					flusher.Flush()
				}
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	// ── 真实 provider + 真实 registry（真的向假上游拉一次 /v1/models）
	prov := openai.New(providers.ProviderConfig{
		Type: "openai", APIKey: "e2e-key", BaseURL: upstream.URL + "/v1",
	}, providers.ProviderOptions{})
	registry := providers.NewModelRegistry()
	registry.RegisterProviderWithNameAndType(prov, "h1004", "openai")
	loaded, err := registry.RefreshProviderModels(context.Background(), "h1004")
	require.NoError(t, err, "registry 应从假上游拉到模型列表")
	require.Positive(t, loaded, "registry 里的模型数必须 > 0")

	providerRouter, err := providers.NewRouter(registry)
	require.NoError(t, err)
	require.True(t, providerRouter.ModelCount() > 0)

	// ── 真实 orchestrator + 真实 group 分发（调度决定用替身）
	orch := orchestrator.NewChatOrchestrator(providerRouter)
	nameCache := cache.NewGroupNameCache("")
	nameCache.SetNames([]string{e2eModelName})
	dispatcher := orchestrator.NewGroupAwareDispatcher(&e2eScheduler{decision: &scheduler.RouteDecision{
		GroupName:    e2eModelName,
		ModelID:      "1",
		ModelName:    e2eModelName,
		ProviderName: "h1004",
	}}, nameCache)

	// ── 真实 handler + 真实 gin 引擎，挂在真实 HTTP server 上
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	messagesHandler := handler.NewMessagesHandlerWithGroup(orch, dispatcher)
	engine.POST("/v1/messages", messagesHandler.HandleMessages)

	gateway := httptest.NewServer(engine)
	t.Cleanup(gateway.Close)
	return gateway.URL
}

// TestE2E_AnthropicMessagesStream_FirstChunkToolArgs 是 2026-10-20 事故的端到端回归：
// 上游把工具参数全放在首块，客户端按 Anthropic SDK 语义累加后必须拿到完整的 input。
func TestE2E_AnthropicMessagesStream_FirstChunkToolArgs(t *testing.T) {
	gatewayURL := startGatewayE2E(t)

	body := `{"model":"` + e2eModelName + `","max_tokens":64,"stream":true,` +
		`"tools":[{"name":"Bash","description":"run a shell command",` +
		`"input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}],` +
		`"messages":[{"role":"user","content":"run git status"}]}`

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayURL+"/v1/messages", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "网关请求失败")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"), "必须是 Anthropic SSE")

	uses, indexes, stopReason := readAnthropicToolUses(t, resp.Body)
	require.Len(t, uses, 1, "应当恰好一个 tool_use 块（事故现场是 0 个可用块）")
	assert.Equal(t, "call_sglang_1", uses[0].ID, "工具调用 id 必须透传")
	assert.Equal(t, "Bash", uses[0].Name, "工具名必须透传")
	assert.Equal(t, "tool_use", stopReason, "finish_reason=tool_calls 必须映射成 tool_use")

	// 块 index 必须严格升序（anthropic-sdk 用 push 累加，乱序会错位）。
	for i := 1; i < len(indexes); i++ {
		assert.Greater(t, indexes[i], indexes[i-1], "content_block index 必须严格升序：%v", indexes)
	}

	var input map[string]any
	require.NoErrorf(t, json.Unmarshal([]byte(uses[0].ArgsRaw), &input),
		"累加后的 tool_use.input 必须是合法 JSON，实际 %q", uses[0].ArgsRaw)
	assert.Equal(t, "git status", input["command"],
		"事故症状：这里为空/缺失，Claude Code 会报 InputValidationError: required parameter command is missing")
	assert.Equal(t, "show tree", input["description"])

	if os.Getenv("E2E_VERBOSE") != "" {
		t.Logf("tool_use = %+v stop_reason=%s", uses[0], stopReason)
	}
}

// TestE2E_AnthropicMessagesNonStream_ToolUseInput 覆盖非流式路径（stream=false）：
// 上游一次性返回 completion，网关必须把它翻成 Anthropic 的 tool_use 块，input 同样完整。
// 这条路径走的是 core.responseMessageToAnthropicBlocks，与流式的转换器是两份代码。
func TestE2E_AnthropicMessagesNonStream_ToolUseInput(t *testing.T) {
	gatewayURL := startGatewayE2E(t)

	body := `{"model":"` + e2eModelName + `","max_tokens":64,` +
		`"tools":[{"name":"Bash","description":"run a shell command",` +
		`"input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}],` +
		`"messages":[{"role":"user","content":"run git status"}]}`

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayURL+"/v1/messages", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var message struct {
		Content []struct {
			Type  string         `json:"type"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&message))

	// 上游先给了一句文本，所以 content 里通常还有 text 块：只挑 tool_use 断言。
	var toolBlocks []struct {
		Type  string         `json:"type"`
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	}
	for _, block := range message.Content {
		if block.Type == "tool_use" {
			toolBlocks = append(toolBlocks, block)
		}
	}
	require.Len(t, toolBlocks, 1, "应当恰好一个 tool_use 块")
	block := toolBlocks[0]
	assert.Equal(t, "call_sglang_1", block.ID)
	assert.Equal(t, "Bash", block.Name)
	assert.Equal(t, "git status", block.Input["command"], "非流式路径的 input 同样不能为空")
	assert.Equal(t, "show tree", block.Input["description"])
	assert.Equal(t, "tool_use", message.StopReason)
}
