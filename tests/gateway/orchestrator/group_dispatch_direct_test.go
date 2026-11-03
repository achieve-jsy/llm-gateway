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
	nc := cache.NewGroupNameCache("")
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
// 的 legacy/测试构造在**组名缓存已就绪**时仍保持原 (nil, nil) 直连行为，不引入额外限制。
// 注意：未就绪判断先于 tierResolver 判定，因此「tierResolver == nil 且缓存未就绪」返回 503
// 而不是 (nil, nil)——见 TestDispatch_TierDisabledButCacheNotReady。
func TestDispatch_DirectModelAllowedWhenTierDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"g1"})
	dispatcher := orchestrator.NewGroupAwareDispatcher(sch, nc)
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1"})

	decision, err := dispatcher.Dispatch(ctx, "4476-some-channel", nil)
	require.NoError(t, err)
	require.Nil(t, decision)
}

// TestDispatch_TierDisabledButCacheNotReady 固化「未就绪判断先于 tier 判定」的组合边界：
// 即使 tierResolver == nil（legacy/测试构造），缓存未就绪时非组名请求仍返回 503
// group_cache_not_ready——因为此时无法区分「真的直连模型」与「尚未加载的合法组名」。
func TestDispatch_TierDisabledButCacheNotReady(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache("") // 故意不 SetNames：未就绪
	require.False(t, nc.IsInitialized())
	dispatcher := orchestrator.NewGroupAwareDispatcher(sch, nc)
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1"})

	decision, err := dispatcher.Dispatch(ctx, "4476-some-channel", nil)
	require.ErrorIs(t, err, model.ErrGroupCacheNotReady)
	require.Nil(t, decision)
}

// TestDispatch_EmptyModelNotRejected 空 model 应放行给下游原有的 unknown_model（404）处理，
// 不能退化为 403 direct_model_not_allowed。
func TestDispatch_EmptyModelNotRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"g1"})
	resolver := &countingTierResolver{}
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, resolver)
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1"})

	decision, err := dispatcher.Dispatch(ctx, "", nil)
	require.NoError(t, err)
	require.Nil(t, decision)
}

// TestDispatch_DirectModelRejectedGroupCacheNotReady 覆盖 2026-10-16 线上蓝绿发布事故：
// 组名缓存未就绪（未 SetNames / Refresh 未成功）时，非组名请求无法区分「真的直连模型」与
// 「尚未加载的合法组名」，必须返回可重试的 503 group_cache_not_ready，而不是 403。
// 拒绝同样发生在 tier 解析之前：不得调用 tierResolver.Resolve，也不得写 tier context。
func TestDispatch_DirectModelRejectedGroupCacheNotReady(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache("") // 故意不 SetNames：initialized=false
	require.False(t, nc.IsInitialized(), "前置条件：缓存必须处于未就绪状态")
	resolver := &countingTierResolver{}
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, resolver)
	ctx, _ := makeContext(&model.UserIdentity{
		KeyID:        "key-1",
		UsableGroups: []string{"g1"},
	})

	decision, err := dispatcher.Dispatch(ctx, "gpt-5.6-terra", nil)
	require.ErrorIs(t, err, model.ErrGroupCacheNotReady)
	require.Nil(t, decision)
	require.Zero(t, resolver.calls) // 未就绪判断必须先于 tier 判定
	_, ok := budget.GetTierContext(ctx)
	require.False(t, ok)
}

// TestDispatch_EmptyModelNotRejectedWhenCacheNotReady 未就绪也不能吞掉空 model 的原有语义：
// 空 model 仍走 (nil, nil) 交给下游 unknown_model（404），不得退化为 503/403。
func TestDispatch_EmptyModelNotRejectedWhenCacheNotReady(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache("") // 未就绪
	resolver := &countingTierResolver{}
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, resolver)
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1"})

	decision, err := dispatcher.Dispatch(ctx, "", nil)
	require.NoError(t, err)
	require.Nil(t, decision)
	require.Zero(t, resolver.calls)
}

// TestDispatch_SameModelNotReadyThenDirect 判定力对照：同一个未就绪缓存对同一个 model
// 先返回 503 group_cache_not_ready；SetNames 之后就绪后同一请求返回 403 direct_model_not_allowed。
// 唯一差异只有 IsInitialized()，正是线上窗口的判别条件。
func TestDispatch_SameModelNotReadyThenDirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache("")
	resolver := &countingTierResolver{}
	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, resolver)
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1"})

	decision, err := dispatcher.Dispatch(ctx, "gpt-5.6-terra", nil)
	require.ErrorIs(t, err, model.ErrGroupCacheNotReady)
	require.Nil(t, decision)

	nc.SetNames([]string{"g1"}) // 缓存就绪：gpt-5.6-terra 确实不是组名
	require.True(t, nc.IsInitialized())

	decision, err = dispatcher.Dispatch(ctx, "gpt-5.6-terra", nil)
	require.ErrorIs(t, err, model.ErrDirectModelNotAllowed)
	require.Nil(t, decision)
	require.Zero(t, resolver.calls)
	_, ok := budget.GetTierContext(ctx)
	require.False(t, ok)
}

// TestDispatch_GroupNameStillRoutes 组名请求在启用档位预算时仍正常路由并设置 tier context。
func TestDispatch_GroupNameStillRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache("")
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
