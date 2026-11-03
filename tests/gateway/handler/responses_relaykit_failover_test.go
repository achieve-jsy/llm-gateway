package handler_test

// responses relaykit 路径的 failover 对齐测试。
//
// 背景：/v1/responses 有两条转换路径 —— legacy（自带 failover）和 relaykit（原本单次调用、
// 无 failover）。policy=active 时原生失败会降级到 relaykit，等于「配了 active 反而失去组内容错」。
// 这里把 relaykit 路径的 failover 语义钉住，与 /v1/messages 的 relaykit 路径保持一致。

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// failoverScheduler 首跳给 modelA，Failover 给 modelB。
type failoverScheduler struct {
	decision   *scheduler.RouteDecision
	failTarget *scheduler.RouteDecision

	// endless 让 pick 无视 excludeIDs 且每次返回全新的模型 ID。
	// 只用于验证 handler 的 failover 轮次上限：正常情况下 pick 靠 excludeIDs
	// 保证有界，那样就永远走不到上限那一支。
	endless    bool
	endlessSeq int32
}

func (f *failoverScheduler) Route(_ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.decision, nil
}

// pick 必须尊重 excludeIDs（= 已尝试过的模型 ID）。
// 真实 GroupAwareDispatcher 用它保证 failover 有界；fake 若无视它，
// 一个「总是失败」的 orchestrator 会让 handler 的 for 循环无限打上游。
func (f *failoverScheduler) pick(excludeIDs []string) (*scheduler.RouteDecision, error) {
	if f.failTarget == nil {
		return nil, model.ErrNoOtherModelAvailable
	}
	if f.endless {
		n := atomic.AddInt32(&f.endlessSeq, 1)
		d := *f.failTarget
		d.ModelID = fmt.Sprintf("endless-%d", n)
		return &d, nil
	}
	for _, id := range excludeIDs {
		if id == f.failTarget.ModelID {
			return nil, model.ErrNoOtherModelAvailable
		}
	}
	return f.failTarget, nil
}

func (f *failoverScheduler) Failover(_, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.pick(nil)
}

func (f *failoverScheduler) FailoverExcluding(_ string, excludeIDs []string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.pick(excludeIDs)
}

func (f *failoverScheduler) FailoverExcludingVideo(_ string, excludeIDs []string, _ bool, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.pick(excludeIDs)
}

func (f *failoverScheduler) GetGroupConfig(_ string) *scheduler.GroupRoutingConfig { return nil }
func (f *failoverScheduler) NotifyFailure(_ string)                                {}
func (f *failoverScheduler) SetCooldown(_ string, _ time.Duration)                 {}
func (f *failoverScheduler) PersistSticky(_ context.Context, _, _ string, _ *scheduler.RouteDecision) {
}
func (f *failoverScheduler) UpdateSticky(_ context.Context, _, _, _, _ string) bool { return false }

// failOnceResponsesOrch 第一次 Chat 返回可 failover 错误，之后成功。
// 同时实现原生入口并统一返回哨兵，让请求落到 relaykit 转换路径。
type failOnceResponsesOrch struct {
	chatCalls int32
	rawBodies [][]byte
}

func (o *failOnceResponsesOrch) NativeRelayFormatFor(_ string, _ core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	return core.NativeRelayOpenAIResponses, true
}

func (o *failOnceResponsesOrch) RelayNative(_ context.Context, _ []byte) ([]byte, error) {
	return nil, core.ErrNativeNotSupported
}

func (o *failOnceResponsesOrch) RelayNativeStream(_ context.Context, _ []byte) (io.ReadCloser, error) {
	return nil, core.ErrNativeNotSupported
}

func (o *failOnceResponsesOrch) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	n := atomic.AddInt32(&o.chatCalls, 1)
	o.rawBodies = append(o.rawBodies, []byte(req.Model))
	if n == 1 {
		// 503 + 可 failover 语义：shouldTriggerFailover 认定的可重试错误。
		return nil, &model.UnifiedError{HTTPStatus: http.StatusServiceUnavailable, Message: "upstream overloaded"}
	}
	return &core.ChatResponse{
		ID:    "resp-ok",
		Model: req.Model,
		Choices: []core.Choice{{
			Index:   0,
			Message: core.ResponseMessage{Role: "assistant", Content: "ok"},
		}},
		Usage: core.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
	}, nil
}

func (o *failOnceResponsesOrch) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return nil, &model.UnifiedError{HTTPStatus: http.StatusServiceUnavailable, Message: "nope"}
}

func (o *failOnceResponsesOrch) Responses(_ context.Context, _ *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return nil, model.ErrInternal
}

func (o *failOnceResponsesOrch) ResponsesStream(_ context.Context, _ *core.ResponsesRequest) (io.ReadCloser, error) {
	return nil, model.ErrInternal
}

// TestResponsesRelaykitPath_FailoverOnRetryableError 断言 relaykit 路径会触发组内 failover。
func TestResponsesRelaykitPath_FailoverOnRetryableError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"gpt-group"})
	sch := &failoverScheduler{
		decision: &scheduler.RouteDecision{
			GroupName: "gpt-group", ModelID: "m1", ModelName: "gpt-4o-mini",
			ModelType: "chat", ProviderName: "openai-a",
		},
		failTarget: &scheduler.RouteDecision{
			GroupName: "gpt-group", ModelID: "m2", ModelName: "gpt-4o-mini-backup",
			ModelType: "chat", ProviderName: "openai-b",
		},
	}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)
	orch := &failOnceResponsesOrch{}

	p, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: "active",
		PerKey:      map[string]string{"test-key": "active"},
	})
	require.NoError(t, err)

	h := handler.NewResponsesHandlerWithGroup(orch, gd)
	// relaykit 路径要求 chatOrchestrator 非空（原生旁路与转换都走它）；不设会静默落到老路径。
	h.SetChatOrchestrator(orch)
	h.SetProtocolPolicy(p)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("key_id", "test-key")
		c.Set("request_id", "req-test")
	})
	r.POST("/v1/responses", h.Handle)

	body := `{"model":"gpt-group","input":"hi"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.EqualValues(t, 2, atomic.LoadInt32(&orch.chatCalls),
		"relaykit 路径必须重试一次：首跳失败后经 HandleFailover 换渠道再试")
	require.Len(t, orch.rawBodies, 2)
	require.Equal(t, "gpt-4o-mini-backup", string(orch.rawBodies[1]),
		"重试必须使用 failover 选出的新模型")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
}
