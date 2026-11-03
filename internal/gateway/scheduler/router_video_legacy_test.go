package scheduler

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestVideoCapabilityAbsentFieldDefaultsToUnsupported 钉住跨进程兼容性：
// 旧版控制面 payload 没有 supports_video_input 字段时，必须按"不支持"处理
// （video 请求得到明确的 400 依据），而不是 panic 或误判为支持。
func TestVideoCapabilityAbsentFieldDefaultsToUnsupported(t *testing.T) {
	oldPayload := `{"data":[{"id":7,"name":"legacy-group","strategy":"priority","enabled":true,
		"models":[{"id":"11111111-1111-1111-1111-111111111111","name":"legacy-model","model_type":"chat",
		"provider_type":"openai","provider_name":"openai"}]}]}`

	var cfg ControlPlaneConfig
	if err := json.Unmarshal([]byte(oldPayload), &cfg); err != nil {
		t.Fatalf("unmarshal legacy payload: %v", err)
	}
	if len(cfg.Data) != 1 || len(cfg.Data[0].Models) != 1 {
		t.Fatalf("unexpected payload shape: %+v", cfg.Data)
	}
	if cfg.Data[0].Models[0].SupportsVideoInput {
		t.Fatal("absent field must decode as false")
	}

	rt := NewRouteTable()
	rt.ReplaceAll(&ControlPlaneConfig{
		Groups: []GroupConfig{{ID: 7, Name: "legacy-group", Strategy: StrategyPriority, Enabled: true}},
		Models: []GroupModel{{
			ID: 1, GroupID: 7, ModelName: "legacy-model", ChannelType: "openai", ProviderName: "openai",
			SupportsVideoInput: cfg.Data[0].Models[0].SupportsVideoInput,
		}},
	})
	router := NewRouter(rt, NewHealthStore(3, 3, 3, 30*time.Second))

	_, err := router.Route("legacy-group", videoRequest())
	if !errors.Is(err, ErrNoVideoCapableModel) {
		t.Fatalf("err = %v, want ErrNoVideoCapableModel", err)
	}
}
