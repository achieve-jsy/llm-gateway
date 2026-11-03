package handler_test

// messages legacy 转换路径的组内 failover 钉桩。
//
// 与 chat_failover_test.go 同源：messages 的 handleNonStream/handleStream 里
// 早已有 HandleFailover 调用，但此前没有任何测试触达。
// relaykit 路径（active）的 failover 由 messages_http_e2e_test.go 覆盖，
// 这里专测 policy 未配置（默认 legacy）时走的那条。

import (
	"context"
	"encoding/json"
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

// messagesFailoverOrch 首次失败、之后成功；不实现原生接口 → 强制走转换路径。
type messagesFailoverOrch struct {
	chatCalls int32
	seenModel []string
}

func (o *messagesFailoverOrch) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	n := atomic.AddInt32(&o.chatCalls, 1)
	o.seenModel = append(o.seenModel, req.Model)
	if n == 1 {
		return nil, &model.UnifiedError{HTTPStatus: http.StatusServiceUnavailable, Message: "overloaded"}
	}
	return &core.ChatResponse{
		ID:    "msg-ok",
		Model: req.Model,
		Choices: []core.Choice{{
			Index:   0,
			Message: core.ResponseMessage{Role: "assistant", Content: "ok"},
		}},
		Usage: core.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
	}, nil
}

func (o *messagesFailoverOrch) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return nil, &model.UnifiedError{HTTPStatus: http.StatusServiceUnavailable, Message: "nope"}
}

func TestMessagesLegacyFailover_RetriesOnRetryableError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"claude-group"})
	sch := &failoverScheduler{
		decision: &scheduler.RouteDecision{
			GroupName: "claude-group", ModelID: "m1", ModelName: "claude-haiku",
			ModelType: "chat", ProviderName: "an-a",
		},
		failTarget: &scheduler.RouteDecision{
			GroupName: "claude-group", ModelID: "m2", ModelName: "claude-haiku-backup",
			ModelType: "chat", ProviderName: "an-b",
		},
	}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)
	orch := &messagesFailoverOrch{}

	// 不设 protocolPolicy → mode=legacy → 走老路径（handleNonStream）
	h := handler.NewMessagesHandlerWithGroup(orch, gd)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("key_id", "test-key")
		c.Set("request_id", "req-msg")
	})
	r.POST("/v1/messages", h.HandleMessages)

	body := `{"model":"claude-group","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.EqualValues(t, 2, atomic.LoadInt32(&orch.chatCalls),
		"首跳 503 必须触发组内 failover 并重试一次")
	require.Len(t, orch.seenModel, 2)
	require.Equal(t, "claude-haiku-backup", orch.seenModel[1],
		"重试必须使用 failover 选出的备份模型")

	// 回程必须是 Anthropic 格式，不是内部 Chat 结构。
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "message", resp["type"], "legacy 路径必须返回 Anthropic message 结构")
	require.Contains(t, resp, "content")
}
