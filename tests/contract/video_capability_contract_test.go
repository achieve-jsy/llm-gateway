package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/stretchr/testify/require"
)

// TestVideoCapabilityContractDecode 钉住两条跨进程契约里 supports_video_input 的
// 字段名与解码方向：控制面写什么键，网关就必须能从同一个键读出来（拼错或漏掉
// json tag 会让"已声明的模型可带视频对话"静默失效，只在运行时表现为 400）。
func TestVideoCapabilityContractDecode(t *testing.T) {
	// 契约 B：/internal/v1/groups/model-info → binding.GroupModelInfo
	var info binding.GroupModelInfo
	require.NoError(t, json.Unmarshal([]byte(`{"mode":"chat","max_input_tokens":1000,
		"max_output_tokens":100,"supports_vision":true,"supports_video_input":true}`), &info))
	require.True(t, info.SupportsVideoInput, "model-info payload 的 supports_video_input 未被解码")

	// 契约 A：/api/v1/internal/groups → scheduler.ControlPlaneConfig
	var cfg scheduler.ControlPlaneConfig
	require.NoError(t, json.Unmarshal([]byte(`{"data":[{"id":1,"name":"g","strategy":"priority","enabled":true,
		"models":[{"id":"m-1","model_id":1,"name":"video-model","model_type":"chat","provider_type":"ark",
		"provider_name":"ark","supports_video_input":true}]}]}`), &cfg))
	require.Len(t, cfg.Data, 1)
	require.Len(t, cfg.Data[0].Models, 1)
	require.True(t, cfg.Data[0].Models[0].SupportsVideoInput, "groups payload 的 supports_video_input 未被解码")

	// 最后一跳：payload → RouteTable 的 ModelRoutingEntry 也必须带上能力位
	rt := scheduler.NewRouteTable()
	rt.ReplaceAll(&scheduler.ControlPlaneConfig{
		Groups: []scheduler.GroupConfig{{ID: 1, Name: "g", Strategy: scheduler.StrategyPriority, Enabled: true}},
		Models: []scheduler.GroupModel{{
			ID: 1, GroupID: 1, ModelID: 1, ModelName: "video-model",
			SupportsVideoInput: cfg.Data[0].Models[0].SupportsVideoInput,
		}},
	})
	group := rt.Get("g")
	require.NotNil(t, group)
	require.Len(t, group.Models, 1)
	require.True(t, group.Models[0].SupportsVideoInput, "RouteTable 未透传能力位")
}
