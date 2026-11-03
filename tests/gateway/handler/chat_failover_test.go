package handler_test

// chat 转换路径的组内 failover 钉桩。
//
// 这段代码（chat.go handleNonStream/handleStream 的 HandleFailover 调用）此前
// 没有任何测试触达 —— 它一直在跑，但没人验证过失败后真的会换渠道。
// 本测试补上：首跳 503 必须触发 failover，第二跳用备份模型并返回 200。

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// failOnceChatOrch 首次 Chat 失败、之后成功；不实现原生接口，强制走转换路径。
type failOnceChatOrch struct {
	chatCalls int32
	seenModel []string
}

func (o *failOnceChatOrch) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	n := atomic.AddInt32(&o.chatCalls, 1)
	o.seenModel = append(o.seenModel, req.Model)
	if n == 1 {
		return nil, &model.UnifiedError{HTTPStatus: http.StatusServiceUnavailable, Message: "overloaded"}
	}
	return &core.ChatResponse{
		ID:    "chat-ok",
		Model: req.Model,
		Choices: []core.Choice{{
			Index:   0,
			Message: core.ResponseMessage{Role: "assistant", Content: "ok"},
		}},
		Usage: core.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3},
	}, nil
}

func (o *failOnceChatOrch) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return nil, &model.UnifiedError{HTTPStatus: http.StatusServiceUnavailable, Message: "nope"}
}

func TestChatFailover_RetriesOnRetryableError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"chat-group"})
	sch := &failoverScheduler{
		decision: &scheduler.RouteDecision{
			GroupName: "chat-group", ModelID: "c1", ModelName: "gpt-4o-mini",
			ModelType: "chat", ProviderName: "ch-a",
		},
		failTarget: &scheduler.RouteDecision{
			GroupName: "chat-group", ModelID: "c2", ModelName: "gpt-4o-mini-backup",
			ModelType: "chat", ProviderName: "ch-b",
		},
	}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)
	orch := &failOnceChatOrch{}

	h := handler.NewChatHandlerWithGroup(orch, gd)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("key_id", "test-key")
		c.Set("request_id", "req-chat")
	})
	r.POST("/v1/chat/completions", h.Handle)

	body := `{"model":"chat-group","messages":[{"role":"user","content":"hi"}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.EqualValues(t, 2, atomic.LoadInt32(&orch.chatCalls),
		"首跳 503 必须触发组内 failover 并重试一次")
	require.Len(t, orch.seenModel, 2)
	require.Equal(t, "gpt-4o-mini-backup", orch.seenModel[1],
		"重试必须使用 failover 选出的备份模型")
}

// 不可 failover 的错误不应触发换渠道。
//
// 用 context.Canceled 是因为 shouldTriggerFailover 对它有**明确**的 false 分支
// （见 handler/shouldTriggerFailover）。注意 *model.UnifiedError 目前会落到
// 「未知错误类型保守触发」那一支——即 400 也会换一次渠道。这是既有设计，
// 本测试不去断言它，避免把有争议的行为固化成契约。
func TestChatFailover_NoRetryOnNonRetryableError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"chat-group"})
	sch := &failoverScheduler{
		decision: &scheduler.RouteDecision{
			GroupName: "chat-group", ModelID: "c1", ModelName: "gpt-4o-mini",
			ModelType: "chat", ProviderName: "ch-a",
		},
		failTarget: &scheduler.RouteDecision{
			GroupName: "chat-group", ModelID: "c2", ModelName: "backup",
			ModelType: "chat", ProviderName: "ch-b",
		},
	}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)

	orch := &canceledChatOrch{}
	h := handler.NewChatHandlerWithGroup(orch, gd)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("key_id", "test-key")
		c.Set("request_id", "req-chat-bad")
	})
	r.POST("/v1/chat/completions", h.Handle)

	body := `{"model":"chat-group","messages":[{"role":"user","content":"hi"}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.NotEqual(t, http.StatusOK, w.Code)
	require.EqualValues(t, 1, atomic.LoadInt32(&orch.calls),
		"context.Canceled 属于不可 failover 错误，不应重试")
}

type canceledChatOrch struct{ calls int32 }

func (o *canceledChatOrch) Chat(_ context.Context, _ *core.ChatRequest) (*core.ChatResponse, error) {
	atomic.AddInt32(&o.calls, 1)
	return nil, context.Canceled
}

func (o *canceledChatOrch) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	atomic.AddInt32(&o.calls, 1)
	return nil, context.Canceled
}

// alwaysFailChatOrch 每次都返回可 failover 的 503。
// 配合 failoverScheduler 永远给出新目标，若不设轮次上限就会无限重试下去。
type alwaysFailChatOrch struct{ calls int32 }

func (o *alwaysFailChatOrch) Chat(_ context.Context, _ *core.ChatRequest) (*core.ChatResponse, error) {
	atomic.AddInt32(&o.calls, 1)
	return nil, &model.UnifiedError{HTTPStatus: http.StatusServiceUnavailable, Message: "down"}
}

func (o *alwaysFailChatOrch) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	atomic.AddInt32(&o.calls, 1)
	return nil, &model.UnifiedError{HTTPStatus: http.StatusServiceUnavailable, Message: "down"}
}

// failover 必须有轮次上限。
//
// 组内模型数只是循环的天然边界，边界可能很大；一个请求会把整组挨个打一遍。
// 上限收口在 GroupAwareDispatcher.handleFailover，本测试通过"永远失败 + 永远
// 给出新目标"的桩把上限逼出来：3 轮 failover 意味着首次 + 3 次切换 = 4 次上游。
func TestChatFailover_RespectsMaxRounds(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"chat-group"})
	sch := &failoverScheduler{
		decision: &scheduler.RouteDecision{
			GroupName: "chat-group", ModelID: "c1", ModelName: "gpt-4o-mini",
			ModelType: "chat", ProviderName: "ch-a",
		},
		failTarget: &scheduler.RouteDecision{
			GroupName: "chat-group", ModelID: "c2", ModelName: "backup",
			ModelType: "chat", ProviderName: "ch-b",
		},
		// 让桩无视 excludeIDs，才能真正走到轮次上限那一支。
		endless: true,
	}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)
	orch := &alwaysFailChatOrch{}
	h := handler.NewChatHandlerWithGroup(orch, gd)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("key_id", "test-key")
		c.Set("request_id", "req-chat-cap")
	})
	r.POST("/v1/chat/completions", h.Handle)

	body := `{"model":"chat-group","messages":[{"role":"user","content":"hi"}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.NotEqual(t, http.StatusOK, w.Code)
	require.EqualValues(t, 4, atomic.LoadInt32(&orch.calls),
		"failover 上限 3 轮：首次 + 3 次切换 = 4 次上游调用，不能无限重试")
}
