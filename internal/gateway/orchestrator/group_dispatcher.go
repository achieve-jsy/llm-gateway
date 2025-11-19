package orchestrator

import (
	"log/slog"

	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
)

const maxFailoverRetries = 1

type GroupAwareDispatcher struct {
	scheduler    Scheduler
	nameCache    *cache.GroupNameCache
	bindingCheck *binding.Client
}

func NewGroupAwareDispatcher(sch Scheduler, nc *cache.GroupNameCache) *GroupAwareDispatcher {
	return &GroupAwareDispatcher{
		scheduler:    sch,
		nameCache:    nc,
		bindingCheck: nil,
	}
}

func NewGroupAwareDispatcherWithBinding(sch Scheduler, nc *cache.GroupNameCache, bindClient *binding.Client) *GroupAwareDispatcher {
	return &GroupAwareDispatcher{
		scheduler:    sch,
		nameCache:    nc,
		bindingCheck: bindClient,
	}
}

func (d *GroupAwareDispatcher) Dispatch(c *gin.Context, modelName string) (*scheduler.RouteDecision, error) {
	if !d.nameCache.IsGroupName(modelName) {
		return nil, nil
	}

	if d.bindingCheck != nil {
		identityVal, exists := c.Get("identity")
		if exists {
			if id, ok := identityVal.(*model.UserIdentity); ok && id.KeyID != "" && !id.AccessAllGroups {
				groups, err := d.bindingCheck.GetKeyGroups(c.Request.Context(), id.KeyID)
				if err != nil {
					slog.Error("group dispatch: binding check failed",
						"group_name", modelName,
						"key_id", id.KeyID,
						"error", err,
					)
					return nil, model.ErrModelGroupUnavailable
				}
				if !binding.ContainsGroup(groups, modelName) {
					return nil, model.ErrModelGroupUnavailable
				}
			}
		}
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
	c.Set("routed_model", decision.ModelName) // models.name，用于日志/计价展示
	c.Set("routed_model_id", decision.ModelID)
	c.Set("routed_via_group", true) // 标记经过了组调度，前端据此显示 ↳

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

	c.Set("routed_model", decision.ModelName) // failover 后更新路由模型名
	c.Set("routed_model_id", decision.ModelID)

	slog.Info("group failover succeeded",
		"group_name", groupName,
		"new_model_id", decision.ModelID,
		"failed_model_id", failedModelID,
	)

	return decision, nil
}