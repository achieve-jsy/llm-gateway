// Package llmclient_test 覆盖 internal/llmclient 的出站重试策略（媒体通道 A6）：
// per-call「禁止重试」选项（提交类请求必须至多一次）以及默认重试语义的回归保护。
//
// 测试手段：httptest 假上游 / 计数 RoundTripper 精确统计「真实发出了几次请求」，
// 并把退避压到毫秒级，使默认路径也能观察到 maxRetries+1 次尝试。
package llmclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/llmclientconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// derivedCtxKey 用于模拟 provider 在透传 ctx 时附加自己的 context value。
type derivedCtxKey struct{}

// testMaxRetries 是测试用的最大重试次数。默认（未传禁止重试选项）路径下
// llmclient 会发出 maxRetries+1 = 4 次请求，这正是回归用例要断言的「既有次数」。
const testMaxRetries = 3

// testRetryConfig 返回「退避极短、无抖动、重试 testMaxRetries 次」的重试配置，
// 让测试既能观察到默认的 4 次尝试，又不真的等待 1s+ 的指数退避。
func testRetryConfig() llmclientconfig.RetryConfig {
	return llmclientconfig.RetryConfig{
		MaxRetries:     testMaxRetries,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     2 * time.Millisecond,
		BackoffFactor:  1,
		JitterFactor:   0,
	}
}

// testClientConfig 生成测试用配置：
//  1. 使用上面的短退避重试配置；
//  2. FailureThreshold=0 关闭熔断——否则连续失败会把熔断打开，
//     之后的请求会在发出前被拒绝（503），干扰「实际发出几次」的计数。
func testClientConfig(baseURL string) llmclient.Config {
	cfg := llmclient.DefaultConfig("openai", baseURL)
	cfg.Retry = testRetryConfig()
	cfg.CircuitBreaker = llmclientconfig.CircuitBreakerConfig{}
	return cfg
}

// countingStatusServer 启动一个固定返回 status 的假上游，并累加真实收到的请求数。
func countingStatusServer(t *testing.T, status int) (*httptest.Server, *int32) {
	t.Helper()
	calls := new(int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream unavailable","type":"server_error"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

// countingErrTransport 是一个只返回固定错误的 RoundTripper，
// 用于模拟「请求根本没拿到响应」的传输层失败（连接重置、超时），并精确统计发出的请求数。
type countingErrTransport struct {
	calls int32
	err   error
}

func (t *countingErrTransport) RoundTrip(*http.Request) (*http.Response, error) {
	atomic.AddInt32(&t.calls, 1)
	return nil, t.err
}

func (t *countingErrTransport) count() int32 { return atomic.LoadInt32(&t.calls) }

// newTransportClient 构造一个传输层必定失败的 llmclient（上游地址不可达，不会真的联网）。
func newTransportClient(err error) (*llmclient.Client, *countingErrTransport) {
	tr := &countingErrTransport{err: err}
	return llmclient.NewWithHTTPClient(&http.Client{Transport: tr}, testClientConfig("http://upstream.invalid"), nil), tr
}

// withNoRetryMode 按模式施加「禁止重试」入口，覆盖 ctx 选项、Request 字段以及同时设置三种情况。
func withNoRetryMode(mode string) (context.Context, bool) {
	ctx := context.Background()
	if mode == "ctx选项" || mode == "两者都设置" {
		ctx = llmclient.WithNoRetry(ctx)
	}
	return ctx, mode == "Request字段" || mode == "两者都设置"
}

// TestDoRaw_NoRetry_SendsExactlyOneRequest 验证 A6 的核心要求：
// 传入「禁止重试」（ctx 选项 / Request.NoRetry / 两者）后，上游连续返回 429 或 5xx 时
// **恰好只发出 1 次**上游请求——媒体提交类接口重发会在上游产生第二个任务并重复计费。
func TestDoRaw_NoRetry_SendsExactlyOneRequest(t *testing.T) {
	statuses := []struct {
		name        string
		description string
		status      int
	}{
		{name: "429限流", description: "上游持续 429：禁止重试必须压掉 isRetryable 的重发", status: http.StatusTooManyRequests},
		{name: "502上游错误", description: "上游持续 502：禁止重试必须压掉 isRetryable 的重发", status: http.StatusBadGateway},
		{name: "503上游不可用", description: "上游持续 503：禁止重试必须压掉 isRetryable 的重发", status: http.StatusServiceUnavailable},
		{name: "504上游网关超时", description: "上游持续 504（HTTP 响应状态，非 client timeout）：同样只发 1 次", status: http.StatusGatewayTimeout},
	}
	modes := []string{"ctx选项", "Request字段", "两者都设置"}

	for _, tt := range statuses {
		for _, mode := range modes {
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				srv, calls := countingStatusServer(t, tt.status)
				client := llmclient.NewWithHTTPClient(srv.Client(), testClientConfig(srv.URL), nil)

				ctx, noRetry := withNoRetryMode(mode)
				resp, err := client.DoRaw(ctx, llmclient.Request{
					Method:   http.MethodPost,
					Endpoint: "/v1/videos/text2video",
					Body:     map[string]any{"model": "kling-v1", "prompt": "a cat"},
					NoRetry:  noRetry,
				})

				// 上游报错，调用方仍需拿到错误（提交失败不能静默成功）。
				require.Error(t, err, "上游 %d 时 DoRaw 必须返回错误", tt.status)
				assert.Nil(t, resp)
				// 关键证据：真实发出的请求次数恰好 1。
				got := atomic.LoadInt32(calls)
				t.Logf("计数证据：上游 %d，模式=%s，实际收到 %d 次请求（期望 1）", tt.status, mode, got)
				assert.Equal(t, int32(1), got, "禁止重试时上游请求次数必须恰好为 1")

				// 错误语义不变：仍携带上游状态码，便于上层映射与计费判定。
				var gwErr *core.GatewayError
				require.ErrorAs(t, err, &gwErr)
				assert.Equal(t, tt.status, gwErr.StatusCode, "错误必须保留上游状态码")
			})
		}
	}
}

// TestDo_NoRetry_SendsExactlyOneRequest 验证最高层入口 Do（DoRaw + JSON 反序列化）同样遵守该选项。
func TestDo_NoRetry_SendsExactlyOneRequest(t *testing.T) {
	srv, calls := countingStatusServer(t, http.StatusBadGateway)
	client := llmclient.NewWithHTTPClient(srv.Client(), testClientConfig(srv.URL), nil)

	var out map[string]any
	err := client.Do(llmclient.WithNoRetry(context.Background()), llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/v1/images/generations",
		Body:     map[string]any{"model": "gpt-image-1", "prompt": "a cat"},
	}, &out)

	require.Error(t, err)
	t.Logf("计数证据：Do 入口（禁止重试）实际发出 %d 次请求（期望 1）", atomic.LoadInt32(calls))
	assert.Equal(t, int32(1), atomic.LoadInt32(calls), "Do 入口同样必须恰好只发 1 次请求")
}

// TestDoRaw_DefaultRetries_Unchanged 是默认行为的回归保护：
// 不传任何「禁止重试」入口时，chat 等既有路径的重试次数仍是 maxRetries+1（此处 4 次）。
func TestDoRaw_DefaultRetries_Unchanged(t *testing.T) {
	tests := []struct {
		name        string
		description string
		endpoint    string
		status      int
	}{
		{
			name:        "chat路径502仍重试",
			description: "不传选项时 /chat/completions 的 POST 仍按配置重发 4 次，证明 A6 未改动既有 chat 语义",
			endpoint:    "/chat/completions",
			status:      http.StatusBadGateway,
		},
		{
			name:        "chat路径429仍重试",
			description: "不传选项时 /chat/completions 的 429 仍按配置重发 4 次",
			endpoint:    "/chat/completions",
			status:      http.StatusTooManyRequests,
		},
		{
			name:        "媒体端点未传选项仍重试",
			description: "禁止重试是显式开关而非路径嗅探：同一个媒体端点不传选项时行为与今天一致（4 次）",
			endpoint:    "/v1/videos/text2video",
			status:      http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := countingStatusServer(t, tt.status)
			client := llmclient.NewWithHTTPClient(srv.Client(), testClientConfig(srv.URL), nil)

			_, err := client.DoRaw(context.Background(), llmclient.Request{
				Method:   http.MethodPost,
				Endpoint: tt.endpoint,
				Body:     map[string]any{"model": "gpt-4o"},
			})

			require.Error(t, err)
			got := atomic.LoadInt32(calls)
			t.Logf("计数证据：%s 未传选项，实际发出 %d 次请求（期望 %d，= maxRetries+1）", tt.endpoint, got, testMaxRetries+1)
			assert.Equal(t, int32(testMaxRetries+1), got,
				"默认路径的重试次数必须仍是 maxRetries+1=4（回归保护）")
		})
	}
}

// TestDoPassthrough_NoRetry 验证 passthrough 路径：
// GET 查询类请求默认会重试（媒体任务轮询依赖它），传了禁止重试后降为恰好 1 次。
func TestDoPassthrough_NoRetry(t *testing.T) {
	// 默认路径（回归）：GET + 503 → 重发 4 次后把上游响应原样返回（err == nil）。
	t.Run("GET默认重试4次", func(t *testing.T) {
		srv, calls := countingStatusServer(t, http.StatusServiceUnavailable)
		client := llmclient.NewWithHTTPClient(srv.Client(), testClientConfig(srv.URL), nil)

		resp, err := client.DoPassthrough(context.Background(), llmclient.Request{
			Method:   http.MethodGet,
			Endpoint: "/v1/videos/task-1",
		})
		require.NoError(t, err, "passthrough 保留非 200 响应，不返回错误")
		require.NotNil(t, resp)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		t.Logf("计数证据：GET 未传选项，实际发出 %d 次请求（期望 %d）", atomic.LoadInt32(calls), testMaxRetries+1)
		assert.Equal(t, int32(testMaxRetries+1), atomic.LoadInt32(calls), "GET 默认重试 maxRetries+1 次")
	})

	// 禁止重试：同一个 GET 只发 1 次，且上游响应照旧原样透出。
	t.Run("GET禁止重试只发1次", func(t *testing.T) {
		srv, calls := countingStatusServer(t, http.StatusServiceUnavailable)
		client := llmclient.NewWithHTTPClient(srv.Client(), testClientConfig(srv.URL), nil)

		resp, err := client.DoPassthrough(llmclient.WithNoRetry(context.Background()), llmclient.Request{
			Method:   http.MethodGet,
			Endpoint: "/v1/videos/task-1",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		t.Logf("计数证据：GET 传禁止重试，实际发出 %d 次请求（期望 1）", atomic.LoadInt32(calls))
		assert.Equal(t, int32(1), atomic.LoadInt32(calls), "禁止重试时 GET 也必须恰好 1 次")
	})

	// 媒体提交类 POST 在 passthrough 上本就不重放（方法不幂等且无 Idempotency-Key），此处锁定该既有语义。
	t.Run("POST默认不重放", func(t *testing.T) {
		srv, calls := countingStatusServer(t, http.StatusBadGateway)
		client := llmclient.NewWithHTTPClient(srv.Client(), testClientConfig(srv.URL), nil)

		resp, err := client.DoPassthrough(context.Background(), llmclient.Request{
			Method:   http.MethodPost,
			Endpoint: "/v1/videos/text2video",
			RawBody:  []byte(`{"model":"kling-v1"}`),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		_ = resp.Body.Close()
		t.Logf("计数证据：passthrough POST 未传选项，实际发出 %d 次请求（期望 1，本就不可重放）", atomic.LoadInt32(calls))
		assert.Equal(t, int32(1), atomic.LoadInt32(calls), "非幂等 POST 在 passthrough 上本来就不重放")
	})
}

// TestDoRaw_TransportError_NoRetry 验证网络错误（非超时，如连接被重置）这一重试来源：
// 默认会重发 4 次，传了禁止重试后恰好 1 次。
func TestDoRaw_TransportError_NoRetry(t *testing.T) {
	t.Run("默认重试4次", func(t *testing.T) {
		client, tr := newTransportClient(errors.New("connection reset by peer"))

		_, err := client.DoRaw(context.Background(), llmclient.Request{
			Method:   http.MethodPost,
			Endpoint: "/v1/videos/text2video",
			Body:     map[string]any{"model": "kling-v1"},
		})

		require.Error(t, err)
		t.Logf("计数证据：网络错误未传选项，实际发出 %d 次请求（期望 %d）", tr.count(), testMaxRetries+1)
		assert.Equal(t, int32(testMaxRetries+1), tr.count(), "网络错误默认重试 maxRetries+1 次")
	})

	t.Run("禁止重试只发1次", func(t *testing.T) {
		client, tr := newTransportClient(errors.New("connection reset by peer"))

		_, err := client.DoRaw(llmclient.WithNoRetry(context.Background()), llmclient.Request{
			Method:   http.MethodPost,
			Endpoint: "/v1/videos/text2video",
			Body:     map[string]any{"model": "kling-v1"},
		})

		require.Error(t, err)
		t.Logf("计数证据：网络错误传禁止重试，实际发出 %d 次请求（期望 1）", tr.count())
		assert.Equal(t, int32(1), tr.count(), "禁止重试时网络错误也必须恰好 1 次")
	})
}

// TestDoRaw_Timeout_IsTerminal 记录并锁定既有事实：
// client 超时（context.DeadlineExceeded / net.Error.Timeout）在 DoRaw 中是**终止性分支**
// （client.go isClientTimeoutGatewayError → 直接返回），无论有没有禁止重试选项都只发 1 次。
// 也就是说当前实现里不存在「超时重发」这条路径；本用例证明 A6 没有改变超时语义。
func TestDoRaw_Timeout_IsTerminal(t *testing.T) {
	tests := []struct {
		name        string
		description string
		withNoRetry bool
	}{
		{
			name:        "默认超时只发1次",
			description: "超时错误被归一为 504 终止分支，默认配置下也不重发",
			withNoRetry: false,
		},
		{
			name:        "禁止重试+超时只发1次",
			description: "禁止重试选项不改变超时的终止语义，仍是 1 次",
			withNoRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, tr := newTransportClient(context.DeadlineExceeded)

			ctx := context.Background()
			if tt.withNoRetry {
				ctx = llmclient.WithNoRetry(ctx)
			}
			_, err := client.DoRaw(ctx, llmclient.Request{
				Method:   http.MethodPost,
				Endpoint: "/v1/videos/text2video",
				Body:     map[string]any{"model": "kling-v1"},
			})

			require.Error(t, err)
			t.Logf("计数证据：超时（withNoRetry=%v），实际发出 %d 次请求（期望 1）", tt.withNoRetry, tr.count())
			assert.Equal(t, int32(1), tr.count(), "超时是终止性失败，只允许发出 1 次请求")

			var gwErr *core.GatewayError
			require.ErrorAs(t, err, &gwErr)
			assert.Equal(t, http.StatusGatewayTimeout, gwErr.StatusCode, "超时统一归一为 504")
		})
	}
}

// TestIsNoRetry_DerivedContextKeepsMark 验证标记随 ctx 派生存活：
// provider 到 llmclient 之间会经过 WithValue/WithTimeout 等派生，标记不能在途中丢失。
func TestIsNoRetry_DerivedContextKeepsMark(t *testing.T) {
	assert.False(t, llmclient.IsNoRetry(nil), "nil ctx 不应携带标记")
	assert.False(t, llmclient.IsNoRetry(context.Background()), "普通 ctx 默认不带标记")

	ctx := llmclient.WithNoRetry(context.Background())
	assert.True(t, llmclient.IsNoRetry(ctx))

	// 模拟 provider 侧常见的派生（携带超时/附加 value）后标记仍在。
	derived, cancel := context.WithTimeout(context.WithValue(ctx, derivedCtxKey{}, "v"), time.Second)
	defer cancel()
	assert.True(t, llmclient.IsNoRetry(derived), "派生 ctx 必须保留禁止重试标记")
}
