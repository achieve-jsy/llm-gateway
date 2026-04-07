package orchestrator

import (
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/authz"
	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
)

// maxFailoverRounds 已移除。failover 轮次由 triedIDs 自然收束：
// 每轮失败的模型加入排除列表，全部模型试过后 FailoverExcluding 返回 ErrNoOtherModel。

type GroupAwareDispatcher struct {
	scheduler    Scheduler
	nameCache    *cache.GroupNameCache
	bindingCheck *binding.Client
	tierResolver budget.TierResolver
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

func NewGroupAwareDispatcherWithBindingAndTier(
	sch Scheduler,
	nc *cache.GroupNameCache,
	bindClient *binding.Client,
	tierResolver budget.TierResolver,
) *GroupAwareDispatcher {
	return &GroupAwareDispatcher{
		scheduler:    sch,
		nameCache:    nc,
		bindingCheck: bindClient,
		tierResolver: tierResolver,
	}
}

func (d *GroupAwareDispatcher) Dispatch(c *gin.Context, modelName string, req *core.ChatRequest) (*scheduler.RouteDecision, error) {
	if !d.nameCache.IsGroupName(modelName) {
		return nil, nil
	}

	identityVal, exists := c.Get("identity")
	if exists {
		if id, ok := identityVal.(*model.UserIdentity); ok && id.KeyID != "" {
			if err := authz.AuthorizeGroupAccess(c.Request.Context(), id, d.bindingCheck, modelName); err != nil {
				return nil, err
			}
		}
	}

	if d.tierResolver != nil {
		if tier, ok := d.tierResolver.Resolve(modelName); ok {
			budget.SetTierContext(c, tier)
		}
	}

	decision, err := d.scheduler.Route(modelName, req)
	if err != nil {
		slog.Error("group route failed",
			"group_name", modelName,
			"error", err,
		)
		return nil, model.ErrNoAvailableModel
	}

	model.SetDispatchContext(c, &model.DispatchContext{
		GroupName:     modelName,
		RoutedModelID: decision.ModelID,
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

func containsGroup(groups []string, target string) bool {
	for _, g := range groups {
		if g == target {
			return true
		}
	}
	return false
}

func (d *GroupAwareDispatcher) HandleFailover(c *gin.Context, groupName, failedModelID string, req *core.ChatRequest) (*scheduler.RouteDecision, error) {
	dc := model.GetDispatchContext(c)

	rounds := 0
	var triedIDs []string
	if dc != nil {
		rounds = dc.FailoverRounds
		triedIDs = dc.TriedModelIDs
	}

	// 将本轮失败的模型加入已尝试列表（去重）
	if !containsID(triedIDs, failedModelID) {
		triedIDs = append(triedIDs, failedModelID)
	}

	decision, err := d.scheduler.FailoverExcluding(groupName, triedIDs, req)
	if err != nil {
		slog.Error("group failover failed",
			"group_name", groupName,
			"failed_model_id", failedModelID,
			"tried_ids", triedIDs,
			"error", err,
		)
		return nil, model.ErrNoOtherModelAvailable
	}

	if dc != nil {
		dc.RoutedModelID = decision.ModelID
		dc.FailoverRounds = rounds + 1
		dc.TriedModelIDs = triedIDs
	} else {
		model.SetDispatchContext(c, &model.DispatchContext{
			GroupName:      groupName,
			RoutedModelID:  decision.ModelID,
			FailoverRounds: rounds + 1,
			TriedModelIDs:  triedIDs,
		})
	}

	c.Set("routed_model", decision.ModelName)
	c.Set("routed_model_id", decision.ModelID)
	if decision.ProviderName != "" {
		c.Set("provider", decision.ProviderName)
	}

	slog.Info("group failover succeeded",
		"group_name", groupName,
		"round", rounds+1,
		"new_model_id", decision.ModelID,
		"tried_ids", triedIDs,
	)

	return decision, nil
}

// NotifyFailure 通知 scheduler 指定模型发生故障，触发健康探针提前检测。
func (d *GroupAwareDispatcher) NotifyFailure(modelID string) {
	d.scheduler.NotifyFailure(modelID)
}

// SetCooldown 为因 429 限流的模型设置冷却期，冷却期内不参与路由。
func (d *GroupAwareDispatcher) SetCooldown(modelID string, duration time.Duration) {
	d.scheduler.SetCooldown(modelID, duration)
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
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
