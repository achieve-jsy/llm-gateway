package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/stretchr/testify/require"
)

// 本文件锁定 /v1/videos 的**对外响应对象**（E4b 决策）：
//
//   - 形状对齐 OpenAI video 对象（权威形状取自 New API dto/openai_video.go），
//     字段集合冻结在 tests/contract/contracts.json 的 media_channels.video_response_fields；
//   - progress 是 **0-100 的整数**（不是字符串，且恒出现——OpenAI 结构体里没有 omitempty）；
//   - 补上 completed_at / seconds / size；
//   - artifacts 保留为**本网关扩展字段**（OpenAI 没有），不得因为「像 OpenAI」而删掉。
//
// 归一规则（mediaProgressValue）的取值理由写在 internal/gateway/handler/media_task_handler.go，
// 本文件只锁行为，避免规则悄悄漂移。

// seedTaskWithPrivate 造一条带 private_data（渠道细节）的任务并投放到 fake store。
func seedTaskWithPrivate(t *testing.T, rig *mediaTestRig, task *infra.MediaTask, private map[string]any) *infra.MediaTask {
	t.Helper()
	raw, err := json.Marshal(private)
	require.NoError(t, err)
	task.PrivateData = infra.JSONText(raw)
	return rig.seedMediaTask(task)
}

// TestMediaTaskResponse_ProgressNormalization 覆盖 progress 的整条归一规则。
func TestMediaTaskResponse_ProgressNormalization(t *testing.T) {
	cases := []struct {
		name         string
		storedStatus string
		storedValue  string
		wantProgress int
	}{
		{"百分比数字直接采用", "in_progress", "42", 42},
		{"上游偶发带百分号", "in_progress", "45%", 45},
		{"小数向下取整（宁可低估）", "in_progress", "99.4", 99},
		{"超上限 clamp 到 100", "in_progress", "150", 100},
		{"负数归零", "in_progress", "-5", 0},
		{"阶段名 queued", "queued", "queued", 0},
		{"阶段名 submitted（Kling 词表）", "in_progress", "submitted", 0},
		{"阶段名 queueing（已入上游队列）", "in_progress", "queueing", 10},
		{"阶段名 processing", "in_progress", "processing", 50},
		{"阶段名 generating", "in_progress", "generating", 50},
		{"空值取下界 0", "in_progress", "", 0},
		{"认不出的词取 0（绝不外推）", "in_progress", "weird_stage", 0},
		{"completed 强制 100（上游不报百分比）", "completed", "", 100},
		{"completed 也强制 100（即使存了 80）", "completed", "80", 100},
		{"failed 保留最后已知数字", "failed", "60", 60},
		{"failed 阶段名取下界", "failed", "failed", 0},
		{"expired 对外归一 failed 且取 0", "expired", "expired", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newMediaTestRig(t, nil, nil)
			rig.seedMediaTask(&infra.MediaTask{
				TaskID:   "task_progress",
				Status:   tc.storedStatus,
				Progress: tc.storedValue,
			})
			rec := rig.retrieve(t, "task_progress")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			resp := decodeJSON(t, rec)
			// ① 类型锁：必须是 JSON number（字符串形态的 "42" 会让 SDK 报类型错）。
			require.IsType(t, float64(0), resp["progress"],
				"progress 必须是整数（JSON number），实际 %T：%s", resp["progress"], rec.Body.String())
			require.EqualValues(t, tc.wantProgress, resp["progress"])
			// ② 恒出现：OpenAI 的 Progress 字段没有 omitempty，0 也必须出现。
			require.Contains(t, rec.Body.String(), `"progress":`)
		})
	}
}

// TestMediaTaskResponse_ExpiredIsFailed 锁定「存储层 expired → 对外 failed」这条归一。
func TestMediaTaskResponse_ExpiredIsFailed(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:       "task_expired",
		Status:       "expired",
		Progress:     "50",
		ErrorMessage: "任务超时",
	})
	rec := rig.retrieve(t, "task_expired")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	resp := decodeJSON(t, rec)
	require.Equal(t, "failed", resp["status"])
	require.EqualValues(t, 50, resp["progress"], "失败前的最后已知进度不该被抹掉")
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok, "失败任务必须带业务错误对象：%s", rec.Body.String())
	require.Equal(t, "upstream_error", errObj["code"])
	require.Contains(t, errObj["message"], "超时")
}

// TestMediaTaskResponse_OpenAIVideoShape 锁定完整字段集合与三个新增字段的来源。
//
// 注意数据来源刻意选 **request_body**（而不是 private_data）：控制面把 private_data
// 标了 json:"-"（internal/controlplane/model/video_task.go），内部 API 从不返回它，
// 因此生产环境能用的请求参数副本只有 media_tasks.request_body。用 private_data 造测试
// 会得到一个「测试绿、线上字段永远为空」的假绿。
func TestMediaTaskResponse_OpenAIVideoShape(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	finished := time.Now().UTC().Truncate(time.Second)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:      "task_shape",
		Status:      "completed",
		Progress:    "100",
		VideoURL:    "https://cdn.example.com/out.mp4",
		FinishedAt:  &finished,
		Usage:       infra.JSONText(`{"vendor":{"duration":5},"cost_input":{"output_seconds":5,"images":0}}`),
		RequestBody: `{"model":"kling-group","prompt":"p","duration":5,"size":"1920x1080"}`,
		ResponseBody: `{"status":"completed","artifacts":[` +
			`{"kind":"video","url":"https://cdn.example.com/out.mp4"}]}`,
	})

	rec := rig.retrieve(t, "task_shape")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodeJSON(t, rec)

	require.Equal(t, "task_shape", resp["id"])
	require.Equal(t, "video", resp["object"])
	require.Equal(t, "completed", resp["status"])
	require.Equal(t, testMediaGroup, resp["model"])
	require.NotZero(t, resp["created_at"])
	require.EqualValues(t, finished.Unix(), resp["completed_at"], "完成时间取自 media_tasks.finished_at")
	require.EqualValues(t, 100, resp["progress"])
	require.Equal(t, "5", resp["seconds"], "seconds 取自上游实际用量（duration=5），字符串形态")
	require.Equal(t, "1920x1080", resp["size"], "size 只在请求参数里字面给出 WxH 时回填")

	// artifacts 是本网关扩展字段，必须保留（前端直接播放/下载，不再换一次 302）。
	artifacts, ok := resp["artifacts"].([]any)
	require.True(t, ok, "artifacts 必须是数组：%s", rec.Body.String())
	require.Len(t, artifacts, 1)
	first, _ := artifacts[0].(map[string]any)
	require.Equal(t, "https://cdn.example.com/out.mp4", first["url"])

	// 刻意不实现的 OpenAI 字段：不发空值，也不假装支持。
	for _, absent := range []string{"task_id", "remixed_from_video_id", "metadata", "expires_at"} {
		require.NotContains(t, resp, absent, "未实现的字段不得出现：%s", absent)
	}
	require.NotContains(t, resp, "error", "成功任务不得带 error")
}

// TestMediaTaskResponse_SecondsAndSizeFallbacks 锁定两个可选字段的退化口径：
// 取不到就**省略**，绝不编造（尤其不做 "720p" → "1280x720" 的推导）。
func TestMediaTaskResponse_SecondsAndSizeFallbacks(t *testing.T) {
	cases := []struct {
		name string
		// privateData 是网关归一后的提交参数副本（控制面不返回它，仅作为兜底来源）。
		privateData map[string]any
		// requestBody 是网关持久化的原始请求体（**生产环境的实际来源**）。
		requestBody string
		usage       string
		wantSeconds string // "" = 期望字段省略
		wantSize    string // "" = 期望字段省略
	}{
		{
			name:        "request_body 是生产来源：从中取 duration",
			requestBody: `{"model":"kling-group","duration":8}`,
			wantSeconds: "8",
		},
		{
			name:        "request_body 里没有 duration：省略 seconds",
			requestBody: `{"model":"kling-group","prompt":"p"}`,
			wantSeconds: "",
		},
		{
			name:        "request_body 被截断（非法 JSON）：当作取不到，绝不猜",
			requestBody: `{"model":"kling-group","duration":8`,
		},
		{
			name:        "private_data 是兜底来源（控制面不返回它，兼容测试夹具/将来开放）",
			privateData: map[string]any{"submit_params": map[string]any{"duration": 8}},
			wantSeconds: "8",
		},
		{
			name:        "既无用量也无 duration：省略 seconds",
			privateData: map[string]any{"submit_params": map[string]any{}},
		},
		{
			name:        "size 由请求参数字面 WxH 回填（大小写归一）",
			privateData: map[string]any{"submit_params": map[string]any{"size": "1280X720"}},
			wantSize:    "1280x720",
		},
		{
			name:        "request_body 的 size 同样回填",
			requestBody: `{"model":"kling-group","size":"1280x720"}`,
			wantSize:    "1280x720",
		},
		{
			name:        "resolution=720p 不做尺寸推导：省略 size",
			privateData: map[string]any{"submit_params": map[string]any{"resolution": "720p", "aspect_ratio": "16:9"}},
		},
		{
			name:        "resolution 本身就是 WxH 时也接受",
			privateData: map[string]any{"submit_params": map[string]any{"resolution": "1080x1920"}},
			wantSize:    "1080x1920",
		},
		{
			name:        "上游用量的 duration 优先于请求参数",
			privateData: map[string]any{"submit_params": map[string]any{"duration": 5}},
			usage:       `{"vendor":{"duration":9}}`,
			wantSeconds: "9",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newMediaTestRig(t, nil, nil)
			task := &infra.MediaTask{
				TaskID:   "task_optional",
				Status:   "completed",
				Progress: "100",
			}
			if tc.privateData != nil {
				raw, err := json.Marshal(tc.privateData)
				require.NoError(t, err)
				task.PrivateData = infra.JSONText(raw)
			}
			task.RequestBody = tc.requestBody
			if tc.usage != "" {
				task.Usage = infra.JSONText(tc.usage)
			}
			rig.seedMediaTask(task)

			rec := rig.retrieve(t, "task_optional")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			resp := decodeJSON(t, rec)

			if tc.wantSeconds == "" {
				require.NotContains(t, resp, "seconds", "取不到时长时必须省略：%s", rec.Body.String())
			} else {
				require.Equal(t, tc.wantSeconds, resp["seconds"])
			}
			if tc.wantSize == "" {
				require.NotContains(t, resp, "size", "取不到尺寸时必须省略（不编造）：%s", rec.Body.String())
			} else {
				require.Equal(t, tc.wantSize, resp["size"])
			}
		})
	}
}

// TestMediaTaskCreate_ResponseProgressIsInteger 锁定**创建**路径的响应形状：
// 原实现把 progress 写成字符串 "0"（早期骨架），SDK 会按整数读。
func TestMediaTaskCreate_ResponseProgressIsInteger(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos",
		`{"model":"kling-group","prompt":"p","duration":5,"size":"1280x720"}`, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// 断言原始字节：`"progress":0`（整数）vs `"progress":"0"`（字符串）一眼可辨。
	require.Contains(t, rec.Body.String(), `"progress":0`)
	require.NotContains(t, rec.Body.String(), `"progress":"0"`)

	resp := decodeJSON(t, rec)
	require.IsType(t, float64(0), resp["progress"])
	require.EqualValues(t, 0, resp["progress"])
	require.Equal(t, "queued", resp["status"])
	require.NotContains(t, resp, "artifacts", "未产出产物时不得发空数组")
	require.NotContains(t, resp, "completed_at", "未完成时不得发 completed_at")
	// 创建响应同样带 seconds / size（来自本次请求参数）。
	require.Equal(t, "5", resp["seconds"])
	require.Equal(t, "1280x720", resp["size"])
}
