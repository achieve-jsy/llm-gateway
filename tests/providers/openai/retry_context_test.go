// 本文件验证 A6 的 per-call「禁止重试」标记能穿过**真实 provider 栈**
// （handler ctx → provider → llmclient）：媒体 handler 只需在自己的 ctx 上打标记，
// 无需修改任何 provider 实现，也不会影响其它调用方（不打标记 = 既有语义）。
package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCountingProvider 构造一个指向假上游的 OpenAI provider：
// 退避压到毫秒级（让默认路径的 4 次尝试仍瞬间完成），并关闭熔断以免干扰请求计数。
func newCountingProvider(t *testing.T, srv *httptest.Server) core.Provider {
	t.Helper()
	return openai.New(
		providers.ProviderConfig{APIKey: "test-key", BaseURL: srv.URL},
		providers.ProviderOptions{
			Resilience: providers.ResilienceConfig{
				Retry: providers.RetryConfig{
					MaxRetries:     3,
					InitialBackoff: time.Millisecond,
					MaxBackoff:     2 * time.Millisecond,
					BackoffFactor:  1,
					JitterFactor:   0,
				},
				// FailureThreshold=0 → 不启用熔断，避免连续失败后请求在发出前被拒而干扰计数。
				CircuitBreaker: providers.CircuitBreakerConfig{},
			},
		},
	)
}

// TestChatCompletion_NoRetryContext_PropagatesToUpstream 验证 ctx 标记在真实 provider 栈上的传播：
// 上游持续 502 时，打标记的调用恰好只发出 1 次请求，不打标记的调用仍是 maxRetries+1=4 次。
func TestChatCompletion_NoRetryContext_PropagatesToUpstream(t *testing.T) {
	tests := []struct {
		name        string
		description string
		noRetry     bool
		wantCalls   int32
	}{
		{
			name:        "未打标记_默认重试",
			description: "不打标记时 chat 路径仍按既有语义重试 maxRetries+1=4 次（回归保护）",
			noRetry:     false,
			wantCalls:   4,
		},
		{
			name:        "ctx打过标记_恰好1次",
			description: "handler 仅在 ctx 上打标记（provider 代码零改动），上游 502 即降为恰好 1 次",
			noRetry:     true,
			wantCalls:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := new(int32)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(calls, 1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error":{"message":"upstream unavailable","type":"server_error"}}`))
			}))
			t.Cleanup(srv.Close)

			p := newCountingProvider(t, srv)

			ctx := context.Background()
			if tt.noRetry {
				// 这正是 Batch 1 媒体 handler 的用法：提交前给 ctx 打标记。
				ctx = llmclient.WithNoRetry(ctx)
			}

			resp, err := p.ChatCompletion(ctx, &core.ChatRequest{
				Model:    "gpt-4o",
				Messages: []core.Message{{Role: "user", Content: "hello"}},
			})

			require.Error(t, err, "上游 502 时必须返回错误")
			assert.Nil(t, resp)
			got := atomic.LoadInt32(calls)
			t.Logf("计数证据：noRetry=%v，provider 栈实际发出 %d 次上游请求（期望 %d）", tt.noRetry, got, tt.wantCalls)
			assert.Equal(t, tt.wantCalls, got, "上游请求次数不符合预期")
		})
	}
}
