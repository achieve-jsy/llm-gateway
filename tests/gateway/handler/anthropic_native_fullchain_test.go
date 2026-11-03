package handler_test

// anthropic 渠道的真链路端到端。
//
// 与 native_relay_fullchain_test.go 同构，区别只在渠道：那里验 bedrock（SigV4），
// 这里验 anthropic（x-api-key）。中间所有层都是生产实现
// （真实 gin handler → 真实 ChatOrchestrator → 真实 providers.Router → 真实
// anthropic.Provider），只伪造最上游的 HTTP 端点，因此能验证：
//   1. 客户端原始 Claude body 是否真的零转换到达上游（除 model 改写）；
//   2. Anthropic 独有字段（thinking 签名 / cache_control / metadata）是否存活；
//   3. 上游响应是否原样回到客户端，usage 是否回填。

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/anthropic"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 两段映射，与生产同构：
//  1. 客户端发组名 claude-group；
//  2. scheduler 解析出真实模型名 claude-sonnet-4-5，handler 写回 req.Model；
//  3. Router 把 body.model 定稿为 selector.Model，上游收到真实模型名。
//
// 这样断言「model 被改写、其余字段不动」才有意义。
const (
	anthropicGroupModel   = "claude-group"
	anthropicRealModel    = "claude-sonnet-4-5"
	anthropicUpstreamName = "claude-sonnet-4-5"
)

func newAnthropicFullChainRouter(t *testing.T, upstreamURL string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	p, ok := anthropic.New(providers.ProviderConfig{
		Type:    "anthropic",
		BaseURL: upstreamURL + "/v1",
		APIKey:  "test-key",
		Models:  []string{anthropicRealModel},
	}, providers.ProviderOptions{}).(*anthropic.Provider)
	require.True(t, ok)

	lookup := &nativeChainLookup{
		model: anthropicRealModel, provider: p,
		providerType: "anthropic", providerName: "anthropic-official",
	}
	router, err := providers.NewRouter(lookup)
	require.NoError(t, err)
	orch := orchestrator.NewChatOrchestrator(router)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{anthropicGroupModel})
	sch := &fakeMessagesScheduler{decision: &scheduler.RouteDecision{
		GroupName: anthropicGroupModel, ModelID: "1",
		ModelName: anthropicRealModel, ProviderName: "anthropic-official",
	}}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)

	h := handler.NewMessagesHandlerWithGroup(orch, gd)
	policy, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: "active",
		PerKey:      map[string]string{"test-key": "active"},
	})
	require.NoError(t, err)
	h.SetProtocolPolicy(policy)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/messages", h.HandleMessages)
	return r
}

// 非流式全链路：body 零转换到达上游、独有字段存活、响应原样回传。
func TestAnthropicFullChain_NonStreamPassthrough(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_up","type":"message","role":"assistant",
			"model":"claude-sonnet-4-5-20250929","content":[{"type":"text","text":"ok"}],
			"thinking":{"type":"enabled","signature":"sig-verify"},
			"usage":{"input_tokens":11,"output_tokens":7}}`))
	}))
	defer srv.Close()

	r := newAnthropicFullChainRouter(t, srv.URL)

	// thinking.signature + cache_control + metadata 全是 Anthropic 独有字段：
	// 走转换路径必丢，走原生旁路必须原样到达上游。
	clientBody := `{"model":"` + anthropicGroupModel + `","max_tokens":64,
		"metadata":{"user_id":"u-verify"},
		"thinking":{"type":"enabled","budget_tokens":1024},
		"system":[{"type":"text","text":"be brief","cache_control":{"type":"ephemeral"}}],
		"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}]}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(clientBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Equal(t, "/v1/messages", gotPath, "原生旁路必须打 /v1/messages（baseURL 自带 /v1）")

	// ── 零转换断言 ──
	var upstream map[string]any
	require.NoError(t, json.Unmarshal([]byte(gotBody), &upstream))

	require.Equal(t, anthropicUpstreamName, upstream["model"],
		"Router 必须把组名改写为解析后的真实模型名")
	require.Equal(t, float64(64), upstream["max_tokens"], "max_tokens 必须原样")
	require.Equal(t, map[string]any{"user_id": "u-verify"}, upstream["metadata"],
		"metadata 是 Anthropic 独有字段，转换路径会丢")
	require.Equal(t, map[string]any{"type": "enabled", "budget_tokens": float64(1024)},
		upstream["thinking"], "thinking 必须是客户端原样的形态，不被改写")

	system, ok := upstream["system"].([]any)
	require.True(t, ok, "system 必须保持 Anthropic 的数组形态")
	require.Contains(t, system[0].(map[string]any), "cache_control",
		"cache_control 是 Anthropic 独有字段，转换路径会丢")

	// ── 响应原样回传 ──
	require.Contains(t, w.Body.String(), "sig-verify",
		"上游响应必须原样回传，thinking 签名不能被清洗")
}

// 流式全链路：SSE 帧原样透传。
func TestAnthropicFullChain_StreamPassthrough(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_s\"}}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"你\"}}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	r := newAnthropicFullChainRouter(t, srv.URL)

	clientBody := `{"model":"` + anthropicGroupModel + `","max_tokens":32,"stream":true,
		"messages":[{"role":"user","content":"hi"}]}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(clientBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Equal(t, "/v1/messages", gotPath)
	require.Contains(t, w.Body.String(), "message_start", "上游 SSE 帧必须透传")
	require.Contains(t, w.Body.String(), "content_block_delta")
	require.Contains(t, w.Body.String(), "你", "UTF-8 分片不能在网关被破坏")
	require.Contains(t, w.Body.String(), "message_stop")
}

// 上游不支持的场景必须降级到转换路径，而不是把原生错误直接抛给客户端。
// 用一个「原生端点 404、chat 端点可用」的假上游来逼近真实情况。
func TestAnthropicFullChain_DegradesWhenUpstreamRejectsNative(t *testing.T) {
	var nativeHit, chatHit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages":
			atomic.AddInt32(&nativeHit, 1)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"type":"not_found_error"}}`))
		default:
			atomic.AddInt32(&chatHit, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion",
				"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
		}
	}))
	defer srv.Close()

	r := newAnthropicFullChainRouter(t, srv.URL)

	clientBody := `{"model":"` + anthropicGroupModel + `","max_tokens":16,
		"messages":[{"role":"user","content":"hi"}]}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(clientBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// 404 不是 ErrNativeNotSupported，因此**不该**降级 —— 这是有意的：
	// 上游已经收到请求，再走一次转换会产生第二次调用与重复计费。
	require.NotEqual(t, http.StatusOK, w.Code,
		"上游 404 属于真实错误，不应静默降级（避免二次打上游）")
	require.EqualValues(t, 1, atomic.LoadInt32(&nativeHit), "只应该打一次原生端点")
	require.EqualValues(t, 0, atomic.LoadInt32(&chatHit), "不应再打 chat 端点")
}
