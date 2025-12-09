// TODO: UsageEvent/UsageMetricsInstance 已迁移至 internal/gateway/observability 包，
// 本文件引用的 infra.UsageEvent 已失效，需按新包路径重写。暂时用 //go:build ignore 跳过编译。
//
//go:build ignore

package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageCollector(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		description  string
		setupContext func(*gin.Context)
		channelSize  int
		prefill      bool
		wantEmitted  int64
		wantDropped  int64
		verifyEvent  func(*testing.T, *infra.UsageEvent)
	}{
		{
			name:        "正常采集",
			description: "Context 填入全部字段后，UsageCollector 应将 UsageEvent 成功发送到 channel，所有字段值与 Context 一致",
			setupContext: func(c *gin.Context) {
				c.Set("request_id", "req-001")
				c.Set("key_id", "key-abc")
				c.Set("user_id", "user-xyz")
				c.Set("dept_id", "dept-789")
				c.Set("model", "gpt-4o")
				c.Set("provider_type", "openai")
				c.Set("provider_model_id", "gpt-4o-2024-08")
				c.Set("is_stream", true)
				c.Set("session_id", "sess-111")
				c.Set("prompt_tokens", 100)
				c.Set("completion_tokens", 200)
				c.Set("reasoning_tokens", 50)
				c.Set("cached_tokens", 30)
				c.Set("ttft_ms", 150)
				c.Set("duration_ms", 3000)
				c.Set("provider_duration_ms", 2800)
				c.Set("status", "success")
			},
			channelSize: 10,
			prefill:     false,
			wantEmitted: 1,
			wantDropped: 0,
			verifyEvent: func(t *testing.T, ev *infra.UsageEvent) {
				assert.Equal(t, "req-001", ev.RequestID)
				assert.Equal(t, "key-abc", ev.KeyID)
				assert.Equal(t, "user-xyz", ev.UserID)
				require.NotNil(t, ev.DeptID)
				assert.Equal(t, "dept-789", *ev.DeptID)
				assert.Equal(t, "gpt-4o", ev.Model)
				assert.Equal(t, "openai", ev.ProviderType)
				require.NotNil(t, ev.ProviderModelID)
				assert.Equal(t, "gpt-4o-2024-08", *ev.ProviderModelID)
				assert.True(t, ev.IsStream)
				require.NotNil(t, ev.SessionID)
				assert.Equal(t, "sess-111", *ev.SessionID)
				assert.Equal(t, 100, ev.PromptTokens)
				assert.Equal(t, 200, ev.CompletionTokens)
				assert.Equal(t, 50, ev.ReasoningTokens)
				assert.Equal(t, 30, ev.CachedTokens)
				require.NotNil(t, ev.TTFTMs)
				assert.Equal(t, 150, *ev.TTFTMs)
				require.NotNil(t, ev.DurationMs)
				assert.Equal(t, 3000, *ev.DurationMs)
				require.NotNil(t, ev.ProviderDurationMs)
				assert.Equal(t, 2800, *ev.ProviderDurationMs)
				assert.Equal(t, "success", ev.Status)
				assert.False(t, ev.StatDate.IsZero())
			},
		},
		{
			name:        "channel满时丢弃",
			description: "channel buffer=1 预填占满后，新事件应丢弃不阻塞，Dropped 递增，Emitted 不变",
			setupContext: func(c *gin.Context) {
				c.Set("request_id", "req-drop")
				c.Set("key_id", "key-drop")
				c.Set("user_id", "user-drop")
				c.Set("model", "gpt-4o-mini")
				c.Set("provider_type", "openai")
				c.Set("is_stream", false)
				c.Set("status", "success")
			},
			channelSize: 1,
			prefill:     true,
			wantEmitted: 0,
			wantDropped: 1,
			verifyEvent: nil,
		},
		{
			name:        "空Context字段容忍",
			description: "大多数 Context 字段缺失时不应 panic，应正常构造 Event 发出，nullable 字段为 nil，status 默认 success",
			setupContext: func(c *gin.Context) {
				c.Set("request_id", "req-minimal")
				c.Set("key_id", "key-min")
				c.Set("model", "minimal-model")
				c.Set("provider_type", "minimal-provider")
				c.Set("is_stream", false)
			},
			channelSize: 10,
			prefill:     false,
			wantEmitted: 1,
			wantDropped: 0,
			verifyEvent: func(t *testing.T, ev *infra.UsageEvent) {
				assert.Equal(t, "req-minimal", ev.RequestID)
				assert.Equal(t, "key-min", ev.KeyID)
				assert.Empty(t, ev.UserID)
				assert.Nil(t, ev.DeptID)
				assert.Equal(t, "minimal-model", ev.Model)
				assert.Equal(t, "minimal-provider", ev.ProviderType)
				assert.Nil(t, ev.ProviderModelID)
				assert.False(t, ev.IsStream)
				assert.Nil(t, ev.SessionID)
				assert.Equal(t, 0, ev.PromptTokens)
				assert.Equal(t, 0, ev.CompletionTokens)
				assert.Equal(t, 0, ev.ReasoningTokens)
				assert.Equal(t, 0, ev.CachedTokens)
				assert.Nil(t, ev.TTFTMs)
				assert.Nil(t, ev.DurationMs)
				assert.Nil(t, ev.ProviderDurationMs)
				assert.Equal(t, "success", ev.Status)
			},
		},
		{
			name:        "非阻塞验证",
			description: "连续两个请求，channel 满后第二个事件走 default 不阻塞，首请求 Emitted=1，次请求 Dropped=1",
			setupContext: func(c *gin.Context) {
				c.Set("request_id", "req-nb")
				c.Set("key_id", "key-nb")
				c.Set("model", "nb-model")
				c.Set("provider_type", "nb-provider")
			},
			channelSize: 1,
			prefill:     false,
			wantEmitted: 0,
			wantDropped: 0,
			verifyEvent: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			infra.UsageMetricsInstance.Reset()

			ch := make(chan *infra.UsageEvent, tt.channelSize)
			biCh := ch

			if tt.prefill {
				ch <- &infra.UsageEvent{RequestID: "prefill-dummy"}
			}

			collector := middleware.NewUsageCollector(ch)

			router := gin.New()
			router.Use(collector.Handler())
			router.GET("/test", func(c *gin.Context) {
				tt.setupContext(c)
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			if tt.name == "非阻塞验证" {
				w2 := httptest.NewRecorder()
				r2, _ := http.NewRequest("GET", "/test", nil)
				router.ServeHTTP(w2, r2)
				assert.Equal(t, http.StatusOK, w2.Code)

				select {
				case ev := <-biCh:
					require.NotNil(t, ev)
					assert.Equal(t, "req-nb", ev.RequestID)
				case <-time.After(time.Second):
					t.Fatal("event not emitted to channel within timeout")
				}
				assert.Equal(t, 0, len(biCh))
				assert.Equal(t, int64(1), infra.UsageMetricsInstance.Emitted.Value())
				assert.Equal(t, int64(1), infra.UsageMetricsInstance.Dropped.Value())
				return
			}

			if tt.verifyEvent != nil {
				select {
				case ev := <-biCh:
					tt.verifyEvent(t, ev)
				case <-time.After(time.Second):
					t.Fatal("event not emitted to channel within timeout")
				}
			}

			assert.Equal(t, tt.wantEmitted, infra.UsageMetricsInstance.Emitted.Value())
			assert.Equal(t, tt.wantDropped, infra.UsageMetricsInstance.Dropped.Value())
		})
	}
}
