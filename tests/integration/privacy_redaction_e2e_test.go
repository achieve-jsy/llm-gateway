package integration

// 进程内全链路 E2E：不依赖 Docker / Postgres / Redis（make test 会跑到）。
//
// 链路（除标注外均为生产实现）：
//   真实 http.Client
//     → gin 引擎 + handler.ChatHandler / MessagesHandler   ← 真实
//     → orchestrator.ChatOrchestrator                      ← 真实
//     → providers.Router + openai.CompatibleProvider        ← 真实
//     → httptest 假上游                                     ← 测试替身
//
// 断言的是脱敏的关键不变量：
//   ① 上游拿到的请求里没有凭证原文，只有占位符
//   ② 客户端拿到的响应里没有占位符，只有还原后的原文（含 SSE 跨帧）
//   ③ mask 模式下保护失败 → 500 redaction_failed，且上游一次都没被调用（fail-closed）
//   ④ off 模式行为与未接入时完全一致

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/privacy"
	"github.com/example/llm-gateway/internal/gateway/redact"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	e2ePrivModel = "bf-privacy-e2e"
	// 合成凭证：GitLab PAT 形状，命中 GITLAB_TOKEN 规则。绝不使用真实密钥。
	e2ePrivSecret = "glpat-AbCdEfGhIjKlMnOpQrSt"
)

// upstreamSpy 记录假上游收到的每一次请求体，并可按需回显。
type upstreamSpy struct {
	mu     sync.Mutex
	bodies []string
	// stream 为 true 时回放 SSE，否则返回一次性 completion。
	stream bool
}

func (s *upstreamSpy) record(b string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, b)
}

func (s *upstreamSpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func (s *upstreamSpy) all() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.bodies, "\n")
}

func (s *upstreamSpy) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"`+e2ePrivModel+`","object":"model"}]}`)
			return
		case "/v1/chat/completions":
			raw, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			s.record(string(raw))

			var req core.ChatRequest
			require.NoError(t, json.Unmarshal(raw, &req))
			// 回显客户端最后一条消息的文本，便于断言「模型看到了什么」。
			echo := lastUserText(req)

			if !s.stream {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "chatcmpl-priv", "object": "chat.completion", "model": e2ePrivModel,
					"choices": []any{map[string]any{
						"index": 0, "finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": "我看到的：" + echo},
					}},
					"usage": map[string]any{"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
				})
				return
			}

			// 流式：把回显文本从占位符中间切开，逼还原逻辑跨帧工作。
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher, _ := w.(http.Flusher)
			emit := func(content string) {
				frame, _ := json.Marshal(map[string]any{
					"id": "chatcmpl-priv", "object": "chat.completion.chunk", "model": e2ePrivModel,
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}}},
				})
				fmt.Fprintf(w, "data: %s\n\n", frame)
				if flusher != nil {
					flusher.Flush()
				}
			}
			for _, part := range splitInPlaceholder("我看到的：" + echo) {
				emit(part)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		case "/v1/responses":
			raw, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			s.record(string(raw))
			echo := findPlaceholder(string(raw))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "resp_priv", "object": "response", "created_at": 1, "model": e2ePrivModel,
				"output": []any{map[string]any{
					"type": "message", "id": "msg_priv", "role": "assistant", "status": "completed",
					"content": []any{map[string]any{"type": "output_text", "text": "我看到的：" + echo}},
				}},
				"usage": map[string]any{"input_tokens": 5, "output_tokens": 3, "total_tokens": 8},
			})
			return
		case "/v1/embeddings":
			raw, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			s.record(string(raw))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list", "model": e2ePrivModel,
				"data":  []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{0.1, 0.2}}},
				"usage": map[string]any{"prompt_tokens": 3, "total_tokens": 3},
			})
			return
		case "/v1/rerank", "/rerank":
			raw, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			s.record(string(raw))
			echo := findPlaceholder(string(raw))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "rerank_priv", "model": e2ePrivModel,
				"results": []any{map[string]any{
					"index": 0, "relevance_score": 0.9,
					"document": map[string]any{"text": echo},
				}},
				"usage": map[string]any{"total_tokens": 6},
			})
			return
		default:
			http.NotFound(w, r)
		}
	}
}

// findPlaceholder 在请求 JSON 里找出第一个占位符字符串，供假上游「回显模型看到了什么」。
func findPlaceholder(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return ""
	}
	var walk func(any) string
	walk = func(x any) string {
		switch t := x.(type) {
		case string:
			if strings.Contains(t, "{{") {
				return t
			}
		case []any:
			for _, e := range t {
				if s := walk(e); s != "" {
					return s
				}
			}
		case map[string]any:
			for _, e := range t {
				if s := walk(e); s != "" {
					return s
				}
			}
		}
		return ""
	}
	return walk(v)
}

// splitInPlaceholder 尽可能把文本从 "{{KIND_" 中间切开；没占位符就切成两半。
func splitInPlaceholder(s string) []string {
	if i := strings.Index(s, "{{"); i >= 0 {
		if j := strings.Index(s[i:], "_"); j > 0 && i+j+1 < len(s) {
			cut := i + j + 1 // 切在 "{{KIND_" 之后
			return []string{s[:cut], s[cut:]}
		}
	}
	mid := len(s) / 2
	return []string{s[:mid], s[mid:]}
}

func lastUserText(req core.ChatRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role != "user" {
			continue
		}
		if s, ok := req.Messages[i].Content.(string); ok {
			return s
		}
	}
	return ""
}

// startPrivacyE2E 起「假上游 + 真实 gateway 链路」，返回 gateway base URL 与上游探针。
func startPrivacyE2E(t *testing.T, cfg redact.Config, stream bool) (string, *upstreamSpy) {
	t.Helper()

	spy := &upstreamSpy{stream: stream}
	upstream := httptest.NewServer(spy.handler(t))
	t.Cleanup(upstream.Close)

	prov := openai.New(providers.ProviderConfig{
		Type: "openai", APIKey: "e2e-key", BaseURL: upstream.URL + "/v1",
	}, providers.ProviderOptions{})
	registry := providers.NewModelRegistry()
	registry.RegisterProviderWithNameAndType(prov, "privacy-e2e", "openai")
	_, err := registry.RefreshProviderModels(context.Background(), "privacy-e2e")
	require.NoError(t, err)

	providerRouter, err := providers.NewRouter(registry)
	require.NoError(t, err)
	orch := orchestrator.NewChatOrchestrator(providerRouter)

	if cfg.KeyPath == "" {
		cfg.KeyPath = filepath.Join(t.TempDir(), "hmac.key")
	}
	engine, err := redact.New(cfg)
	require.NoError(t, err)

	chatHandler := handler.NewChatHandler(orch)
	chatHandler.SetRedactor(engine)
	messagesHandler := handler.NewMessagesHandler(orch)
	messagesHandler.SetRedactor(engine)

	gin.SetMode(gin.TestMode)
	g := gin.New()
	responsesHandler := handler.NewResponsesHandler(orch)
	responsesHandler.SetRedactor(engine)
	embeddingsHandler := handler.NewEmbeddingsHandler(orch)
	embeddingsHandler.SetRedactor(engine)
	rerankHandler := handler.NewRerankHandler(orch)
	rerankHandler.SetRedactor(engine)

	g.POST("/v1/chat/completions", chatHandler.Handle)
	g.POST("/v1/messages", messagesHandler.HandleMessages)
	g.POST("/v1/responses", responsesHandler.Handle)
	g.POST("/v1/embeddings", embeddingsHandler.Handle)
	g.POST("/v1/rerank", rerankHandler.Handle)

	gw := httptest.NewServer(g)
	t.Cleanup(gw.Close)
	return gw.URL, spy
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	return resp
}

func chatBody(stream bool, content string) string {
	b, _ := json.Marshal(map[string]any{
		"model":  e2ePrivModel,
		"stream": stream,
		"messages": []any{
			map[string]any{"role": "user", "content": content},
		},
	})
	return string(b)
}

// ① 非流式：上游只见占位符，客户端只见原文。
func TestE2E_Privacy_ChatNonStream_MaskUpstreamRestoreClient(t *testing.T) {
	url, spy := startPrivacyE2E(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true}, false)

	resp := postJSON(t, url+"/v1/chat/completions", chatBody(false, "帮我看看这个 token："+e2ePrivSecret))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// 上游：没有原文，只有占位符
	require.NotContains(t, spy.all(), e2ePrivSecret, "凭证原文被发给了上游")
	require.Contains(t, spy.all(), "{{", "上游应收到占位符")

	// 客户端：没有占位符，原文已还原
	require.Contains(t, string(body), e2ePrivSecret, "客户端未拿到还原后的原文")
	require.NotContains(t, string(body), "{{", "客户端看到了占位符")
}

// ② 流式：占位符被切在两帧之间，客户端拼出来仍必须是原文。
func TestE2E_Privacy_ChatStream_RestoresSplitPlaceholder(t *testing.T) {
	url, spy := startPrivacyE2E(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true}, true)

	resp := postJSON(t, url+"/v1/chat/completions", chatBody(true, "token="+e2ePrivSecret))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var assembled strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var frame struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame))
		for _, c := range frame.Choices {
			assembled.WriteString(c.Delta.Content)
		}
	}
	require.NoError(t, scanner.Err())

	require.NotContains(t, spy.all(), e2ePrivSecret, "凭证原文被发给了上游")
	require.Contains(t, assembled.String(), e2ePrivSecret, "流式拼接后没有还原原文")
	require.NotContains(t, assembled.String(), "{{", "流式里漏出了占位符")
}

// ③ fail-closed：Block 动作 → 500 redaction_failed，且上游一次都没收到请求。
func TestE2E_Privacy_BlockFailsClosed_NoUpstreamCall(t *testing.T) {
	url, spy := startPrivacyE2E(t, redact.Config{
		Mode:      redact.ModeMask,
		RulesOnly: true,
		Actions:   map[string]string{"GITLAB_TOKEN": privacy.Block},
	}, false)

	resp := postJSON(t, url+"/v1/chat/completions", chatBody(false, "token="+e2ePrivSecret))
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Contains(t, string(body), "redaction_failed")
	require.Zero(t, spy.count(), "fail-closed 时上游不应被调用")
}

// ④ off 模式：行为与未接入完全一致。
func TestE2E_Privacy_OffPassesThrough(t *testing.T) {
	url, spy := startPrivacyE2E(t, redact.Config{Mode: redact.ModeOff}, false)

	resp := postJSON(t, url+"/v1/chat/completions", chatBody(false, "token="+e2ePrivSecret))
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, spy.all(), e2ePrivSecret, "off 模式下不该改写请求")
	require.Contains(t, string(body), e2ePrivSecret)
	require.NotContains(t, string(body), "{{")
}

// ⑤ /v1/responses：数组 input + instructions 都脱敏，响应 output_text 还原。
func TestE2E_Privacy_Responses_MaskAndRestore(t *testing.T) {
	url, spy := startPrivacyE2E(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true}, false)

	body, _ := json.Marshal(map[string]any{
		"model":        e2ePrivModel,
		"instructions": "记住这个 token:" + e2ePrivSecret,
		"input": []any{map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "key=" + e2ePrivSecret}},
		}},
	})
	resp := postJSON(t, url+"/v1/responses", string(body))
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode, "响应: %s", string(out))
	require.NotContains(t, spy.all(), e2ePrivSecret, "responses 链路把原文发给了上游")
	require.Contains(t, spy.all(), "{{", "上游应收到占位符")
	require.NotContains(t, string(out), "{{", "responses 响应里漏出了占位符")
	require.Contains(t, string(out), e2ePrivSecret, "responses 响应未还原原文")
}

// ⑥ /v1/embeddings：只脱敏入参，响应是向量、无需还原。
func TestE2E_Privacy_Embeddings_MaskUpstreamOnly(t *testing.T) {
	url, spy := startPrivacyE2E(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true}, false)

	body, _ := json.Marshal(map[string]any{
		"model": e2ePrivModel,
		"input": []any{"我的 key 是 " + e2ePrivSecret, "second"},
	})
	resp := postJSON(t, url+"/v1/embeddings", string(body))
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotContains(t, spy.all(), e2ePrivSecret, "embeddings 链路把原文发给了上游")
	require.Contains(t, spy.all(), "{{", "上游应收到占位符")
	require.Contains(t, spy.all(), "second", "非敏感文本不应被改动")
}

// ⑦ /v1/rerank：query/documents 脱敏，上游回显的文档文本必须还原成原文。
func TestE2E_Privacy_Rerank_RestoresEchoedDocument(t *testing.T) {
	url, spy := startPrivacyE2E(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true}, false)

	body, _ := json.Marshal(map[string]any{
		"model":     e2ePrivModel,
		"query":     "找 token " + e2ePrivSecret,
		"documents": []any{"文档里也有 " + e2ePrivSecret},
	})
	resp := postJSON(t, url+"/v1/rerank", string(body))
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode, "响应: %s", string(out))
	require.NotContains(t, spy.all(), e2ePrivSecret, "rerank 链路把原文发给了上游")
	require.NotContains(t, string(out), "{{", "rerank 回显文档漏出了占位符")
	require.Contains(t, string(out), e2ePrivSecret, "rerank 回显文档未还原原文")
}

// ⑧ detect 模式：只观测不改写——上游照旧收到原文，客户端也不会看到占位符。
func TestE2E_Privacy_DetectForwardsPlaintext(t *testing.T) {
	url, spy := startPrivacyE2E(t, redact.Config{Mode: redact.ModeDetect, RulesOnly: true}, false)

	resp := postJSON(t, url+"/v1/chat/completions", chatBody(false, "token="+e2ePrivSecret))
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, spy.all(), e2ePrivSecret, "detect 模式不应改写请求")
	require.NotContains(t, string(body), "{{")
	require.Contains(t, string(body), e2ePrivSecret)
}

// ⑥ Anthropic /v1/messages：入站 Anthropic 结构、出站 Anthropic 结构，同样必须脱敏+还原。
func TestE2E_Privacy_AnthropicMessages_MaskAndRestore(t *testing.T) {
	url, spy := startPrivacyE2E(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true}, false)

	body, _ := json.Marshal(map[string]any{
		"model":      e2ePrivModel,
		"max_tokens": 64,
		"messages": []any{
			map[string]any{"role": "user", "content": "我的 token 是 " + e2ePrivSecret},
		},
	})
	resp := postJSON(t, url+"/v1/messages", string(body))
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode, "响应: %s", string(out))
	require.NotContains(t, spy.all(), e2ePrivSecret, "Anthropic 链路把原文发给了上游")
	require.Contains(t, spy.all(), "{{", "上游应收到占位符")
	require.NotContains(t, string(out), "{{", "Anthropic 响应里漏出了占位符")
	require.Contains(t, string(out), e2ePrivSecret, "Anthropic 响应未还原原文")
}
