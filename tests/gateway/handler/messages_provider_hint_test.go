package handler_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturingMessagesOrchestrator 记录转发上游前的 ChatRequest，用于断言 provider hint。
type capturingMessagesOrchestrator struct {
	lastReq *core.ChatRequest
	resp    *core.ChatResponse
}

func (f *capturingMessagesOrchestrator) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	if req != nil {
		cp := *req
		f.lastReq = &cp
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &core.ChatResponse{
		ID:     "msg_test",
		Object: "chat.completion",
		Model:  req.Model,
		Choices: []core.Choice{{
			Index:        0,
			FinishReason: "stop",
			Message:      core.ResponseMessage{Role: "assistant", Content: "ok"},
		}},
	}, nil
}

func (f *capturingMessagesOrchestrator) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

type fakeMessagesScheduler struct {
	decision *scheduler.RouteDecision
}

func (f *fakeMessagesScheduler) Route(_ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.decision, nil
}

func (f *fakeMessagesScheduler) Failover(_, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, model.ErrNoOtherModelAvailable
}

func (f *fakeMessagesScheduler) FailoverExcluding(_ string, _ []string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, model.ErrNoOtherModelAvailable
}

func (f *fakeMessagesScheduler) FailoverExcludingVideo(_ string, _ []string, _ bool, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.FailoverExcluding("", nil, nil)
}

func (f *fakeMessagesScheduler) GetGroupConfig(_ string) *scheduler.GroupRoutingConfig {
	return nil
}

func (f *fakeMessagesScheduler) NotifyFailure(_ string) {}

func (f *fakeMessagesScheduler) SetCooldown(_ string, _ time.Duration) {}

func (f *fakeMessagesScheduler) PersistSticky(_ context.Context, _, _ string, _ *scheduler.RouteDecision) {
}

func (f *fakeMessagesScheduler) UpdateSticky(_ context.Context, _, _, _, _ string) bool { return false }

func TestMessagesHandler_SetsProviderHintFromGroupDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"claude-opus-4-6"})
	sch := &fakeMessagesScheduler{
		decision: &scheduler.RouteDecision{
			GroupName:    "claude-opus-4-6",
			ModelID:      "66",
			ModelName:    "global.anthropic.claude-opus-4-6-v1",
			ProviderName: "aws-0799",
		},
	}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)
	orch := &capturingMessagesOrchestrator{}

	router := gin.New()
	h := handler.NewMessagesHandlerWithGroup(orch, gd)
	router.POST("/v1/messages", h.HandleMessages)

	body := `{"model":"claude-opus-4-6","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.NotNil(t, orch.lastReq)
	assert.Equal(t, "global.anthropic.claude-opus-4-6-v1", orch.lastReq.Model)
	assert.Equal(t, "aws-0799", orch.lastReq.Provider,
		"messages 首跳必须把 scheduler 选定的渠道实例名写入 req.Provider，避免同名模型走裸 ID 先注册者")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
}
