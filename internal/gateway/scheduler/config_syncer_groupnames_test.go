package scheduler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/cache"
)

// 同一份组清单必须由**一个**数据源供给：调度器 config sync 与控制面
// /api/v1/internal/groups 打的是同一个接口，但历史上网关为“组名判定”单独维护了
// 一份 HTTP 缓存（30s 刷新），于是重启时出现“路由已就绪、组名判定仍为空”的窗口，
// 全体请求被误判成直连模型 403（2026-10-30 17:17 事故）。
//
// 本用例锁死：sync 成功即把组名灌给组名缓存，且就绪顺序必须是
// “组名缓存先就绪 → lastSyncAt 后置”，从而让 /ready 为 true 时组名一定可用。
func TestConfigSyncerFeedsGroupNameCache(t *testing.T) {
	payload := `{
	  "data": [
	    {"id": 1, "name": "g-alpha", "strategy": "priority", "enabled": true, "models": []},
	    {"id": 2, "name": "g-beta", "strategy": "priority", "enabled": true,
	     "models": [{"id": "11111111-1111-1111-1111-111111111111", "name": "m1", "model_type": "chat",
	                 "provider_type": "openai", "provider_name": "openai"}]},
	    {"id": 3, "name": "g-disabled", "strategy": "priority", "enabled": false, "models": []}
	  ]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	nc := cache.NewGroupNameCache()
	if nc.IsInitialized() {
		t.Fatal("新建的组名缓存不应处于已初始化状态")
	}

	rt := NewRouteTable()
	hs := NewHealthStore(3, 3, 3, 30*time.Second)
	syncer := NewConfigSyncer(srv.URL, "test-token", rt, hs, time.Second)
	syncer.SetGroupNamesSink(nc.SetNames)

	if err := syncer.doSync(); err != nil {
		t.Fatalf("doSync: %v", err)
	}

	if !nc.IsInitialized() {
		t.Fatal("sync 成功后组名缓存必须就绪，否则冷启动窗口仍会被误判成直连模型")
	}
	for _, want := range []string{"g-alpha", "g-beta"} {
		if !nc.IsGroupName(want) {
			t.Fatalf("组名缓存缺少 %q", want)
		}
	}
	if nc.IsGroupName("g-disabled") {
		t.Fatal("禁用组不应进入组名缓存")
	}
	if syncer.LastSyncAt().IsZero() {
		t.Fatal("sync 成功后 lastSyncAt 必须更新（/ready 依赖它）")
	}
}
