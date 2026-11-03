package orchestrator

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
)

// videoSchedulerStub 只实现 Route（其余方法在本用例中不会被调用）。
type videoSchedulerStub struct {
	routeErr error
}

func (s *videoSchedulerStub) Route(string, *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, s.routeErr
}
func (s *videoSchedulerStub) Failover(string, string, *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, errors.New("not implemented")
}
func (s *videoSchedulerStub) FailoverExcluding(string, []string, *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, errors.New("not implemented")
}
func (s *videoSchedulerStub) GetGroupConfig(string) *scheduler.GroupRoutingConfig { return nil }
func (s *videoSchedulerStub) NotifyFailure(string)                                {}
func (s *videoSchedulerStub) SetCooldown(string, time.Duration)                    {}
func (s *videoSchedulerStub) PersistSticky(context.Context, string, string, *scheduler.RouteDecision) {
}
func (s *videoSchedulerStub) UpdateSticky(context.Context, string, string, string, string) bool {
	return false
}

// TestDispatchMapsNoVideoCapableModel 组内没有视频成员时必须返回
// video_input_not_supported（400），而不是"无可用模型"。
func TestDispatchMapsNoVideoCapableModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"text-only-group"})
	d := NewGroupAwareDispatcher(&videoSchedulerStub{routeErr: scheduler.ErrNoVideoCapableModel}, nc)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	_, err := d.Dispatch(c, "text-only-group", nil)
	if !errors.Is(err, model.ErrVideoInputNotSupported) {
		t.Fatalf("err = %v, want model.ErrVideoInputNotSupported", err)
	}
	var unified *model.UnifiedError
	if !errors.As(err, &unified) {
		t.Fatalf("err is not *model.UnifiedError: %T", err)
	}
	if unified.HTTPStatus != 400 || unified.Code != "video_input_not_supported" {
		t.Fatalf("status/code = %d/%q, want 400/video_input_not_supported", unified.HTTPStatus, unified.Code)
	}
}
