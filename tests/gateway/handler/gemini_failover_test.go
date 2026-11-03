package handler_test

// Gemini 降级路径的 failover 对齐测试。
//
// 四条入站的降级路径现在都应在「建立上游请求失败」时换渠道重试：
// chat（老管线）、messages（rk）、responses（rk）、gemini（gelaykit 转换）。
// gemini 早年是有意的单次尝试（ponytail 标记），本次拉齐。

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type geminiFailoverScheduler struct {
	decision   *scheduler.RouteDecision
	failTarget *scheduler.RouteDecision
}

func (f *geminiFailoverScheduler) Route(_ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.decision, nil
}
func (f *geminiFailoverScheduler) Failover(_, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.failTarget, nil
}
func (f *geminiFailoverScheduler) FailoverExcluding(_ string, _ []string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.failTarget, nil
}
func (f *geminiFailoverScheduler) FailoverExcludingVideo(_ string, _ []string, _ bool, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.failTarget, nil
}
func (f *geminiFailoverScheduler) GetGroupConfig(_ string) *scheduler.GroupRoutingConfig { return nil }
func (f *geminiFailoverScheduler) NotifyFailure(_ string)                                {}
func (f *geminiFailoverScheduler) SetCooldown(_ string, _ time.Duration)                 {}
func (f *geminiFailoverScheduler) PersistSticky(_ context.Context, _, _ string, _ *scheduler.RouteDecision) {
}
func (f *geminiFailoverScheduler) UpdateSticky(_ context.Context, _, _, _, _ string) bool {
	return false
}

// geminiFailOnceOrch 原生返回哨兵（强制走转换），Chat 首次失败后成功。
type geminiFailOnceOrch struct {
	chatCalls int32
	seenModel []string
}

func (o *geminiFailOnceOrch) NativeRelayFormatFor(_ string, _ core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	return core.NativeRelayGemini, true
}
func (o *geminiFailOnceOrch) RelayNative(_ context.Context, _ []byte) ([]byte, error) {
	return nil, core.ErrNativeNotSupported
}
func (o *geminiFailOnceOrch) RelayNativeStream(_ context.Context, _ []byte) (io.ReadCloser, error) {
	return nil, core.ErrNativeNotSupported
}

func (o *geminiFailOnceOrch) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	n := atomic.AddInt32(&o.chatCalls, 1)
	o.seenModel = append(o.seenModel, req.Model)
	if n == 1 {
		return nil, &model.UnifiedError{HTTPStatus: http.StatusServiceUnavailable, Message: "overloaded"}
	}
	return &core.ChatResponse{
		ID:    "gemini-ok",
		Model: req.Model,
		Choices: []core.Choice{{
			Index:   0,
			Message: core.ResponseMessage{Role: "assistant", Content: "ok"},
		}},
		Usage: core.Usage{PromptTokens: 4, CompletionTokens: 1, TotalTokens: 5},
	}, nil
}

func (o *geminiFailOnceOrch) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return nil, &model.UnifiedError{HTTPStatus: http.StatusServiceUnavailable, Message: "nope"}
}

func TestGeminiDegrade_FailoverOnRetryableError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"gemini-group"})
	sch := &geminiFailoverScheduler{
		decision: &scheduler.RouteDecision{
			GroupName: "gemini-group", ModelID: "g1", ModelName: "gemini-2.5-flash",
			ModelType: "chat", ProviderName: "google-a",
		},
		failTarget: &scheduler.RouteDecision{
			GroupName: "gemini-group", ModelID: "g2", ModelName: "gemini-2.5-flash-backup",
			ModelType: "chat", ProviderName: "google-b",
		},
	}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)
	orch := &geminiFailOnceOrch{}

	h := handler.NewGeminiHandlerWithGroup(orch, gd)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("key_id", "test-key")
		c.Set("request_id", "req-gemini")
	})
	r.POST("/v1beta/models/:modelAction", h.Handle)

	body := `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-group:generateContent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.EqualValues(t, 2, atomic.LoadInt32(&orch.chatCalls),
		"降级路径必须重试一次：首跳失败后经 HandleFailover 换渠道再试")
	require.Len(t, orch.seenModel, 2)
	require.Equal(t, "gemini-2.5-flash-backup", orch.seenModel[1],
		"重试必须使用 failover 选出的新模型")
}
