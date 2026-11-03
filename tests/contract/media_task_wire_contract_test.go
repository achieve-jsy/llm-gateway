package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

// 本文件锁死 gateway → control-plane 这条**内部接缝**的 jsonb 字段线上表示。
//
// 为什么需要一条专门的测试（真实缺陷复盘，2026-10 Batch 1 合并时发现）：
// gateway 侧把 private_data / usage 的 Go 类型写成了 string，于是 json.Marshal 产出
//
//	"private_data": "{\"upstream_model\":\"kling-v1-6\"}"   ← 带引号的双层编码
//
// 而控制面侧按 map[string]interface{} 绑定，**解不开** → 每一次创建请求都 400。
// 两支队伍各自的门禁全绿：gateway 队打的是 fake HTTP server（它不按控制面的结构体绑定），
// 控制面队测的是自己的 handler（body 由自己构造）。**谁都看不见这个问题**——
// 只有把两侧放进同一个进程、对同一份字节做「编码 → 绑定」才会暴露。
//
// 因此本测试有两个不可省的断言，任何一条单独存在都发现不了该缺陷：
//  ① 编码形状：private_data / usage 必须是 JSON 对象，不是 JSON 字符串；
//  ② 可解码性：同一份字节必须能被控制面**真实的请求结构体 + 真实的 binding** 解开。
//
// 契约归属：docs/media-channels-integration-design.md 内部 API 契约；
// 类型定义见 internal/gateway/infra/video_task_client.go（json.RawMessage）。
// 谁要把这两个字段改回 string 或 datatypes.JSON 之外的有损类型，本测试必须失败。

// encode 用客户端**真实的编码方式**产出请求体（见 video_task_client.go 里各处的
// json.Marshal(req)），再给出该 JSON 的 map 视图以便断言字段形状。
func encode(t *testing.T, req any) ([]byte, map[string]any) {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(body, &view))
	return body, view
}

// requireJSONObject 断言某个 jsonb 字段是**裸 JSON 对象**。
//
// 失败信息会把实际类型和内容打出来：如果哪天退化成字符串，报错会直接显示
// `实际是 string: {"upstream_model":"kling-v1-6"}`，一眼看出是双层编码。
func requireJSONObject(t *testing.T, view map[string]any, field string) map[string]any {
	t.Helper()
	raw, exists := view[field]
	require.True(t, exists, "%s 字段应当存在（非空时不得被省略）", field)
	obj, ok := raw.(map[string]any)
	require.True(t, ok,
		"%s 必须是裸 JSON 对象（控制面按 map 绑定），实际是 %T：%v", field, raw, raw)
	return obj
}

// TestMediaTaskWire_CreateRequestPrivateDataIsObject 覆盖创建请求的 private_data：
// 它承载渠道细节（上游模型名、提交参数），每次创建都会带上，因此这条一旦错，
// /v1/videos 创建链路是**全量**失败而不是偶发失败。
func TestMediaTaskWire_CreateRequestPrivateDataIsObject(t *testing.T) {
	req := infra.MediaCreateRequest{
		TaskID:       "task_wire_1",
		APIKeyID:     "key_1",
		APIKeyName:   "default",
		APIKeyType:   "user",
		UserID:       "u_1",
		UserName:     "tester",
		Model:        "video-group",
		ProviderName: "kling-main",
		ProviderType: "kling",
		Status:       "queued",
		Kind:         "video",
		Action:       "text_to_video",
		Progress:     "0",
		PrivateData: json.RawMessage(
			`{"kind":"video","action":"text_to_video","upstream_model":"kling-v1-6","submit_params":{"duration":8}}`),
	}

	body, view := encode(t, req)

	// ① 编码形状
	private := requireJSONObject(t, view, "private_data")
	require.Equal(t, "kling-v1-6", private["upstream_model"])

	// ② 对端能解开（当初正是这一步 400）
	var got handler.CreateVideoTaskRequest
	require.NoError(t, binding.JSON.BindBody(body, &got),
		"控制面必须能绑定网关发出的创建请求体（原始字节：%s）", string(body))
	require.Equal(t, "video", got.Kind)
	require.Equal(t, "text_to_video", got.Action)
	require.NotNil(t, got.PrivateData, "private_data 必须解成非 nil 的对象")
	require.Equal(t, "kling-v1-6", got.PrivateData["upstream_model"])
	require.Equal(t, 8.0, got.PrivateData["submit_params"].(map[string]any)["duration"])
}

// TestMediaTaskWire_UpdateRequestUsageIsObject 覆盖更新请求的 usage：
// 终态完成时写用量，若这里退化成字符串，记账会在 PATCH 阶段 400 失败
// （此前置条件更隐蔽：创建成功、上游也跑完了，只在最后记账时炸）。
func TestMediaTaskWire_UpdateRequestUsageIsObject(t *testing.T) {
	cost := int64(125000)
	req := infra.MediaUpdateRequest{
		Status:       "completed",
		Progress:     "100",
		VendorTaskID: "vendor_abc",
		Usage: json.RawMessage(
			`{"vendor":{"credits":3.5},"cost_input":{"output_seconds":5},"cost_usd":0.125,"cost_missing":false}`),
		CostMicroUSD:   &cost,
		BilledAt:       "2026-10-16T12:00:00Z",
		ExpectedStatus: "in_progress",
	}

	body, view := encode(t, req)

	usage := requireJSONObject(t, view, "usage")
	require.NotNil(t, usage["cost_input"])

	var got handler.UpdateVideoTaskRequest
	require.NoError(t, binding.JSON.BindBody(body, &got),
		"控制面必须能绑定网关发出的更新请求体（原始字节：%s）", string(body))
	require.NotNil(t, got.Usage, "usage 必须解成非 nil 的对象")
	require.Equal(t, 5.0, (*got.Usage)["cost_input"].(map[string]any)["output_seconds"])
	require.NotNil(t, got.CostMicroUSD)
	require.Equal(t, int64(125000), *got.CostMicroUSD)
	require.NotNil(t, got.BilledAt, "billed_at 必须解析成时间")
}

// TestMediaTaskWire_EmptyJSONBFieldsAreOmitted 固化「不写」的语义：
// private_data / usage 为空时必须**整字段省略**，控制面据此把值留作 nil
// （usage 的 nil = 不修改该列；若发成 null/"" 会覆盖掉既有用量）。
func TestMediaTaskWire_EmptyJSONBFieldsAreOmitted(t *testing.T) {
	createBody, createView := encode(t, infra.MediaCreateRequest{
		TaskID: "task_wire_2", APIKeyID: "key_1", Model: "m",
		ProviderName: "p", ProviderType: "kling",
	})
	require.NotContains(t, createView, "private_data", "空 private_data 必须被省略")

	updateBody, updateView := encode(t, infra.MediaUpdateRequest{Status: "in_progress"})
	require.NotContains(t, updateView, "usage", "空 usage 必须被省略")

	// 省略后控制面侧仍应绑定成功，且指针为 nil（= 不修改）。
	var gotUpdate handler.UpdateVideoTaskRequest
	require.NoError(t, binding.JSON.BindBody(updateBody, &gotUpdate))
	require.Nil(t, gotUpdate.Usage)

	var gotCreate handler.CreateVideoTaskRequest
	require.NoError(t, binding.JSON.BindBody(createBody, &gotCreate))
	require.Nil(t, gotCreate.PrivateData)
}

// TestMediaTaskWire_StringEncodedJSONBIsRejected 是**反向控制**：把当初的错误编码
// 直接写进 body，证明控制面会拒绝它。
//
// 存在的意义：这条负向断言把「jsonb 字段必须是对象」从注释里的约定变成可执行约束。
// 如果有人为了「兼容」在控制面加一个宽容的字符串解包，本测试会失败并提醒：
// 该做法会把 JSON 字符串原样存进 jsonb 列，而读侧按 map 解析同样会失败，
// 于是把一个响亮的 400 变成一次静默的数据损坏。
func TestMediaTaskWire_StringEncodedJSONBIsRejected(t *testing.T) {
	// 注意 private_data 的值是**字符串**（旧编码），不是对象。
	body := []byte(`{"model":"m","provider_name":"p","provider_type":"kling",` +
		`"private_data":"{\"upstream_model\":\"kling-v1-6\"}"}`)

	var got handler.CreateVideoTaskRequest
	err := binding.JSON.BindBody(body, &got)
	require.Error(t, err,
		"控制面不得接受 JSON 字符串形态的 private_data（会静默写入损坏的 jsonb）")
}
