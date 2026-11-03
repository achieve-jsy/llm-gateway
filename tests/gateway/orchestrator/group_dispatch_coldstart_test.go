package orchestrator_test

import (
	"net/http"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 生产事故（2026-10-30 17:17 blue 色切流）根因回归：
// 网关重启时组名注册表还没加载完（空集合），旧逻辑把每个合法组名都判成“直连模型”，
// 于是启用档位预算的生产环境对全体调用方返回 403 direct_model_not_allowed。
//
// 语义约束：“还不知道”不能等同于“不是组”。
// 注册表未就绪时必须是可重试的 503 config_not_ready，绝不能是 403。
func TestDispatch_ColdStartRegistryReturns503NotDirectModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache() // 冷启动：从未 SetNames
	require.False(t, nc.IsInitialized())

	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, &countingTierResolver{})
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1", AccessAllGroups: true})

	decision, err := dispatcher.Dispatch(ctx, "deepseek-v4-flash-official", nil)
	require.Nil(t, decision)
	require.ErrorIs(t, err, model.ErrConfigNotReady)
	require.NotErrorIs(t, err, model.ErrDirectModelNotAllowed)
	require.Equal(t, http.StatusServiceUnavailable, model.ErrConfigNotReady.HTTPStatus)
	require.Equal(t, "config_not_ready", model.ErrConfigNotReady.Code)
}

// 未启用档位预算的部署同样不能把冷启动误判成直连：否则退化成 404 unknown_model，
// 调用方看到的是“模型不存在”，同样误导。
func TestDispatch_ColdStartRegistryNoTierAlsoRetryable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache()

	dispatcher := orchestrator.NewGroupAwareDispatcher(sch, nc)
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1"})

	decision, err := dispatcher.Dispatch(ctx, "deepseek-v4-flash-official", nil)
	require.Nil(t, decision)
	require.ErrorIs(t, err, model.ErrConfigNotReady)
}

// 空 model 的既有语义不变：放行给下游的 unknown_model(404)。
func TestDispatch_ColdStartEmptyModelStillPassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache()

	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, &countingTierResolver{})
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1"})

	decision, err := dispatcher.Dispatch(ctx, "", nil)
	require.NoError(t, err)
	require.Nil(t, decision)
}

// 注册表就绪后，真正的直连模型名仍必须 403 —— 修复不能把档位预算闸门一起放开。
func TestDispatch_ReadyRegistryStillRejectsDirectModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sch := &fakeScheduler{decision: &scheduler.RouteDecision{ModelID: "1", ModelName: "m"}}
	nc := cache.NewGroupNameCache()
	nc.SetNames([]string{"g1"})

	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, &countingTierResolver{})
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1", AccessAllGroups: true})

	decision, err := dispatcher.Dispatch(ctx, "4476-some-channel", nil)
	require.Nil(t, decision)
	require.ErrorIs(t, err, model.ErrDirectModelNotAllowed)
}
