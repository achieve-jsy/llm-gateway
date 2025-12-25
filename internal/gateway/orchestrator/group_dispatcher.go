package orchestrator

import (
	"log/slog"
	"time"

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

	// v0.7: 基于 usable_groups 快照鉴权（取代 key_group_bindings 手动绑定）。
	// 授权天花板 = public ∪ 本部门已开通 controlled，由 verify-key 快照下发。
	// access_all_groups=true → 可用 usable_groups 全集；
	// access_all_groups=false → 仅 usable_groups ∩ 手动绑定子集（兼容过渡期）。
	identityVal, exists := c.Get("identity")
	if exists {
		if id, ok := identityVal.(*model.UserIdentity); ok {
			// 有 usable_groups 快照：使用 v0.7 授权逻辑
			if len(id.UsableGroups) > 0 {
				if !containsStr(id.UsableGroups, modelName) {
					slog.Warn("group dispatch: group not in usable_groups",
						"group_name", modelName,
						"key_id", id.KeyID,
					)
					return nil, model.ErrModelGroupUnavailable
				}
				// usable_groups 已通过，若 access_all_groups=false 还需检查手动绑定子集
				if !id.AccessAllGroups && d.bindingCheck != nil && id.KeyID != "" {
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
			} else if d.bindingCheck != nil && id.KeyID != "" && !id.AccessAllGroups {
				// 无 usable_groups 快照（旧版缓存兼容）：退回原有 key_group_bindings 逻辑
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
	c.Set("routed_model", decision.ModelName)
	c.Set("routed_model_id", decision.ModelID)
	c.Set("routed_via_group", true)
	if decision.ProviderName != "" {
		c.Set("provider", decision.ProviderName)
	}

	return decision, nil
}

// containsStr 判断 slice 中是否包含指定字符串。
func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
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

	c.Set("routed_model", decision.ModelName)
	c.Set("routed_model_id", decision.ModelID)
	if decision.ProviderName != "" {
		c.Set("provider", decision.ProviderName)
	}

	slog.Info("group failover succeeded",
		"group_name", groupName,
		"new_model_id", decision.ModelID,
		"failed_model_id", failedModelID,
	)

	return decision, nil
}

// GetGroupTimeout 返回组的自定义超时时长，0 表示使用系统默认值。
// defaultTimeout 是调用方传入的系统默认值。
func (d *GroupAwareDispatcher) GetGroupTimeout(groupName string, defaultTimeout time.Duration) time.Duration {
	cfg := d.scheduler.GetGroupConfig(groupName)
	if cfg != nil && cfg.TimeoutSeconds > 0 {
		return time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	return defaultTimeout
}