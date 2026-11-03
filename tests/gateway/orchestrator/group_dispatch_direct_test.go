package orchestrator_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestDispatch_DirectModelRejectedWhenTierEnabled 覆盖漏洞修复核心：
// 启用档位预算（tierResolver != nil）时，直连（非组名）模型请求必须被 403 拒绝，
// 而不能走 (nil, nil) 直连路径绕过档位预算闸门。
func TestDispatch_DirectModelRejectedWhenTierEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache()
	nc.SetNames([]string{"g1"})
	resolver := &countingTierResolver{}
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, resolver)
	ctx, _ := makeContext(&model.UserIdentity{
		KeyID:        "key-1",
		UsableGroups: []string{"g1"},
	})

	decision, err := dispatcher.Dispatch(ctx, "4476-some-channel", nil)
	require.ErrorIs(t, err, model.ErrDirectModelNotAllowed)
	require.Nil(t, decision)
	require.Zero(t, resolver.calls) // 拒绝发生在 tier 解析之前，Resolve 不应被调用
	_, ok := budget.GetTierContext(ctx)
	require.False(t, ok)
}

// TestDispatch_DirectModelAllowedWhenTierDisabled 保证未启用档位预算（tierResolver == nil）
// 的 legacy/测试构造仍保持原 (nil, nil) 直连行为，不引入额外限制。
func TestDispatch_DirectModelAllowedWhenTierDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache()
	nc.SetNames([]string{"g1"})
	dispatcher := orchestrator.NewGroupAwareDispatcher(sch, nc)
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1"})

	decision, err := dispatcher.Dispatch(ctx, "4476-some-channel", nil)
	require.NoError(t, err)
	require.Nil(t, decision)
}

// TestDispatch_EmptyModelNotRejected 空 model 应放行给下游原有的 unknown_model（404）处理，
// 不能退化为 403 direct_model_not_allowed。
func TestDispatch_EmptyModelNotRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache()
	nc.SetNames([]string{"g1"})
	resolver := &countingTierResolver{}
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, resolver)
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1"})

	decision, err := dispatcher.Dispatch(ctx, "", nil)
	require.NoError(t, err)
	require.Nil(t, decision)
}

// TestDispatch_GroupNameStillRoutes 组名请求在启用档位预算时仍正常路由并设置 tier context。
func TestDispatch_GroupNameStillRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache()
	nc.SetNames([]string{"g1"})
	resolver := &countingTierResolver{}
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, resolver)
	ctx, _ := makeContext(&model.UserIdentity{
		KeyID:           "key-1",
		AccessAllGroups: true,
		UsableGroups:    []string{"g1"},
	})

	decision, err := dispatcher.Dispatch(ctx, "g1", nil)
	require.NoError(t, err)
	require.NotNil(t, decision)
	tier, ok := budget.GetTierContext(ctx)
	require.True(t, ok)
	require.Equal(t, "tier-1", tier.TierID)
}
