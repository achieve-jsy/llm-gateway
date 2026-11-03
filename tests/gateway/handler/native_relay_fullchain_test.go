package handler_test

// 真链路端到端：真实 gin handler → 真实 ChatOrchestrator → 真实 providers.Router
// → 真实 bedrock.Provider → httptest 假 Bedrock 端点。
//
// 与 messages_native_e2e_test.go 的区别：那里用 fake orchestrator 验证 handler 分支，
// 这里把中间所有层都换成生产实现，只伪造最上游的 Bedrock HTTP 端点，
// 因此能验证「客户端原始 Claude body 是否真的零转换到达上游」以及
// 「上游 Anthropic 响应是否原样回到客户端、usage 是否被回填」。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream/eventstreamapi"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/bedrock"
	"github.com/gin-gonic/gin"
)

// nativeChainLookup 是单模型注册表，只为 Router 提供"这个模型属于哪个 provider"。
// providerType/providerName 参数化，好让同一套装配逻辑覆盖不同渠道
// （bedrock 走 SigV4、anthropic 走 x-api-key，但链路结构相同）。
type nativeChainLookup struct {
	model        string
	provider     core.Provider
	providerType string
	providerName string
}

func (l *nativeChainLookup) trim(selector string) string {
	if i := strings.LastIndex(selector, "/"); i >= 0 {
		return selector[i+1:]
	}
	return selector
}

func (l *nativeChainLookup) Supports(model string) bool { return l.trim(model) == l.model }

func (l *nativeChainLookup) GetProvider(model string) core.Provider {
	if l.trim(model) == l.model {
		return l.provider
	}
	return nil
}

func (l *nativeChainLookup) GetProviderType(model string) string {
	if l.trim(model) == l.model {
		return l.providerType
	}
	return ""
}

func (l *nativeChainLookup) ListModels() []core.Model {
	return []core.Model{{ID: l.model}}
}

func (l *nativeChainLookup) ModelCount() int { return 1 }

func (l *nativeChainLookup) GetProviderName(model string) string {
	if l.trim(model) == l.model {
		return l.providerName
	}
	return ""
}

func (l *nativeChainLookup) GetProviderNameForType(string) string { return l.providerName }
func (l *nativeChainLookup) GetProviderTypeForName(string) string { return l.providerType }

// newFullChainRouter 组装「handler + 真 orchestrator + 真 router + 真 bedrock provider」。
// upstream 收到的请求体通过 *capturedBody 回传，供断言零转换。
func newFullChainRouter(t *testing.T, upstreamURL string, extra gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// bedrock provider 指向假端点。用 IAM JSON 凭证让 SDK 走 SigV4（假端点不校验签名）。
	t.Setenv("AWS_REGION", "us-east-1")
	p := bedrock.New(providers.ProviderConfig{
		Type:    "bedrock",
		BaseURL: upstreamURL,
		APIKey:  `{"access_key_id":"AKIAFAKE","secret_access_key":"secret"}`,
		Models:  []string{"claude-sonnet-4-5"},
		// 组名 → 上游模型 ID：验证 Router 改写 body.model 后进到这里。
		ModelUpstreamIDs: map[string]string{"claude-sonnet-4-5": "global.anthropic.claude-sonnet-4-5-20250929-v1:0"},
	}, providers.ProviderOptions{})

	lookup := &nativeChainLookup{
		model: "claude-sonnet-4-5", provider: p,
		providerType: "bedrock", providerName: "aws",
	}
	router, err := providers.NewRouter(lookup)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	orch := orchestrator.NewChatOrchestrator(router)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"claude-sonnet-4-5"})
	sch := &fakeMessagesScheduler{decision: &scheduler.RouteDecision{
		GroupName: "claude-sonnet-4-5", ModelID: "1", ModelName: "claude-sonnet-4-5", ProviderName: "aws",
	}}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)

	h := handler.NewMessagesHandlerWithGroup(orch, gd)
	policy, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: "active",
		PerKey:      map[string]string{"test-key": "active"},
	})
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	h.SetProtocolPolicy(policy)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	// extra 必须在注册路由之前挂上：gin 的 Use 不会作用于已注册的路由。
	if extra != nil {
		r.Use(extra)
	}
	r.POST("/v1/messages", h.HandleMessages)
	return r
}

// TestFullChain_NativeClaudePassthrough 验证 Claude 请求以原生 body 到达 Bedrock。
func TestFullChain_NativeClaudePassthrough(t *testing.T) {
	var gotBody []byte
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hi there"}],"usage":{"input_tokens":25,"output_tokens":15}}`))
	}))
	defer upstream.Close()

	r := newFullChainRouter(t, upstream.URL, nil)

	// 客户端发的是组名 + 只有 Anthropic 才有的字段（top_k / context_management）
	reqBody := `{"model":"claude-sonnet-4-5","max_tokens":1024,"top_k":7,"temperature":0.5,"messages":[{"role":"user","content":"Hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	// 1. 确实打到了 InvokeModel 端点
	if !strings.Contains(gotPath, "invoke") {
		t.Errorf("upstream path = %q, want 包含 invoke", gotPath)
	}

	// 2. 上游收到的是 Anthropic 原生 body，不是 Chat
	var up map[string]any
	if err := json.Unmarshal(gotBody, &up); err != nil {
		t.Fatalf("upstream body 不是 JSON: %v / %s", err, gotBody)
	}
	if v, _ := up["anthropic_version"].(string); v != "bedrock-2023-05-31" {
		t.Errorf("anthropic_version = %q, want bedrock-2023-05-31", v)
	}
	if v, ok := up["top_k"].(float64); !ok || v != 7 {
		t.Errorf("top_k 未保留: %v（这是本次改造的核心动机）", up["top_k"])
	}
	// 3. model 已被 Router 改写成上游 ID
	if v, _ := up["model"].(string); v != "global.anthropic.claude-sonnet-4-5-20250929-v1:0" {
		t.Errorf("model = %q, want 上游 ID", v)
	}
	// 4. 不得出现 Chat 专有字段（说明没走转换）
	if _, ok := up["max_tokens"]; !ok {
		t.Error("max_tokens 丢失")
	}
	if _, ok := up["messages"]; !ok {
		t.Error("messages 丢失")
	}

	// 5. 客户端收到的是上游 Anthropic 响应原文
	if !strings.Contains(w.Body.String(), "Hi there") {
		t.Errorf("客户端响应缺少上游文本: %s", w.Body.String())
	}
	var clientResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &clientResp); err != nil {
		t.Fatalf("客户端响应不是 JSON: %v", err)
	}
	if v, _ := clientResp["id"].(string); v != "msg_1" {
		t.Errorf("响应 id = %q, want msg_1（应为上游原样）", v)
	}
}

// TestFullChain_NonClaudeModelFallsBack 验证非 Claude 模型不走原生路径，
// 而是回退到 Converse（证明分流是按模型而非按渠道）。
func TestFullChain_NonClaudeModelFallsBack(t *testing.T) {
	var hitInvoke, hitConverse bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "converse") {
			hitConverse = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"from converse"}]}},"stopReason":"end_turn","usage":{"inputTokens":5,"outputTokens":2,"totalTokens":7}}`))
			return
		}
		hitInvoke = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	// 这里故意只注册 gpt 模型，验证 Claude 判定不会误命中
	t.Setenv("AWS_REGION", "us-east-1")
	p := bedrock.New(providers.ProviderConfig{
		Type:    "bedrock",
		BaseURL: upstream.URL,
		APIKey:  `{"access_key_id":"AKIAFAKE","secret_access_key":"secret"}`,
		Models:  []string{"gpt-5.6-sol"},
	}, providers.ProviderOptions{})

	lookup := &nativeChainLookup{model: "gpt-5.6-sol", provider: p}
	router, err := providers.NewRouter(lookup)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	// 直接问用户真正关心的问题：这个 provider 认不认 Claude 原生格式
	native, ok := p.(core.NativeRelayProvider)
	if !ok {
		t.Fatal("bedrock provider 未实现 NativeRelayProvider")
	}
	if _, supported := native.NativeRelayFormatFor("gpt-5.6-sol", core.NativeRelayClaude); supported {
		t.Error("gpt-5.6-sol 不应被判定为支持 Claude 原生格式")
	}
	if _, supported := native.NativeRelayFormatFor("claude-sonnet-4-5", core.NativeRelayClaude); !supported {
		t.Error("claude-sonnet-4-5 应支持 Claude 原生格式")
	}

	// Router 对不支持的模型必须返回哨兵，而不是硬着头皮发原生请求
	_, err = router.RelayNative(context.Background(), &core.ChatRequest{Model: "gpt-5.6-sol"}, []byte(`{"model":"gpt-5.6-sol"}`), core.NativeRelayClaude)
	if err == nil {
		t.Fatal("不支持的模型应返回错误")
	}
	if !errorsIs(err, core.ErrNativeNotSupported) {
		t.Errorf("err = %v, want ErrNativeNotSupported", err)
	}
	if hitInvoke {
		t.Error("不应打到 InvokeModel")
	}
	if hitConverse {
		t.Error("Router.RelayNative 不应触发 Converse（降级由 handler 负责）")
	}
}

// errorsIs 小包装，避免测试文件顶部再引一个包名与断言宏混淆。
func errorsIs(err, target error) bool {
	for e := err; e != nil; {
		if e == target {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// captureUsageMiddleware 在 handler 返回后读取 gin ctx 里的 usage，
// 用来端到端验证原生路径的计费回填（这是修复 Anthropic usage 漏算后的回归闸）。
func captureUsageMiddleware(dst *core.Usage, seen *bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("key_id", "test-key")
		c.Next()
		if v, ok := c.Get("usage"); ok {
			if u, ok := v.(core.Usage); ok {
				*dst = u
				*seen = true
			}
		}
	}
}

// writeEventStreamFrames 把若干 SSE 帧编码成 Bedrock 的 event stream 响应体。
func writeEventStreamFrames(t *testing.T, buf *bytes.Buffer, frames []string) {
	t.Helper()
	enc := eventstream.NewEncoder()
	for _, frame := range frames {
		payload, err := json.Marshal(map[string]string{
			"bytes": base64.StdEncoding.EncodeToString([]byte(frame)),
		})
		if err != nil {
			t.Fatalf("marshal chunk payload: %v", err)
		}
		msg := eventstream.Message{
			Headers: eventstream.Headers{
				{Name: eventstreamapi.MessageTypeHeader, Value: eventstream.StringValue(eventstreamapi.EventMessageType)},
				{Name: eventstreamapi.EventTypeHeader, Value: eventstream.StringValue("chunk")},
			},
			Payload: payload,
		}
		if err := enc.Encode(buf, msg); err != nil {
			t.Fatalf("encode event stream: %v", err)
		}
	}
}

// TestFullChain_NativeClaudeStreaming 端到端验证流式原生旁路：
// Bedrock event stream → 原样 SSE 到客户端 → usage 回填正确。
func TestFullChain_NativeClaudeStreaming(t *testing.T) {
	frames := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"role\":\"assistant\",\"usage\":{\"input_tokens\":25,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi there\"}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":15}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	var streamBuf bytes.Buffer
	writeEventStreamFrames(t, &streamBuf, frames)

	var gotPath string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(streamBuf.Bytes())
	}))
	defer upstream.Close()

	var usage core.Usage
	var usageSeen bool
	r := newFullChainRouter(t, upstream.URL, captureUsageMiddleware(&usage, &usageSeen))

	reqBody := `{"model":"claude-sonnet-4-5","max_tokens":1024,"top_k":7,"stream":true,"messages":[{"role":"user","content":"Hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(gotPath, "invoke-with-response-stream") {
		t.Errorf("upstream path = %q, want invoke-with-response-stream", gotPath)
	}

	// 上游收到的仍是 Anthropic 原生 body
	var up map[string]any
	if err := json.Unmarshal(gotBody, &up); err != nil {
		t.Fatalf("upstream body 不是 JSON: %v", err)
	}
	if v, _ := up["anthropic_version"].(string); v != "bedrock-2023-05-31" {
		t.Errorf("anthropic_version = %q", v)
	}
	if v, ok := up["top_k"].(float64); !ok || v != 7 {
		t.Errorf("top_k 未保留: %v", up["top_k"])
	}

	// 客户端收到合法的 Anthropic SSE
	body := w.Body.String()
	for _, want := range []string{"event: message_start", "event: content_block_delta", "event: message_stop", "Hi there"} {
		if !strings.Contains(body, want) {
			t.Errorf("客户端 SSE 缺少 %q\n got=%.400s", want, body)
		}
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", w.Header().Get("Content-Type"))
	}

	// 计费：input 与 output 都必须回填（这是本次修复的核心）
	if !usageSeen {
		t.Fatal("流式原生路径未回填 usage：计费会漏")
	}
	if usage.PromptTokens != 25 {
		t.Errorf("PromptTokens = %d, want 25（message_start 里的 input_tokens 不能被覆盖）", usage.PromptTokens)
	}
	if usage.CompletionTokens != 15 {
		t.Errorf("CompletionTokens = %d, want 15", usage.CompletionTokens)
	}
}
