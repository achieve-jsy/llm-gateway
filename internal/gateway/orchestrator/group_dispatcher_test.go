package orchestrator

import (
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type mockScheduler struct {
	routeDecision    *scheduler.RouteDecision
	routeErr         error
	failoverDecision *scheduler.RouteDecision
	failoverErr      error
}

func (m *mockScheduler) Route(groupName string) (*scheduler.RouteDecision, error) {
	return m.routeDecision, m.routeErr
}

func (m *mockScheduler) Failover(groupName, failedModelID string) (*scheduler.RouteDecision, error) {
	return m.failoverDecision, m.failoverErr
}

func newTestDispatcher() (*GroupAwareDispatcher, *mockScheduler, *cache.GroupNameCache) {
	sch := &mockScheduler{}
	nc := cache.NewGroupNameCache()
	return NewGroupAwareDispatcher(sch, nc), sch, nc
}

func TestGroupAwareDispatcher_Dispatch_NotGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	d, _, nc := newTestDispatcher()
	nc.SetNames(nil)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	decision, err := d.Dispatch(c, "gpt-4")

	assert.NoError(t, err)
	assert.Nil(t, decision)
}

func TestGroupAwareDispatcher_Dispatch_GroupRouteSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	d, sch, nc := newTestDispatcher()
	nc.SetNames([]string{"gpt-group"})

	sch.routeDecision = &scheduler.RouteDecision{
		GroupName: "gpt-group",
		ModelID:   "model-001",
		ModelName: "gpt-4",
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	decision, err := d.Dispatch(c, "gpt-group")

	assert.NoError(t, err)
	assert.NotNil(t, decision)
	assert.Equal(t, "model-001", decision.ModelID)

	dc := model.GetDispatchContext(c)
	assert.NotNil(t, dc)
	assert.Equal(t, "gpt-group", dc.GroupName)
	assert.Equal(t, "model-001", dc.RoutedModelID)
	assert.False(t, dc.FailoverAttempted)
}

func TestGroupAwareDispatcher_Dispatch_GroupRouteFailed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	d, sch, nc := newTestDispatcher()
	nc.SetNames([]string{"down-group"})

	sch.routeErr = model.ErrNoAvailableModel

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	decision, err := d.Dispatch(c, "down-group")

	assert.Error(t, err)
	assert.Nil(t, decision)
	assert.Equal(t, model.ErrNoAvailableModel, err)
}

func TestGroupAwareDispatcher_HandleFailover_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	d, sch, _ := newTestDispatcher()

	sch.failoverDecision = &scheduler.RouteDecision{
		GroupName: "gpt-group",
		ModelID:   "model-002",
		ModelName: "gpt-4-turbo",
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	model.SetDispatchContext(c, &model.DispatchContext{
		GroupName:     "gpt-group",
		RoutedModelID: "model-001",
	})

	decision, err := d.HandleFailover(c, "gpt-group", "model-001")

	assert.NoError(t, err)
	assert.NotNil(t, decision)
	assert.Equal(t, "model-002", decision.ModelID)

	dc := model.GetDispatchContext(c)
	assert.True(t, dc.FailoverAttempted)
	assert.Equal(t, "model-002", dc.RoutedModelID)
}

func TestGroupAwareDispatcher_HandleFailover_MaxRetries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	d, _, _ := newTestDispatcher()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	model.SetDispatchContext(c, &model.DispatchContext{
		GroupName:         "gpt-group",
		RoutedModelID:     "model-002",
		FailoverAttempted: true,
	})

	decision, err := d.HandleFailover(c, "gpt-group", "model-002")

	assert.Error(t, err)
	assert.Nil(t, decision)
	assert.Equal(t, model.ErrNoOtherModelAvailable, err)
}

func TestGroupAwareDispatcher_HandleFailover_SchedulerError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	d, sch, _ := newTestDispatcher()
	sch.failoverErr = model.ErrNoOtherModelAvailable

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	decision, err := d.HandleFailover(c, "gpt-group", "model-001")

	assert.Error(t, err)
	assert.Nil(t, decision)
	assert.Equal(t, model.ErrNoOtherModelAvailable, err)
}
