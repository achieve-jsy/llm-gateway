package orchestrator_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 端到端复现线上事故的**启动时序**（不是单个函数）：控制面比网关晚起来。
//
// 2026-10-30 17:17 blue 色：网关与控制面同一秒启动，控制面 ~33s 才可用；网关
// 自己的组名 HTTP 缓存 5×2s 就放弃了，而调度器路由表 17:17:52 已就绪、/ready 变 200，
// 于是 nginx 在 17:17:58 切流，17:17:58→17:18:15 有 65 个 403 direct_model_not_allowed。
//
// 本用例按 main.go 的真实接线顺序（建缓存 → 挂 sink → Start）跑一遍：
//   ① CP 未就绪 → 请求必须拿到可重试的 503 config_not_ready，绝不能是 403；
//   ② CP 上线后再取请求 → 正常路由；
//   ③ 并且 /ready（scheduler.IsReady）为 true 的那一刻，组名缓存必须已经就绪——
//      这是部署脚本敢切流的前提。
func TestColdStartWiring_ControlPlaneLate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payload := `{"data":[{"id":1,"name":"g-alpha","strategy":"priority","enabled":true,
	  "models":[{"id":"11111111-1111-1111-1111-111111111111","name":"m1","model_type":"chat",
	              "provider_type":"openai","provider_name":"openai"}]}]}`
	// 先只创建不启动，模拟“控制面还没起来”
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/internal/groups" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))

	// NewUnstartedServer 的 URL 要等到 Start 才填充，这里先按监听地址拼
	cpURL := "http://" + srv.Listener.Addr().String()
	cfg := scheduler.DefaultConfig(cpURL)
	cfg.InternalToken = "test-token"
	cfg.ConfigSyncInterval = 50 * time.Millisecond
	sch, err := scheduler.New(cfg)
	require.NoError(t, err)

	// 与 cmd/gateway/main.go 完全一致的接线顺序：建缓存 → 挂 sink → Start
	nc := cache.NewGroupNameCache()
	sch.SetGroupNamesSink(nc.SetNames)
	sch.Start(context.Background())
	defer sch.Shutdown()

	dispatcher := orchestrator.NewGroupAwareDispatcherWithBindingAndTier(sch, nc, nil, &countingTierResolver{})

	// ① 控制面未就绪：503（可重试），不是 403（永久性鉴权失败）
	ctx, _ := makeContext(&model.UserIdentity{KeyID: "key-1", AccessAllGroups: true})
	decision, err := dispatcher.Dispatch(ctx, "g-alpha", nil)
	require.Nil(t, decision)
	require.ErrorIs(t, err, model.ErrConfigNotReady)
	require.NotErrorIs(t, err, model.ErrDirectModelNotAllowed)
	require.False(t, nc.IsInitialized())

	// ② 控制面上线
	srv.Start()
	defer srv.Close()

	// ③ /ready 一旦为 true，组名必须已经可用（否则切流又会打进 403 窗口）
	deadline := time.Now().Add(5 * time.Second)
	for !sch.IsReady() {
		require.True(t, time.Now().Before(deadline), "控制面上线后调度器迟迟未就绪")
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, nc.IsInitialized(), "scheduler ready 时组名缓存必须已就绪（/ready 是切流门禁）")
	require.True(t, nc.IsGroupName("g-alpha"))

	// ④ 同一请求恢复为正常路由
	ctx2, _ := makeContext(&model.UserIdentity{KeyID: "key-1", AccessAllGroups: true})
	decision, err = dispatcher.Dispatch(ctx2, "g-alpha", nil)
	require.NoError(t, err)
	require.NotNil(t, decision)
}
