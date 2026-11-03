package handler_test

// 原生旁路 vs 转换路径的端到端对比基准。
//
// 为什么需要它：原生旁路唯一的价值主张是「更快、字段更保真」，但此前只有
// 转换器的微基准（internal/protocol/bench_test.go），没有一条测过整条链路。
// 没有对比数据，就无法回答「这套东西值不值得继续扩」。
//
// 对照设计（关键）：**同一个渠道、同一个假上游、同一份响应**，只切 protocol.Policy
// —— active 走原生旁路，legacy 走结构体往返。这样唯一变量就是路径本身，
// 上游延迟被完全消除（httptest 本机、零 sleep）。
//
// 注意：测的是「网关自身开销」，不代表线上墙钟时间；线上差异主要由上游决定。
//
// 跑法：
//   go test ./tests/gateway/handler/ -bench 'BenchmarkPath_' -benchmem -run '^$'

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/anthropic"
	"github.com/example/llm-gateway/internal/providers/litellm"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	benchGroup         = "bench-group"
	benchReal          = "bench-model"
	benchAnthropicBody = `{"model":"bench-group","max_tokens":64,
		"metadata":{"user_id":"u-bench"},
		"thinking":{"type":"enabled","budget_tokens":1024},
		"system":[{"type":"text","text":"be brief","cache_control":{"type":"ephemeral"}}],
		"messages":[{"role":"user","content":[{"type":"text","text":"hello world","cache_control":{"type":"ephemeral"}}]}]}`

	benchChatBody = `{"model":"bench-group","messages":[{"role":"user","content":"hello world"}],
		"temperature":0.7,"max_tokens":64,"top_p":0.9}`

	// 上游固定响应：内容瘦、字段齐，避免响应解析成为噪声主因。
	benchAnthropicResp = `{"id":"msg_bench","type":"message","role":"assistant",
		"model":"bench-model","content":[{"type":"text","text":"ok"}],
		"stop_reason":"end_turn","usage":{"input_tokens":11,"output_tokens":7}}`

	benchChatResp = `{"id":"chatcmpl-bench","object":"chat.completion","created":1700000000,
		"model":"bench-model",
		"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`
)

// benchAnthropicBodyLarge 模拟真实长上下文（~25KB：system prompt + 60 轮历史）。
// 小 body 会把协议转换成本稀释掉，看不出两条路的真实差距。
var benchAnthropicBodyLarge = func() string {
	var b strings.Builder
	b.WriteString(`{"model":"bench-group","max_tokens":512,"metadata":{"user_id":"u-bench"},"thinking":{"type":"enabled","budget_tokens":8192},"messages":[`)
	for i := 0; i < 60; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		b.WriteString(`{"role":"` + role + `","content":[{"type":"text","text":"`)
		b.WriteString(strings.Repeat("lorem ipsum dolor sit amet ", 12))
		b.WriteString(`"}]}`)
	}
	b.WriteString(`]}`)
	return b.String()
}()

// policyFor 构造指定模式、作用于 test-key 的策略。
func policyFor(b testing.TB, mode string) *protocol.Policy {
	b.Helper()
	p, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: mode,
		PerKey:      map[string]string{"test-key": mode},
	})
	require.NoError(b, err)
	return p
}

func benchDispatcher(names []string) *orchestrator.GroupAwareDispatcher {
	nc := cache.NewGroupNameCache("")
	nc.SetNames(names)
	sch := &fakeMessagesScheduler{decision: &scheduler.RouteDecision{
		GroupName: benchGroup, ModelID: "1",
		ModelName: benchReal, ProviderName: "bench-provider",
	}}
	return orchestrator.NewGroupAwareDispatcher(sch, nc)
}

// newMessagesBenchRouter 组装 /v1/messages 全链路（真 orchestrator + 真 Router + 真 anthropic）。
func newMessagesBenchRouter(tb testing.TB, upstreamURL, mode string) *gin.Engine {
	tb.Helper()
	gin.SetMode(gin.TestMode)

	p, ok := anthropic.New(providers.ProviderConfig{
		Type: "anthropic", BaseURL: upstreamURL + "/v1", APIKey: "k",
		Models: []string{benchReal},
	}, providers.ProviderOptions{}).(*anthropic.Provider)
	require.True(tb, ok)

	lookup := &nativeChainLookup{
		model: benchReal, provider: p,
		providerType: "anthropic", providerName: "bench-provider",
	}
	router, err := providers.NewRouter(lookup)
	require.NoError(tb, err)
	orch := orchestrator.NewChatOrchestrator(router)

	h := handler.NewMessagesHandlerWithGroup(orch, benchDispatcher([]string{benchGroup}))
	h.SetProtocolPolicy(policyFor(tb, mode))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/messages", h.HandleMessages)
	return r
}

// newChatBenchRouter 组装 /v1/chat/completions 全链路（真 orchestrator + 真 Router + 真 litellm）。
func newChatBenchRouter(tb testing.TB, upstreamURL, mode string) *gin.Engine {
	tb.Helper()
	gin.SetMode(gin.TestMode)

	p := litellm.NewWithHTTPClient("k", upstreamURL+"/v1", nil, llmclient.Hooks{})
	lookup := &nativeChainLookup{
		model: benchReal, provider: p,
		providerType: "litellm", providerName: "bench-provider",
	}
	router, err := providers.NewRouter(lookup)
	require.NoError(tb, err)
	orch := orchestrator.NewChatOrchestrator(router)

	h := handler.NewChatHandlerWithGroup(orch, benchDispatcher([]string{benchGroup}))
	h.SetProtocolPolicy(policyFor(tb, mode))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/chat/completions", h.Handle)
	return r
}

// ── 守卫：确保基准真的测到了目标路径 ───────────────────────────────
//
// 如果守卫失败，benchmark 的数据毫无意义（可能在测两条一样的路）。

func TestPathGuard_MessagesNativeKeepsPrivilegedFields(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(benchAnthropicResp))
	}))
	defer srv.Close()

	r := newMessagesBenchRouter(t, srv.URL, "active")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(benchAnthropicBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, gotBody, "cache_control", "原生路径必须保留 Anthropic 独有字段")
	require.Contains(t, gotBody, "\"metadata\"")
	require.Contains(t, gotBody, "\"thinking\"")
}

func TestPathGuard_MessagesConversionDropsPrivilegedFields(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(benchAnthropicResp))
	}))
	defer srv.Close()

	r := newMessagesBenchRouter(t, srv.URL, "legacy")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(benchAnthropicBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	// 注意用词：cache_control 在结构体里有建模，转换路径**不丢**；
	// 真正丢的是没有建模的 metadata / thinking。
	require.Contains(t, gotBody, "cache_control",
		"cache_control 已建模，转换路径会保留")
	require.NotContains(t, gotBody, "\"metadata\"",
		"预期转换路径丢失 metadata —— 这是原生旁路真正的保真价值")
	require.NotContains(t, gotBody, "\"thinking\"",
		"预期转换路径丢失 thinking")
}

// ── 有效对照组：同一条 RK 管线，只切渠道是否声明原生能力 ─────────────
//
// 上面的 Messages_Native vs Messages_Conversion 实际比较的是 RK 管线与 legacy 管线，
// 变量不止一个（legacy 走 core 结构体解析，RK 走 relaykit 全量解码）。
// 真正该比的是：同一个 RK 入口，provider 有没有实现 NativeRelayProvider。
//
// noNativeProvider 包住真 anthropic provider 但不实现原生契约，
// 于是 prepareNativeRelay 返回 ErrNativeNotSupported → 走 RK 的转换转发。

// 嵌入 core.Provider 接口即获得全部常规方法（Chat / Embeddings …），
// 而 NativeRelayProvider 的方法集不在 core.Provider 里，所以**不会**被提升 ——
// 这正是我们要的「同一渠道、没有原生能力」。
type noNativeProvider struct{ core.Provider }

func newMessagesBenchRouterWith(tb testing.TB, p core.Provider, mode string) *gin.Engine {
	tb.Helper()
	gin.SetMode(gin.TestMode)

	lookup := &nativeChainLookup{
		model: benchReal, provider: p,
		providerType: "anthropic", providerName: "bench-provider",
	}
	router, err := providers.NewRouter(lookup)
	require.NoError(tb, err)
	orch := orchestrator.NewChatOrchestrator(router)

	h := handler.NewMessagesHandlerWithGroup(orch, benchDispatcher([]string{benchGroup}))
	h.SetProtocolPolicy(policyFor(tb, mode))

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/messages", h.HandleMessages)
	return r
}

func benchmarkMessagesCompare(b *testing.B, wrap func(*anthropic.Provider) core.Provider, clientBody string) {
	// 转换路径会打 slog（thinking 的 max_tokens 被自动上调），
	// 日志与基准输出混在一行会吞掉数字。
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(prev) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(benchAnthropicResp))
	}))
	defer srv.Close()

	inner, ok := anthropic.New(providers.ProviderConfig{
		Type: "anthropic", BaseURL: srv.URL + "/v1", APIKey: "k",
		Models: []string{benchReal},
	}, providers.ProviderOptions{}).(*anthropic.Provider)
	require.True(b, ok)

	r := newMessagesBenchRouterWith(b, wrap(inner), "active")
	body := strings.NewReader(clientBody)

	b.SetBytes(int64(len(clientBody)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body.Reset(clientBody)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

// 端到端验证：RK 管线（bridge.go）必须把客户端显式 thinking 预算搬运到 provider。
// 这是 relaykit 的明文契约：「exact budgets must survive multi-step routes」。
func TestRKPath_PreservesExplicitThinkingBudget(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(benchAnthropicResp))
	}))
	defer srv.Close()

	inner, ok := anthropic.New(providers.ProviderConfig{
		Type: "anthropic", BaseURL: srv.URL + "/v1", APIKey: "k",
		Models: []string{benchReal},
	}, providers.ProviderOptions{}).(*anthropic.Provider)
	require.True(t, ok)

	// active + 无原生能力 → 走 relaykit 转换后由 anthropic provider 出站。
	r := newMessagesBenchRouterWith(t, withoutNative(inner), "active")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(benchAnthropicBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, gotBody, "\"budget_tokens\":1024",
		"客户端显式预算 1024 必须原样到达上游，不能被 effort 反推成 5000")
	require.NotContains(t, gotBody, "6024",
		"max_tokens 不该被放大到 6024（那是预算被错误放大到 5000 的连带后果）")
}

func withNative(p *anthropic.Provider) core.Provider    { return p }
func withoutNative(p *anthropic.Provider) core.Provider { return &noNativeProvider{Provider: p} }

// 同一条 RK 管线：原生直发 vs 降级后走转换。这才是干净的对照组。
func BenchmarkCompare_Small_Native(b *testing.B) {
	benchmarkMessagesCompare(b, withNative, benchAnthropicBody)
}
func BenchmarkCompare_Small_Degraded(b *testing.B) {
	benchmarkMessagesCompare(b, withoutNative, benchAnthropicBody)
}
func BenchmarkCompare_Large_Native(b *testing.B) {
	benchmarkMessagesCompare(b, withNative, benchAnthropicBodyLarge)
}
func BenchmarkCompare_Large_Degraded(b *testing.B) {
	benchmarkMessagesCompare(b, withoutNative, benchAnthropicBodyLarge)
}

// ── 基准 ────────────────────────────────────────────────────────

func benchmarkMessages(b *testing.B, mode, clientBody string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(benchAnthropicResp))
	}))
	defer srv.Close()

	r := newMessagesBenchRouter(b, srv.URL, mode)
	body := strings.NewReader(clientBody)

	b.SetBytes(int64(len(clientBody)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body.Reset(clientBody)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func BenchmarkPath_Messages_Native(b *testing.B) {
	benchmarkMessages(b, "active", benchAnthropicBody)
}
func BenchmarkPath_Messages_Conversion(b *testing.B) {
	benchmarkMessages(b, "legacy", benchAnthropicBody)
}

// 长上下文对比：body 越大，协议转换的解析/重建成本越高，原生旁路的优势才显现。
func BenchmarkPath_MessagesLarge_Native(b *testing.B) {
	benchmarkMessages(b, "active", benchAnthropicBodyLarge)
}
func BenchmarkPath_MessagesLarge_Conversion(b *testing.B) {
	benchmarkMessages(b, "legacy", benchAnthropicBodyLarge)
}

func benchmarkChat(b *testing.B, mode string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(benchChatResp))
	}))
	defer srv.Close()

	r := newChatBenchRouter(b, srv.URL, mode)
	body := strings.NewReader(benchChatBody)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body.Reset(benchChatBody)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func BenchmarkPath_Chat_Native(b *testing.B)     { benchmarkChat(b, "active") }
func BenchmarkPath_Chat_Conversion(b *testing.B) { benchmarkChat(b, "legacy") }

var _ = context.Background
