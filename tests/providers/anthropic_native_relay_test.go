package providers_test

// anthropic 渠道的原生旁路测试。
//
// 背景：/v1/messages 入站是 Anthropic 格式，anthropic 渠道的上游也是 Anthropic 格式，
// 两者协议完全相同 —— 本应零转换直发，但此前 anthropic 没有实现 NativeRelayProvider，
// 请求被迫走 Claude → Chat → Claude 两次转换，thinking 签名 / cache_control /
// metadata 这些 Anthropic 独有字段会在往返中丢失。
//
// 注意 anthropic 的 baseURL 自带 /v1 前缀，端点写 /messages（不是 /v1/messages）。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/anthropic"
	"github.com/stretchr/testify/require"
)

func newAnthropicForTest(t *testing.T, baseURL string) *anthropic.Provider {
	t.Helper()
	p, ok := anthropic.New(providers.ProviderConfig{
		APIKey:  "test-key",
		BaseURL: baseURL,
	}, providers.ProviderOptions{}).(*anthropic.Provider)
	require.True(t, ok, "anthropic.New 应返回 *anthropic.Provider")
	return p
}

// 编译期断言：anthropic 必须实现原生旁路契约。
func TestAnthropicProvider_ImplementsNativeRelayProvider(t *testing.T) {
	p := newAnthropicForTest(t, "http://127.0.0.1:1/v1")
	var _ core.NativeRelayProvider = p
}

// anthropic 渠道只原生支持 Claude 协议；其余格式必须拒绝，交由转换路径处理。
func TestAnthropicProvider_NativeFormatMatrix(t *testing.T) {
	p := newAnthropicForTest(t, "http://127.0.0.1:1/v1")

	got, ok := p.NativeRelayFormatFor("claude-sonnet-4-5", core.NativeRelayClaude)
	require.True(t, ok, "anthropic 必须声明支持 Claude 原生")
	require.Equal(t, core.NativeRelayClaude, got)

	for _, f := range []core.NativeRelayFormat{
		core.NativeRelayOpenAIChat,
		core.NativeRelayOpenAIResponses,
		core.NativeRelayGemini,
	} {
		_, ok := p.NativeRelayFormatFor("claude-sonnet-4-5", f)
		require.False(t, ok, "anthropic 不该声明支持 %s —— 上游只有 /v1/messages", f)
	}
}

// 非流式：body 必须原样发到 /v1/messages，响应原样回传。
func TestAnthropicProvider_RelayNativePostsRawBodyToMessages(t *testing.T) {
	var gotPath, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("x-api-key")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","thinking":{"signature":"sig-abc"}}`))
	}))
	defer srv.Close()

	p := newAnthropicForTest(t, srv.URL+"/v1")

	// 含 Anthropic 独有字段：如果走转换路径，signature 会丢。
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"thinking":{"type":"enabled","signature":"sig-abc"},"messages":[{"role":"user","content":"hi"}]}`)

	out, err := p.RelayNative(context.Background(), body)
	require.NoError(t, err)

	require.Equal(t, "/v1/messages", gotPath,
		"anthropic 的 messages 端点必须在 /v1 前缀下（baseURL 自带 /v1，端点写 /messages）")
	require.Equal(t, string(body), gotBody, "原生旁路必须原样转发 body，一个字段都不能改")
	require.Equal(t, "test-key", gotAuth, "鉴权头必须照常带上")

	require.Contains(t, string(out), "sig-abc", "响应必须原样回传，不做清洗")
}

// 流式：返回可读的原始 SSE 流。
func TestAnthropicProvider_RelayNativeStream(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n"))
	}))
	defer srv.Close()

	p := newAnthropicForTest(t, srv.URL+"/v1")
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	stream, err := p.RelayNativeStream(context.Background(), body)
	require.NoError(t, err)
	defer stream.Close()

	raw, err := io.ReadAll(stream)
	require.NoError(t, err)
	require.Equal(t, "/v1/messages", gotPath)
	require.Contains(t, string(raw), "message_start", "原始 SSE 帧必须透传")
}

// 上游非 2xx 必须返回真实错误，而不是伪装成 ErrNativeNotSupported
// （否则 handler 会降级并对同一请求二次打上游 + 重复计费）。
func TestAnthropicProvider_RelayNativeUpstreamErrorNotSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	p := newAnthropicForTest(t, srv.URL+"/v1")
	body := []byte(`{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)

	_, err := p.RelayNative(context.Background(), body)
	require.Error(t, err)
	require.False(t, errorsIs(err, core.ErrNativeNotSupported),
		"429 不能被包装成降级哨兵，否则会对同一请求二次打上游")
}

// errorsIs 避免直接 import errors 只为一次判断。
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

var _ = json.Marshal
var _ = llmclient.Hooks{}
var _ = strings.TrimSpace
