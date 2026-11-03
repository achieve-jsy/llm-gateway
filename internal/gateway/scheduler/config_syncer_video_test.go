package scheduler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
)

// TestConfigSyncerMapsVideoCapability 走真实的 HTTP 映射链路（不再手工拼
// GroupModel）：控制面 payload 里的 supports_video_input 必须一路落到
// RouteTable 的 ModelRoutingEntry，并且能影响路由选择。
//
// 这条用例专门堵"只改了 payload 结构或只改了 struct 字段名，映射漏写"的漏洞——
// 那种错误在编译期和手工拼结构的用例里都看不出，只在线上表现为视频永远 400。
func TestConfigSyncerMapsVideoCapability(t *testing.T) {
	payload := `{
	  "data": [
	    {
	      "id": 1,
	      "name": "mixed-group",
	      "strategy": "priority",
	      "enabled": true,
	      "models": [
	        {"id": "11111111-1111-1111-1111-111111111111", "name": "video-model", "model_type": "chat",
	         "provider_type": "ark", "provider_name": "ark", "priority": 0, "supports_video_input": true},
	        {"id": "22222222-2222-2222-2222-222222222222", "name": "text-model", "model_type": "chat",
	         "provider_type": "openai", "provider_name": "openai", "priority": 1, "supports_video_input": false}
	      ]
	    }
	  ]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Auth-Token"); got != "test-token" {
			t.Errorf("X-Auth-Token = %q, want test-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	rt := NewRouteTable()
	hs := NewHealthStore(3, 3, 30*time.Second, time.Minute)
	syncer := NewConfigSyncer(srv.URL, "test-token", rt, hs, time.Second)

	cfg, err := syncer.fetchConfig()
	if err != nil {
		t.Fatalf("fetchConfig: %v", err)
	}
	if len(cfg.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(cfg.Models))
	}
	if !cfg.Models[0].SupportsVideoInput {
		t.Fatalf("fetchConfig 丢掉了 supports_video_input（models[0]=%+v）", cfg.Models[0])
	}
	if cfg.Models[1].SupportsVideoInput {
		t.Fatalf("text-model 不应被标记支持视频")
	}

	rt.ReplaceAll(cfg)

	decision, err := NewRouter(rt, hs).Route("mixed-group", videoRequest())
	if err != nil {
		t.Fatalf("video route: %v", err)
	}
	if decision.ModelName != "video-model" {
		t.Fatalf("video route = %q, want video-model", decision.ModelName)
	}

	textDecision, err := NewRouter(rt, hs).Route("mixed-group", &core.ChatRequest{
		Model:    "mixed-group",
		Messages: []core.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("text route: %v", err)
	}
	if textDecision.ModelName != "video-model" {
		t.Fatalf("text route = %q, want video-model（priority 更高）", textDecision.ModelName)
	}
}
