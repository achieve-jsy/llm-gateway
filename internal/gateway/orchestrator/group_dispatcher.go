package orchestrator

import (
	"log/slog"

	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
)

const maxFailoverRetries = 1

type GroupAwareDispatcher struct {
	scheduler Scheduler
	nameCache *cache.GroupNameCache
}

func NewGroupAwareDispatcher(sch Scheduler, nc *cache.GroupNameCache) *GroupAwareDispatcher {
	return &GroupAwareDispatcher{
		scheduler: sch,
		nameCache: nc,
	}
}

func (d *GroupAwareDispatcher) Dispatch(c *gin.Context, modelName string) (*scheduler.RouteDecision, error) {
	if !d.nameCache.IsGroupName(modelName) {
		return nil, nil
	}

	decision, err := d.scheduler.Route(modelName)
	if err != nil {
		slog.Error("group route failed",
			"group_name", modelName,
			"error", err,
		)
		return nil, model.ErrNoAvailableModel
	}

	model.SetDispatchContext(c, &model.DispatchContext{
		GroupName:         modelName,
		RoutedModelID:     decision.ModelID,
		FailoverAttempted: false,
	})

	c.Set("group_name", modelName)
	c.Set("routed_model_id", decision.ModelID)

	return decision, nil
}

func (d *GroupAwareDispatcher) HandleFailover(c *gin.Context, groupName, failedModelID string) (*scheduler.RouteDecision, error) {
	dc := model.GetDispatchContext(c)
	if dc != nil && dc.FailoverAttempted {
		return nil, model.ErrNoOtherModelAvailable
	}

	decision, err := d.scheduler.Failover(groupName, failedModelID)
	if err != nil {
		slog.Error("group failover failed",
			"group_name", groupName,
			"failed_model_id", failedModelID,
			"error", err,
		)
		return nil, model.ErrNoOtherModelAvailable
	}

	if dc != nil {
		dc.RoutedModelID = decision.ModelID
		dc.FailoverAttempted = true
	} else {
		model.SetDispatchContext(c, &model.DispatchContext{
			GroupName:         groupName,
			RoutedModelID:     decision.ModelID,
			FailoverAttempted: true,
		})
	}

	c.Set("routed_model_id", decision.ModelID)

	slog.Info("group failover succeeded",
		"group_name", groupName,
		"new_model_id", decision.ModelID,
		"failed_model_id", failedModelID,
	)

	return decision, nil
}
