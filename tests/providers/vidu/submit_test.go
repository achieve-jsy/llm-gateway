package vidu_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/vidu"
)

// 测试目的（核心规则之一）：**action 与提交端点由 images 数组长度决定**，逐条遍历判定表。
//
//	0 张 → /ent/v2/text2video      （text_to_video）
//	1 张 → /ent/v2/img2video       （image_to_video）
//	2 张 → /ent/v2/start-end2video （first_tail_to_video）
//	>2 张 → /ent/v2/reference2video（reference_to_video）
func TestSubmit_ActionByImageCount(t *testing.T) {
	cases := []struct {
		name       string
		images     []string
		action     core.MediaAction
		wantPath   string
		wantPrompt bool
	}{
		{
			name:     "0 张 → 文生",
			images:   nil,
			action:   core.MediaActionTextToVideo,
			wantPath: "/ent/v2/text2video",
		},
		{
			name:     "1 张 → 图生",
			images:   []string{"https://cdn.example.com/a.png"},
			action:   core.MediaActionImageToVideo,
			wantPath: "/ent/v2/img2video",
		},
		{
			name:     "2 张 → 首尾帧",
			images:   []string{"https://cdn.example.com/first.png", "https://cdn.example.com/last.png"},
			action:   core.MediaActionFirstTailToVideo,
			wantPath: "/ent/v2/start-end2video",
		},
		{
			name:     "3 张 → 参考生视频",
			images:   []string{"https://cdn.example.com/1.png", "https://cdn.example.com/2.png", "https://cdn.example.com/3.png"},
			action:   core.MediaActionReferenceToVideo,
			wantPath: "/ent/v2/reference2video",
		},
		{
			name:     "7 张 → 仍是参考生视频（>2 不设本地上限）",
			images:   []string{"u1", "u2", "u3", "u4", "u5", "u6", "u7"},
			action:   core.MediaActionReferenceToVideo,
			wantPath: "/ent/v2/reference2video",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			// viduq2 家族对 duration/resolution 宽松，避免干扰本用例关注点。
			result, err := adapter.Submit(t.Context(), submitRequest(tc.action, tc.images, nil))
			require.NoError(t, err)
			require.NotNil(t, result)

			request := fake.last(t)
			assert.Equal(t, "POST", request.Method)
			assert.Equal(t, tc.wantPath, request.Path)

			body := decodeBody(t, request.Body)
			assert.Equal(t, "viduq2", body["model"])
			assert.Equal(t, "auto", body["movement_amplitude"])
			assert.Equal(t, "一只猫在弹钢琴", body["prompt"])

			images, hasImages := body["images"]
			if len(tc.images) == 0 {
				assert.False(t, hasImages, "纯文生不得发送 images 字段（避免上游按空图处理）")
				return
			}
			require.True(t, hasImages)
			decoded, ok := images.([]any)
			require.True(t, ok)
			require.Len(t, decoded, len(tc.images))
			for i, want := range tc.images {
				assert.Equal(t, want, decoded[i], "images[%d] 必须保序透传", i)
			}
		})
	}
}

// 测试目的：`req.Action` 与图片张数**矛盾**时必须 400（且不发上游请求），不得自作主张。
//
// 静默按数量纠正会让用户以为自己的 action 生效了（例如以为走了首尾帧，实际被当成单图）。
func TestSubmit_ActionMismatchIsRejected(t *testing.T) {
	cases := []struct {
		name   string
		action core.MediaAction
		images []string
	}{
		{name: "文生却带 1 张图", action: core.MediaActionTextToVideo, images: []string{"u1"}},
		{name: "文生却带 2 张图", action: core.MediaActionTextToVideo, images: []string{"u1", "u2"}},
		{name: "图生却带 0 张图", action: core.MediaActionImageToVideo, images: nil},
		{name: "图生却带 2 张图", action: core.MediaActionImageToVideo, images: []string{"u1", "u2"}},
		{name: "首尾帧却带 1 张图", action: core.MediaActionFirstTailToVideo, images: []string{"u1"}},
		{name: "首尾帧却带 3 张图", action: core.MediaActionFirstTailToVideo, images: []string{"u1", "u2", "u3"}},
		{name: "参考图却带 1 张图", action: core.MediaActionReferenceToVideo, images: []string{"u1"}},
		{name: "action 为空（不在词汇表内）", action: "", images: []string{"u1"}},
		{name: "action 为未知词", action: core.MediaAction("video_edit"), images: []string{"u1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			result, err := adapter.Submit(t.Context(), submitRequest(tc.action, tc.images, nil))
			require.Error(t, err)
			assert.Nil(t, result)
			assertBadRequest(t, err)
			assert.Zero(t, fake.count(), "参数校验失败时不得请求上游")
		})
	}
}

// 测试目的：Params 表外键 **fail-closed** —— 400 而不是静默忽略。
//
// 静默忽略会让用户以为参数生效了（例如 movement_amplitude / seed / negative_prompt），
// 而费用照付。这里逐条覆盖设计文档点名过的厂商私有参数。
func TestSubmit_UnknownParamKeyIsRejected(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  any
	}{
		{name: "movement_amplitude（适配器内部固定 auto）", key: "movement_amplitude", val: "large"},
		{name: "seed", key: "seed", val: 42},
		{name: "negative_prompt", key: "negative_prompt", val: "暴力"},
		{name: "metadata（厂商私有逃生舱）", key: "metadata", val: map[string]any{"a": 1}},
		{name: "callback_url", key: "callback_url", val: "https://example.com/cb"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			req := textToVideoRequest(func(params map[string]any) {
				params[tc.key] = tc.val
			})
			result, err := adapter.Submit(t.Context(), req)

			require.Error(t, err)
			assert.Nil(t, result)
			assertBadRequest(t, err)
			assert.Contains(t, err.Error(), tc.key, "报错文案必须点名表外键")
			assert.Zero(t, fake.count(), "表外键必须在发上游之前被拦住")
		})
	}
}

// 测试目的：多个表外键时报错文案稳定（排序）且全部列出。
func TestSubmit_UnknownParamKeysSorted(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	req := textToVideoRequest(func(params map[string]any) {
		params["seed"] = 1
		params["negative_prompt"] = "x"
	})
	_, err := adapter.Submit(t.Context(), req)
	require.Error(t, err)
	assertBadRequest(t, err)
	assert.Contains(t, err.Error(), "negative_prompt, seed")
	assert.Zero(t, fake.count())
}

// 测试目的：duration 按模型家族收紧（vidu2.0∈{4,8}、viduq1 仅 5、viduq2∈[1,10]），
// 越界**提前 400**；未提供时落到该家族的缺省值。
func TestSubmit_DurationConstraints(t *testing.T) {
	cases := []struct {
		name         string
		model        string
		duration     any // nil = 不提供
		wantErr      bool
		wantDuration float64
	}{
		{name: "vidu2.0 duration=4 合法", model: "vidu2.0", duration: 4, wantDuration: 4},
		{name: "vidu2.0 duration=8 合法（分辨率取 720p）", model: "vidu2.0", duration: 8, wantDuration: 8},
		{name: "vidu2.0 duration=5 非法（不在 {4,8}）", model: "vidu2.0", duration: 5, wantErr: true},
		{name: "vidu2.0 duration=6 非法", model: "vidu2.0", duration: 6, wantErr: true},
		{name: "vidu2.0 缺省 → 4（不是通用缺省 5）", model: "vidu2.0", duration: nil, wantDuration: 4},

		{name: "viduq1 duration=5 合法", model: "viduq1", duration: 5, wantDuration: 5},
		{name: "viduq1 duration=4 非法", model: "viduq1", duration: 4, wantErr: true},
		{name: "viduq1 duration=8 非法", model: "viduq1", duration: 8, wantErr: true},
		{name: "viduq1 缺省 → 5", model: "viduq1", duration: nil, wantDuration: 5},

		{name: "viduq2 duration=1 合法（下界）", model: "viduq2", duration: 1, wantDuration: 1},
		{name: "viduq2 duration=10 合法（上界）", model: "viduq2", duration: 10, wantDuration: 10},
		{name: "viduq2 duration=11 非法", model: "viduq2", duration: 11, wantErr: true},
		{name: "viduq2 duration=100 非法", model: "viduq2", duration: 100, wantErr: true},
		{name: "viduq2 duration=0 视为未提供 → 5", model: "viduq2", duration: 0, wantDuration: 5},
		{name: "viduq2 duration=负数 视为未提供 → 5", model: "viduq2", duration: -3, wantDuration: 5},
		{name: "viduq2 数字字符串 \"7\" 合法", model: "viduq2", duration: "7", wantDuration: 7},
		{name: "viduq2 非数字字符串非法", model: "viduq2", duration: "abc", wantErr: true},
		{name: "viduq2 浮点非法（类型不符）", model: "viduq2", duration: []any{1}, wantErr: true},

		{name: "vidu1.5（未在矩阵内）任意正整数不设本地约束", model: "vidu1.5", duration: 7, wantDuration: 7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			// 统一用 2 张图的首尾帧请求：这样 vidu2.0 也能走到 duration 校验
			// （纯文生在 vidu2.0 上是另一条独立约束，见 TestSubmit_Vidu20TextToVideoRejected）。
			req := submitRequest(core.MediaActionFirstTailToVideo, []string{"u1", "u2"},
				func(params map[string]any) {
					if tc.duration != nil {
						params["duration"] = tc.duration
					}
				})
			req.UpstreamModel = tc.model

			result, err := adapter.Submit(t.Context(), req)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				assertBadRequest(t, err)
				assert.Zero(t, fake.count(), "参数非法时不得请求上游")
				return
			}
			require.NoError(t, err)
			body := decodeBody(t, fake.last(t).Body)
			assert.Equal(t, tc.wantDuration, body["duration"])
		})
	}
}

// 测试目的：resolution 全局白名单 {360p,540p,720p,1080p}＋viduq1 强制 1080p＋缺省档位。
func TestSubmit_ResolutionConstraints(t *testing.T) {
	cases := []struct {
		name           string
		model          string
		resolution     string
		duration       any
		wantErr        bool
		wantResolution string
	}{
		{name: "viduq2 360p 合法", model: "viduq2", resolution: "360p", wantResolution: "360p"},
		{name: "viduq2 540p 合法", model: "viduq2", resolution: "540p", wantResolution: "540p"},
		{name: "viduq2 720p 合法", model: "viduq2", resolution: "720p", wantResolution: "720p"},
		{name: "viduq2 1080p 合法", model: "viduq2", resolution: "1080p", wantResolution: "1080p"},
		{name: "viduq2 大小写归一（1080P → 1080p）", model: "viduq2", resolution: "1080P", wantResolution: "1080p"},
		{name: "viduq2 缺省 → 1080p", model: "viduq2", resolution: "", wantResolution: "1080p"},
		{name: "viduq2 4k 非法", model: "viduq2", resolution: "4k", wantErr: true},
		{name: "viduq2 720 非法（缺 p）", model: "viduq2", resolution: "720", wantErr: true},

		{name: "viduq1 强制 1080p：显式 1080p 合法", model: "viduq1", resolution: "1080p", wantResolution: "1080p"},
		{name: "viduq1 强制 1080p：缺省 → 1080p", model: "viduq1", resolution: "", wantResolution: "1080p"},
		{name: "viduq1 强制 1080p：显式 720p 报错（不静默改写）", model: "viduq1", resolution: "720p", wantErr: true},
		{name: "viduq1 强制 1080p：显式 360p 报错", model: "viduq1", resolution: "360p", wantErr: true},

		{name: "vidu2.0 duration=4 时 1080p 合法", model: "vidu2.0", resolution: "1080p", duration: 4, wantResolution: "1080p"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			// 同样用 2 张图的首尾帧请求，避免与 vidu2.0 的纯文生约束互相干扰。
			req := submitRequest(core.MediaActionFirstTailToVideo, []string{"u1", "u2"},
				func(params map[string]any) {
					params["resolution"] = tc.resolution
					if tc.duration != nil {
						params["duration"] = tc.duration
					}
				})
			req.UpstreamModel = tc.model

			result, err := adapter.Submit(t.Context(), req)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				assertBadRequest(t, err)
				assert.Zero(t, fake.count(), "参数非法时不得请求上游")
				return
			}
			require.NoError(t, err)
			body := decodeBody(t, fake.last(t).Body)
			assert.Equal(t, tc.wantResolution, body["resolution"])
		})
	}
}

// 测试目的（最易漏的一条）：**组合约束 `vidu2.0` 且 duration=8 时只允许 720p**。
//
// 三种情形都要覆盖：显式 720p 放行、显式其它档 400、未指定时兜底 720p。
func TestSubmit_Vidu20Duration8OnlyAllows720p(t *testing.T) {
	cases := []struct {
		name           string
		duration       any
		resolution     string
		wantErr        bool
		wantResolution string
	}{
		{name: "8s + 720p 放行", duration: 8, resolution: "720p", wantResolution: "720p"},
		{name: "8s + 1080p 拒绝", duration: 8, resolution: "1080p", wantErr: true},
		{name: "8s + 540p 拒绝", duration: 8, resolution: "540p", wantErr: true},
		{name: "8s + 360p 拒绝", duration: 8, resolution: "360p", wantErr: true},
		{name: "8s + 未指定 → 兜底 720p", duration: 8, resolution: "", wantResolution: "720p"},
		{name: "4s + 1080p 放行（组合约束只在 8s 生效）", duration: 4, resolution: "1080p", wantResolution: "1080p"},
		{name: "4s + 未指定 → 1080p", duration: 4, resolution: "", wantResolution: "1080p"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			// 2 张图（首尾帧）避开「vidu2.0 不支持纯文生」这条独立约束。
			req := submitRequest(core.MediaActionFirstTailToVideo,
				[]string{"https://cdn.example.com/first.png", "https://cdn.example.com/last.png"},
				func(params map[string]any) {
					params["duration"] = tc.duration
					params["resolution"] = tc.resolution
				})
			req.UpstreamModel = "vidu2.0"

			result, err := adapter.Submit(t.Context(), req)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				assertBadRequest(t, err)
				assert.Zero(t, fake.count(), "组合约束必须在发上游之前拦住")
				return
			}
			require.NoError(t, err)
			body := decodeBody(t, fake.last(t).Body)
			assert.Equal(t, tc.wantResolution, body["resolution"])
			assert.Equal(t, "vidu2.0", body["model"])
		})
	}
}

// 测试目的：`vidu2.0` 不支持纯文生 → 无图提前 400（任务书硬约束）。
func TestSubmit_Vidu20TextToVideoRejected(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	req := textToVideoRequest(nil)
	req.UpstreamModel = "vidu2.0"

	_, err := adapter.Submit(t.Context(), req)
	require.Error(t, err)
	assertBadRequest(t, err)
	assert.Contains(t, err.Error(), "vidu2.0")
	assert.Zero(t, fake.count(), "无图纯文生在 vidu2.0 上必须提前 400，不得请求上游")

	// 对照：带 1 张图（图生）时 vidu2.0 可用。
	okReq := submitRequest(core.MediaActionImageToVideo, []string{"https://cdn.example.com/a.png"},
		func(params map[string]any) { params["duration"] = 4 })
	okReq.UpstreamModel = "vidu2.0"
	_, err = adapter.Submit(t.Context(), okReq)
	require.NoError(t, err)
	assert.Equal(t, "/ent/v2/img2video", fake.last(t).Path)
}

// 测试目的：纯文生必须有 prompt（上游硬要求，提前 400 优于让上游报错）。
func TestSubmit_TextToVideoRequiresPrompt(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	req := textToVideoRequest(func(params map[string]any) { params["prompt"] = "   " })
	_, err := adapter.Submit(t.Context(), req)
	require.Error(t, err)
	assertBadRequest(t, err)
	assert.Zero(t, fake.count())

	// 图生无 prompt 是允许的（不在这里发明约束）。
	imgReq := submitRequest(core.MediaActionImageToVideo, []string{"https://cdn.example.com/a.png"},
		func(params map[string]any) { params["prompt"] = "" })
	_, err = adapter.Submit(t.Context(), imgReq)
	require.NoError(t, err)
}

// 测试目的：上游报文**逐字**对齐——model/prompt/duration/resolution/movement_amplitude，
// 且**不发送** Vidu 没有的上游字段（mode/aspect_ratio/size 与厂商私有字段）。
func TestSubmit_PayloadGolden(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	req := submitRequest(core.MediaActionImageToVideo, []string{"https://cdn.example.com/a.png"},
		func(params map[string]any) {
			params["prompt"] = "一只猫在弹钢琴"
			params["duration"] = 3
			params["resolution"] = "720p"
			// 冻结键集里 Vidu 不消费的三个键：给出值也不得出现在上游报文里。
			params["mode"] = "pro"
			params["aspect_ratio"] = "9:16"
			params["size"] = "720x1280"
		})
	req.UpstreamModel = "viduq2"

	_, err := adapter.Submit(t.Context(), req)
	require.NoError(t, err)

	body := decodeBody(t, fake.last(t).Body)
	assert.Equal(t, "viduq2", body["model"])
	assert.Equal(t, "一只猫在弹钢琴", body["prompt"])
	assert.Equal(t, float64(3), body["duration"])
	assert.Equal(t, "720p", body["resolution"])
	assert.Equal(t, "auto", body["movement_amplitude"], "movement_amplitude 必须固定 auto")

	for _, unsupported := range []string{"mode", "aspect_ratio", "size", "seed", "bgm", "callback_url", "payload"} {
		_, present := body[unsupported]
		assert.False(t, present, "不得向上游发送未纳入契约的字段 %s", unsupported)
	}
	assert.Len(t, body, 6, "上游报文只应有 model/images/prompt/duration/resolution/movement_amplitude")
}

// 测试目的：鉴权头是 `Token ` 前缀（**不是** Bearer），提交带 JSON 头。
func TestSubmit_RequestHeaders(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	_, err := adapter.Submit(t.Context(), textToVideoRequest(nil))
	require.NoError(t, err)

	header := fake.last(t).Header
	assert.Equal(t, "Token "+testAPIKey, header.Get("Authorization"))
	assert.NotContains(t, header.Get("Authorization"), "Bearer")
	assert.Equal(t, "application/json", header.Get("Content-Type"))
	assert.Equal(t, "application/json", header.Get("Accept"))
}

// 测试目的：vendorTaskID 是**逐字的上游 id**（Vidu 查询路径不含 action 段，故不编码）。
//
// 与 Kling 的差异：Kling 必须把 action 编进 vendorTaskID（查询路径含 action），
// Vidu 则不需要——这直接体现在返回值里不能有 `img2video:` 之类前缀。
func TestSubmit_VendorTaskIDIsRawUpstreamID(t *testing.T) {
	fake := startFakeUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		_, _ = w.Write([]byte(submitBody("v_upstream_raw_id_42", "queueing")))
	})
	adapter := newTestAdapter(t, fake)

	result, err := adapter.Submit(t.Context(), submitRequest(
		core.MediaActionImageToVideo, []string{"https://cdn.example.com/a.png"}, nil))
	require.NoError(t, err)
	assert.Equal(t, "v_upstream_raw_id_42", result.VendorTaskID)
	assert.NotContains(t, result.VendorTaskID, ":")
}

// 测试目的：提交响应的 state 走同一张状态映射表；缺省视为 queued；未知状态报错（协议失败）。
func TestSubmit_InitialStatus(t *testing.T) {
	cases := []struct {
		name      string
		state     string
		want      core.MediaStatus
		wantError bool
	}{
		{name: "created → queued", state: "created", want: core.MediaStatusQueued},
		{name: "queueing → queued", state: "queueing", want: core.MediaStatusQueued},
		{name: "processing → in_progress", state: "processing", want: core.MediaStatusInProgress},
		{name: "state 为空串 → 视为 queued", state: "", want: core.MediaStatusQueued},
		{name: "未知 state → 协议失败", state: "queued", wantError: true},
		{name: "大写变体不折叠 → 协议失败", state: "CREATED", wantError: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
				_, _ = w.Write([]byte(submitBody(upstreamTaskID, tc.state)))
			})
			adapter := newTestAdapter(t, fake)

			result, err := adapter.Submit(t.Context(), textToVideoRequest(nil))
			if tc.wantError {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, result.Status)
		})
	}
}

// 测试目的：提交响应缺 task_id → 协议失败 error（契约 3：拿不到上游 id 不能落库）。
func TestSubmit_MissingTaskIDIsError(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200, `{"state":"created"}`))
	adapter := newTestAdapter(t, fake)

	result, err := adapter.Submit(t.Context(), textToVideoRequest(nil))
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "task_id")
}

// 测试目的：上游业务失败（state=failed）在**提交**路径返回 Status=failed 且不报 error。
//
// 理由：上游任务已创建（有 task_id），返回 error 会让客户端重试并再建一个上游任务；
// MediaSubmitResult 没有 FailReason 字段，业务失败只能靠状态表达，调用方仍可从后续 Query 拿 err_code。
func TestSubmit_UpstreamStateFailedIsStatusNotError(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200,
		`{"task_id":"v_rejected","state":"failed","err_code":"content_moderation"}`))
	adapter := newTestAdapter(t, fake)

	result, err := adapter.Submit(t.Context(), textToVideoRequest(nil))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, core.MediaStatusFailed, result.Status)
	assert.Equal(t, "v_rejected", result.VendorTaskID)
}

// 测试目的：非 2xx → error，且错误文案**保留 HTTP 状态码数字**（便于 auth/transient 归类）；
// 非法 JSON 同样是 error（协议失败，不降级）。
func TestSubmit_TransportFailures(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		wantText   string
	}{
		{name: "401 鉴权失败", statusCode: 401, body: `{"message":"unauthorized"}`, wantText: "401"},
		{name: "403 无权限", statusCode: 403, body: `{"message":"forbidden"}`, wantText: "403"},
		{name: "429 限流", statusCode: 429, body: `{"message":"too many requests"}`, wantText: "429"},
		{name: "500 上游故障", statusCode: 500, body: `{"message":"internal"}`, wantText: "500"},
		{name: "200 但非法 JSON", statusCode: 200, body: `not-json`, wantText: "JSON"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondWith(tc.statusCode, tc.body))
			adapter := newTestAdapter(t, fake)

			result, err := adapter.Submit(t.Context(), textToVideoRequest(nil))
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.wantText)
			assert.NotErrorIs(t, err, core.ErrMediaTaskNotFound,
				"提交路径不得报 not_found（那是查询语义）")
		})
	}
}

// 测试目的：data URL 图片**解码后**超过 15728640 字节 → 提前 400（不发上游）。
//
// 设计文档 §3.2 的对外 multipart 上限是 15 MiB；适配器在发上游之前再卡一次，
// 避免把一个上游必然拒绝的巨大 body 推过去（浪费配额与时长）。
func TestSubmit_OversizeDataURLRejected(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	// base64 长度取 4 的倍数：decoded = len/4*3 需 > 15728640。
	const limit = 15728640
	payload := strings.Repeat("A", (limit/3+2)*4)
	require.Greater(t, len(payload)/4*3, limit, "测试数据本身必须超限")

	req := submitRequest(core.MediaActionImageToVideo,
		[]string{"data:image/png;base64," + payload}, nil)

	_, err := adapter.Submit(t.Context(), req)
	require.Error(t, err)
	assertBadRequest(t, err)
	assert.Contains(t, err.Error(), "15728640")
	assert.Zero(t, fake.count(), "超限图片必须在发上游之前拦住")
}

// 测试目的：正常大小的 data URL、以及非 data URL 的普通 http(s) 图片都要放行；
// 形态非法（缺 `,` / base64 长度不是 4 的倍数 / padding 非法）→ 400。
func TestSubmit_DataURLImageValidation(t *testing.T) {
	cases := []struct {
		name    string
		image   string
		wantErr bool
	}{
		{name: "小 data URL（base64）", image: "data:image/png;base64,iVBORw0KGgo="},
		{name: "data URL 带换行的 base64", image: "data:image/png;base64,iVBORw0K\nggo="},
		{name: "非 base64 的 data URL", image: "data:text/plain,hello"},
		{name: "普通 https URL", image: "https://cdn.example.com/a.png"},
		{name: "http URL", image: "http://cdn.example.com/a.png"},
		{name: "data URL 缺逗号", image: "data:image/png;base64", wantErr: true},
		{name: "base64 长度不是 4 的倍数", image: "data:image/png;base64,iVBORw0KGg", wantErr: true},
		{name: "base64 padding 非法", image: "data:image/png;base64,iVBORw===", wantErr: true},
		{name: "base64 载荷为空", image: "data:image/png;base64,", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			req := submitRequest(core.MediaActionImageToVideo, []string{tc.image}, nil)
			result, err := adapter.Submit(t.Context(), req)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				assertBadRequest(t, err)
				assert.Zero(t, fake.count())
				return
			}
			require.NoError(t, err)
			body := decodeBody(t, fake.last(t).Body)
			images, ok := body["images"].([]any)
			require.True(t, ok)
			require.Len(t, images, 1)
			assert.Equal(t, tc.image, images[0])
		})
	}
}

// 测试目的：images 里的空串/类型错误 → 400（与空数组区分：空数组是合法的纯文生）。
func TestSubmit_InvalidImagesInput(t *testing.T) {
	cases := []struct {
		name   string
		images any
	}{
		{name: "元素为空串", images: []string{"https://cdn.example.com/a.png", "   "}},
		{name: "不是数组", images: "https://cdn.example.com/a.png"},
		{name: "数组元素不是字符串", images: []any{1}},
		{name: "数组元素为 null", images: []any{nil}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			req := textToVideoRequest(func(params map[string]any) { params["images"] = tc.images })
			req.Action = core.MediaActionTextToVideo

			_, err := adapter.Submit(t.Context(), req)
			require.Error(t, err)
			assertBadRequest(t, err)
			assert.Zero(t, fake.count())
		})
	}
}

// 测试目的：UpstreamModel 为空时兜底 `viduq1`（与 New API 一致），且该家族的默认
// duration=5 / resolution=1080p 一并生效。
func TestSubmit_DefaultModelWhenUpstreamModelEmpty(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	req := textToVideoRequest(nil)
	req.UpstreamModel = ""

	_, err := adapter.Submit(t.Context(), req)
	require.NoError(t, err)

	body := decodeBody(t, fake.last(t).Body)
	assert.Equal(t, "viduq1", body["model"])
	assert.Equal(t, float64(5), body["duration"])
	assert.Equal(t, "1080p", body["resolution"])
}

// 测试目的：参考生视频只能用基础 `viduq2`（New API 的硬编码）——viduq2 家族的后缀被去掉，
// 其它模型原样透传（不发明替换规则）。
func TestSubmit_ReferenceModelNormalized(t *testing.T) {
	cases := []struct {
		name      string
		model     string
		wantModel string
	}{
		{name: "viduq2-pro → viduq2", model: "viduq2-pro", wantModel: "viduq2"},
		{name: "viduq2-turbo → viduq2", model: "viduq2-turbo", wantModel: "viduq2"},
		{name: "viduq2 原样", model: "viduq2", wantModel: "viduq2"},
		{name: "非 viduq2 家族原样透传", model: "vidu1.5", wantModel: "vidu1.5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, nil)
			adapter := newTestAdapter(t, fake)

			req := submitRequest(core.MediaActionReferenceToVideo,
				[]string{"u1", "u2", "u3"}, nil)
			req.UpstreamModel = tc.model

			_, err := adapter.Submit(t.Context(), req)
			require.NoError(t, err)
			assert.Equal(t, tc.wantModel, decodeBody(t, fake.last(t).Body)["model"])
			assert.Equal(t, "/ent/v2/reference2video", fake.last(t).Path)
		})
	}
}

// 测试目的：非 viduq2 模型走参考生视频时**不做**后缀改写（避免发明规则）。
func TestSubmit_ReferenceModelNormalizationIsFamilyScoped(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	req := submitRequest(core.MediaActionReferenceToVideo, []string{"u1", "u2", "u3"}, nil)
	req.UpstreamModel = "vidu2.0-pro"

	_, err := adapter.Submit(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "vidu2.0-pro", decodeBody(t, fake.last(t).Body)["model"])
}

// 测试目的：请求体是合法 JSON 且字段类型符合上游契约（duration 是**数字**而非字符串）。
func TestSubmit_DurationIsJSONNumber(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	_, err := adapter.Submit(t.Context(), textToVideoRequest(func(params map[string]any) {
		params["duration"] = 8
	}))
	require.NoError(t, err)

	raw := string(fake.last(t).Body)
	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded))
	assert.Equal(t, "8", strings.TrimSpace(string(decoded["duration"])),
		"duration 必须是 JSON 数字（Kling 用字符串，Vidu 不是）")
}

// 测试目的：nil 请求不得 panic，返回 400 类错误。
func TestSubmit_NilRequest(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newTestAdapter(t, fake)

	result, err := adapter.Submit(t.Context(), nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assertBadRequest(t, err)
	assert.Zero(t, fake.count())
}

// 测试目的：endpoint 带自建反代的路径前缀时，提交路径仍拼在正确位置
// （前缀在 /ent/v2 之前，不能吃掉或重复）。
func TestSubmit_PathWithCustomPrefix(t *testing.T) {
	fake := startFakeUpstream(t, nil)
	adapter := newAdapterWith(t, providers.MediaAdapterParams{
		Type:        vidu.MediaProviderType,
		BaseURL:     fake.URL + "/gw/vidu",
		Credentials: testCredentialsBare,
	})

	_, err := adapter.Submit(t.Context(), submitRequest(
		core.MediaActionFirstTailToVideo, []string{"u1", "u2"}, nil))
	require.NoError(t, err)
	assert.Equal(t, "/gw/vidu/ent/v2/start-end2video", fake.last(t).Path)
}
