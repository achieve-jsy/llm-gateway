package handler_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 Batch 1 · B7：**提交类请求不得走 handler 层组内 failover**。
//
// 背景：chat.go 的 failover 循环在 429/5xx/网络错误时会把**同一个请求体**发给组内
// 另一个成员。对提交类请求（媒体提交、任务创建）：换一个上游 = 再建一次 + 重复计费。
// llmclient 的 WithNoRetry 只覆盖「单次上游调用内部的重试」，覆盖不到这一层。
//
// 门控判据：llmclient.IsNoRetry(ctx)（标记由 submit 路径写在请求 context 上）。
//
// 验收（任务书原文）：
//   - 提交路径 429 时组内第二个渠道**零请求**；
//   - 查询路径仍 failover。

// countingChatOrchestrator 记录每次上游调用的目标模型，并返回可控错误。
type countingChatOrchestrator struct {
	mu       sync.Mutex
	calls    []string
	resp     *core.ChatResponse
	err      error
	respFunc func(model string) (*core.ChatResponse, error)
}

func (o *countingChatOrchestrator) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	model := ""
	if req != nil {
		model = req.Model
	}
	o.mu.Lock()
	o.calls = append(o.calls, model)
	o.mu.Unlock()
	if o.respFunc != nil {
		return o.respFunc(model)
	}
	return o.resp, o.err
}

func (o *countingChatOrchestrator) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return nil, o.err
}

func (o *countingChatOrchestrator) callCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.calls)
}

func (o *countingChatOrchestrator) calledModels() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, len(o.calls))
	copy(out, o.calls)
	return out
}

// failoverTrackingScheduler 记录 FailoverExcluding 被调用的次数（= 换渠道的次数）。
type failoverTrackingScheduler struct {
	failoverCalls int
	// secondDecision 是组内第二个成员的决策（若被调用即意味着「换了渠道」）。
	secondDecision *scheduler.RouteDecision
}

func (f *failoverTrackingScheduler) Route(_ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return &scheduler.RouteDecision{
		GroupName:    "g",
		ModelID:      "model-1",
		ModelName:    "upstream-1",
		ProviderName: "channel-1",
	}, nil
}

func (f *failoverTrackingScheduler) Failover(_, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.FailoverExcluding("", nil, nil)
}

func (f *failoverTrackingScheduler) FailoverExcluding(_ string, _ []string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	f.failoverCalls++
	if f.secondDecision == nil {
		return nil, core.NewNotFoundError("no other model")
	}
	return f.secondDecision, nil
}

func (f *failoverTrackingScheduler) GetGroupConfig(_ string) *scheduler.GroupRoutingConfig {
	return nil
}
func (f *failoverTrackingScheduler) NotifyFailure(_ string)                {}
func (f *failoverTrackingScheduler) SetCooldown(_ string, _ time.Duration) {}
func (f *failoverTrackingScheduler) PersistSticky(_ context.Context, _, _ string, _ *scheduler.RouteDecision) {
}
func (f *failoverTrackingScheduler) UpdateSticky(_ context.Context, _, _, _, _ string) bool {
	return false
}

// rateLimitUnifiedError 返回 handler 能识别的 429（UnifiedError 带 HTTPStatus）。
func rateLimitUnifiedError() error {
	return &model.UnifiedError{
		Type:       "rate_limit_error",
		Code:       "rate_limit_error",
		Message:    "rate limited",
		HTTPStatus: http.StatusTooManyRequests,
	}
}

func newFailoverTestHandler(t *testing.T, orch handler.ChatOrchestratorInterface, sch *failoverTrackingScheduler) *gin.Engine {
	t.Helper()
	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"g"})
	dispatcher := orchestrator.NewGroupAwareDispatcher(sch, nc)

	h := handler.NewChatHandlerWithGroup(orch, dispatcher)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 模拟「提交路径已经标记 NoRetry」：在 handler 之前把标记写到请求 context 上。
	// 真实调用方（媒体/任务创建链路）在进入 handler 前用 llmclient.WithNoRetry(ctx) 标记。
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		if c.GetHeader("X-Test-No-Retry") == "1" {
			c.Request = c.Request.WithContext(llmclient.WithNoRetry(c.Request.Context()))
		}
		h.Handle(c)
	})
	return router
}

func TestB7_SubmitPath_DoesNotFailoverOn429(t *testing.T) {
	calls := 0
	orch := &countingChatOrchestrator{
		respFunc: func(string) (*core.ChatResponse, error) {
			calls++
			return nil, rateLimitUnifiedError()
		},
	}
	sch := &failoverTrackingScheduler{secondDecision: &scheduler.RouteDecision{
		ModelID: "model-2", ModelName: "upstream-2", ProviderName: "channel-2",
	}}
	router := newFailoverTestHandler(t, orch, sch)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"g","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-No-Retry", "1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	t.Run("组内第二个渠道零请求", func(t *testing.T) {
		assert.Equal(t, 1, orch.callCount(), "提交路径 429 时只能调用一次上游")
		assert.Equal(t, []string{"upstream-1"}, orch.calledModels(),
			"第二个渠道（upstream-2）必须零请求：换上游 = 再建一次 + 重复计费")
	})

	t.Run("failover 决策一次都没做", func(t *testing.T) {
		assert.Equal(t, 0, sch.failoverCalls, "NoRetry 请求不得进入 HandleFailover")
	})

	t.Run("429 原样返回给调用方", func(t *testing.T) {
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Equal(t, 1, calls)
	})
}

func TestB7_QueryPath_StillFailovers(t *testing.T) {
	// 对照组：没有 NoRetry 标记（查询/轮询类路径）必须**保留** failover。
	attempted := 0
	orch := &countingChatOrchestrator{
		respFunc: func(model string) (*core.ChatResponse, error) {
			attempted++
			if model == "upstream-1" {
				return nil, rateLimitUnifiedError()
			}
			return &core.ChatResponse{ID: "ok", Model: model}, nil
		},
	}
	sch := &failoverTrackingScheduler{secondDecision: &scheduler.RouteDecision{
		ModelID: "model-2", ModelName: "upstream-2", ProviderName: "channel-2",
	}}
	router := newFailoverTestHandler(t, orch, sch)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"g","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, sch.failoverCalls, "查询路径必须保留 failover")
	assert.Equal(t, 2, orch.callCount())
	assert.Equal(t, []string{"upstream-1", "upstream-2"}, orch.calledModels())
}

func TestB7_SubmitPath_DoesNotFailoverOn5xx(t *testing.T) {
	orch := &countingChatOrchestrator{
		respFunc: func(string) (*core.ChatResponse, error) {
			return nil, core.NewProviderError("channel-1", http.StatusInternalServerError, "boom", nil)
		},
	}
	sch := &failoverTrackingScheduler{secondDecision: &scheduler.RouteDecision{
		ModelID: "model-2", ModelName: "upstream-2", ProviderName: "channel-2",
	}}
	router := newFailoverTestHandler(t, orch, sch)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"g","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-No-Retry", "1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 1, orch.callCount(), "5xx 同样不得换上游重发")
	assert.Equal(t, 0, sch.failoverCalls)
	assert.Equal(t, []string{"upstream-1"}, orch.calledModels())
}

func TestB7_SubmitPath_DoesNotFailoverOnNetworkError(t *testing.T) {
	orch := &countingChatOrchestrator{
		respFunc: func(string) (*core.ChatResponse, error) {
			return nil, io.ErrUnexpectedEOF
		},
	}
	sch := &failoverTrackingScheduler{secondDecision: &scheduler.RouteDecision{
		ModelID: "model-2", ModelName: "upstream-2", ProviderName: "channel-2",
	}}
	router := newFailoverTestHandler(t, orch, sch)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"g","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-No-Retry", "1")
	httptest.NewRecorder()
	router.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, 1, orch.callCount(), "网络错误同样不得换上游重发")
	assert.Equal(t, 0, sch.failoverCalls)
}

func TestB7_SubmitPath_SucceedsFirstTry(t *testing.T) {
	// NoRetry 只影响失败后的 failover，不影响首轮成功。
	orch := &countingChatOrchestrator{
		respFunc: func(model string) (*core.ChatResponse, error) {
			return &core.ChatResponse{ID: "ok", Model: model}, nil
		},
	}
	sch := &failoverTrackingScheduler{}
	router := newFailoverTestHandler(t, orch, sch)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"g","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-No-Retry", "1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, orch.callCount())
	assert.Equal(t, 0, sch.failoverCalls)
}

// TestB7_SubmitPath_RegressionGuard_SecondChannelIsNeverReached 是「第二个渠道零请求」的
// **抗变异**版本：组内第二个成员**会成功**，因此只要门控失效（failover 真的发生），
// 请求就会变成 200/upstream-2，断言必然失败 —— 不会像「第二个成员也 429」那样
// 走进 failover 死循环掩盖问题。
func TestB7_SubmitPath_RegressionGuard_SecondChannelIsNeverReached(t *testing.T) {
	orch := &countingChatOrchestrator{
		respFunc: func(model string) (*core.ChatResponse, error) {
			if model == "upstream-1" {
				return nil, rateLimitUnifiedError()
			}
			return &core.ChatResponse{ID: "should-never-happen", Model: model}, nil
		},
	}
	sch := &failoverTrackingScheduler{secondDecision: &scheduler.RouteDecision{
		ModelID: "model-2", ModelName: "upstream-2", ProviderName: "channel-2",
	}}
	router := newFailoverTestHandler(t, orch, sch)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"g","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-No-Retry", "1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code, "提交路径必须把首轮 429 直接返回")
	assert.Equal(t, []string{"upstream-1"}, orch.calledModels())
	assert.Equal(t, 0, sch.failoverCalls)
}
