package kling_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/core"
)

// respondToQuery 构造「POST 回提交成功、GET 回指定报文」的脚本，
// 这样查询用例仍能走完整的 Submit → Query 链路（vendorTaskID 由真实提交产出）。
func respondToQuery(statusCode int, body string) func(w http.ResponseWriter, r *http.Request, _ []byte) {
	return func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(submitBody(upstreamTaskID, "submitted")))
			return
		}
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}
}

// ---------------------------------------------------------------------------
// action 记忆：查询必须复用提交时的 action 段
// ---------------------------------------------------------------------------

// 测试目的：`GET /v1/videos/{action}/{task_id}` 的 action 段**必须与提交时一致**，
// 否则上游按另一端点查（或直接 404），任务永远轮询不到。
//
// 本用例覆盖三条端到端路径：Submit(text_to_video) → Query 用 text2video；
// Submit(image_to_video) → Query 用 image2video；首尾帧提交也落在 image2video。
// 同时锁定 vendorTaskID 的编码形状（无状态记忆方案，见 encodeVendorTaskID 注释）。
func TestQuery_ReusesActionSegmentFromSubmit(t *testing.T) {
	cases := []struct {
		name         string
		action       core.MediaAction
		images       []string
		wantPath     string
		wantVendorID string
	}{
		{
			name:         "文生视频",
			action:       core.MediaActionTextToVideo,
			wantPath:     "/v1/videos/text2video/" + upstreamTaskID,
			wantVendorID: "text2video:" + upstreamTaskID,
		},
		{
			name:         "图生视频",
			action:       core.MediaActionImageToVideo,
			images:       []string{"https://cdn.example.com/a.png"},
			wantPath:     "/v1/videos/image2video/" + upstreamTaskID,
			wantVendorID: "image2video:" + upstreamTaskID,
		},
		{
			name:         "首尾帧（上游端点仍是 image2video）",
			action:       core.MediaActionFirstTailToVideo,
			images:       []string{"https://cdn.example.com/first.png", "https://cdn.example.com/last.png"},
			wantPath:     "/v1/videos/image2video/" + upstreamTaskID,
			wantVendorID: "image2video:" + upstreamTaskID,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondToQuery(http.StatusOK, queryBody("processing", "", "", "")))
			adapter := newTestAdapter(t, fake)

			submitted, err := adapter.Submit(t.Context(), submitRequest(tc.action, tc.images, nil))
			require.NoError(t, err)
			require.Equal(t, tc.wantVendorID, submitted.VendorTaskID,
				"vendorTaskID 必须编码 action（无状态记忆方案，见 encodeVendorTaskID 注释）")

			status, err := adapter.Query(t.Context(), submitted.VendorTaskID)
			require.NoError(t, err)
			require.NotNil(t, status)
			assert.Equal(t, core.MediaStatusInProgress, status.Status)

			assert.Equal(t, tc.wantPath, fake.last(t).Path,
				"查询路径的 action 段必须与提交时一致")
		})
	}
}

// 测试目的：不是本适配器生成的 vendorTaskID（缺前缀 / 前缀未知 / 缺任务 id）必须报错，
// 且**不得**谎报 not_found（那会让调用方立刻放弃轮询），也不得打上游。
func TestQuery_RejectsForeignVendorTaskID(t *testing.T) {
	cases := []struct {
		name         string
		vendorTaskID string
	}{
		{name: "裸上游 id（无前缀）", vendorTaskID: upstreamTaskID},
		{name: "未知 action 前缀", vendorTaskID: "image2video-v2:" + upstreamTaskID},
		{name: "空 action 段", vendorTaskID: ":" + upstreamTaskID},
		{name: "缺任务 id 段", vendorTaskID: "text2video:"},
		{name: "空串", vendorTaskID: ""},
		{name: "只有空白", vendorTaskID: "   "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondToQuery(http.StatusOK, queryBody("processing", "", "", "")))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(context.Background(), tc.vendorTaskID)
			require.Error(t, err)
			assert.Nil(t, status)
			assert.False(t, errors.Is(err, core.ErrMediaTaskNotFound),
				"无法确定查询路径 ≠ 上游说任务不存在")
			assert.Zero(t, fake.count(), "解析失败不得打上游")
		})
	}
}

// ---------------------------------------------------------------------------
// 状态归一
// ---------------------------------------------------------------------------

// 测试目的：上游状态词逐字映射到对外词表（submitted/processing/succeed/failed）。
func TestQuery_StatusMapping(t *testing.T) {
	videos := `[{"id":"v-1","url":"https://cdn.example.com/a.mp4","duration":"5"}]`

	cases := []struct {
		name       string
		upstream   string
		wantStatus core.MediaStatus
	}{
		{name: "submitted → queued", upstream: "submitted", wantStatus: core.MediaStatusQueued},
		{name: "processing → in_progress", upstream: "processing", wantStatus: core.MediaStatusInProgress},
		{name: "succeed → completed", upstream: "succeed", wantStatus: core.MediaStatusCompleted},
		{name: "failed → failed", upstream: "failed", wantStatus: core.MediaStatusFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondToQuery(http.StatusOK,
				queryBody(tc.upstream, "审核不通过", videos, "")))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
			require.NoError(t, err)
			require.NotNil(t, status)
			assert.Equal(t, tc.wantStatus, status.Status)
			assert.True(t, status.Status.IsValid(), "对外状态必须在四个枚举内")
		})
	}
}

// 测试目的：未知状态词是**协议失败**（core/media_task.go 契约 2）——既不得当 completed
// （会给出错的成片并漏计费），也不得静默当 in_progress（无限轮询）。
//
// 覆盖插件反向渲染表的词（QUEUED/SUCCESS/NOT_START/IN_PROGRESS/FAILURE）与大小写变体，
// 证明我们没有把「内部态 → 对外形状」那张表反着抄进来。
func TestQuery_UnknownStatusIsProtocolError(t *testing.T) {
	cases := []string{
		"SUCCESS", "SUCCEED", "Succeed", "queued", "QUEUED", "NOT_START",
		"IN_PROGRESS", "FAILURE", "finished", "0", "",
	}

	for _, upstream := range cases {
		t.Run("上游状态 "+upstream, func(t *testing.T) {
			fake := startFakeUpstream(t, respondToQuery(http.StatusOK,
				queryBody(upstream, "", "", "")))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
			require.Error(t, err, "未知状态词必须报错")
			assert.Nil(t, status)
			assert.False(t, errors.Is(err, core.ErrMediaTaskNotFound))
			if upstream != "" {
				assert.Contains(t, err.Error(), "未知的上游任务状态")
			} else {
				assert.Contains(t, err.Error(), "task_status")
			}
		})
	}
}

// 测试目的：状态词两侧空白被归一（上游偶发带空格不得被当成未知词）。
func TestQuery_StatusTrimsSurroundingWhitespace(t *testing.T) {
	fake := startFakeUpstream(t, respondToQuery(http.StatusOK,
		queryBody(" succeed ", "", `[{"id":"v-1","url":"https://cdn.example.com/a.mp4","duration":"5"}]`, "")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, core.MediaStatusCompleted, status.Status)
}

// ---------------------------------------------------------------------------
// 产物 / 失败原因 / 用量
// ---------------------------------------------------------------------------

// 测试目的：`data.task_result.videos[]` **全量**转产物（New API 只取首个是它的缺陷），
// Kind 一律 video，MimeType 尽力按扩展名推断。
func TestQuery_CompletedCollectsAllArtifacts(t *testing.T) {
	videos := `[
		{"id":"v-1","url":"https://cdn.example.com/a.mp4","duration":"5"},
		{"id":"v-2","url":"https://cdn.example.com/b.mov?sign=abc","duration":"5"},
		{"id":"v-3","url":"https://cdn.example.com/c","duration":"5"},
		{"id":"v-4","url":"","duration":"5"}
	]`
	fake := startFakeUpstream(t, respondToQuery(http.StatusOK, queryBody("succeed", "", videos, "1.0")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
	require.NoError(t, err)
	require.NotNil(t, status)
	require.Equal(t, core.MediaStatusCompleted, status.Status)

	// URL 为空的条目被跳过，其余 3 条**全部**保留。
	require.Len(t, status.Artifacts, 3)
	assert.Equal(t, "https://cdn.example.com/a.mp4", status.Artifacts[0].URL)
	assert.Equal(t, "https://cdn.example.com/b.mov?sign=abc", status.Artifacts[1].URL)
	assert.Equal(t, "https://cdn.example.com/c", status.Artifacts[2].URL)
	for _, artifact := range status.Artifacts {
		assert.Equal(t, core.MediaKindVideo, artifact.Kind)
	}
	assert.Equal(t, "video/mp4", status.Artifacts[0].MimeType)
	assert.Equal(t, "video/quicktime", status.Artifacts[1].MimeType)
	assert.Empty(t, status.Artifacts[2].MimeType, "扩展名未知时留空，不瞎猜")
}

// 测试目的：上游说 succeed 却没有产物 → 协议失败（core 契约要求 completed 时 Artifacts
// 非空；返回空产物的 completed 会让上层落库「成功但无交付物」的任务并记账）。
func TestQuery_CompletedWithoutArtifactsIsError(t *testing.T) {
	cases := []struct {
		name   string
		videos string
	}{
		{name: "videos 为空数组", videos: `[]`},
		{name: "videos 全是空 URL", videos: `[{"id":"v-1","url":""}]`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondToQuery(http.StatusOK, queryBody("succeed", "", tc.videos, "")))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
			require.Error(t, err)
			assert.Nil(t, status)
			assert.Contains(t, err.Error(), "videos")
		})
	}
}

// 测试目的：业务失败必须返回 `(status=failed, nil)`——**绝不是 error**
// （返回 error 会被上层重试放大成重复计费，见 core/media_task.go 契约 1）。
func TestQuery_BusinessFailureReturnsStatusNotError(t *testing.T) {
	fake := startFakeUpstream(t, respondToQuery(http.StatusOK,
		queryBody("failed", "input image violates content policy", "", "0")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
	require.NoError(t, err, "业务失败必须返回 (status, nil)，不得返回 error")
	require.NotNil(t, status)
	assert.Equal(t, core.MediaStatusFailed, status.Status)
	assert.True(t, status.Status.IsTerminal())
	assert.Equal(t, "input image violates content policy", status.FailReason)
	assert.Empty(t, status.Artifacts)
}

// 测试目的：failed 但上游没给 task_status_msg 时，FailReason 仍必须非空（core 契约）。
func TestQuery_FailedWithoutMessageStillHasFailReason(t *testing.T) {
	fake := startFakeUpstream(t, respondToQuery(http.StatusOK, queryBody("failed", "", "", "")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, core.MediaStatusFailed, status.Status)
	assert.NotEmpty(t, status.FailReason, "failed 时 FailReason 必须非空")
}

// 测试目的：终态用量抽取——`data.final_unit_deduction` 优先，并能带上 videos[].duration
// 供对账（core.MediaCostInput.OutputSeconds 就取 usage 里的 duration）。
func TestQuery_TerminalUsage(t *testing.T) {
	videos := `[{"id":"v-1","url":"https://cdn.example.com/a.mp4","duration":"5"}]`

	cases := []struct {
		name       string
		body       string
		wantUnits  any
		wantSecond any
		wantNil    bool
	}{
		{
			name:       "数字形态",
			body:       queryBody("succeed", "", videos, "3.5"),
			wantUnits:  float64(3.5),
			wantSecond: float64(5),
		},
		{
			name:       "字符串形态归一成数字",
			body:       queryBody("succeed", "", videos, `"7"`),
			wantUnits:  float64(7),
			wantSecond: float64(5),
		},
		{
			name:       "无法解析的字符串原样保留（不改写成 0）",
			body:       queryBody("succeed", "", videos, `"n/a"`),
			wantUnits:  "n/a",
			wantSecond: float64(5),
		},
		{
			name:       "上游未提供扣减、只有时长",
			body:       queryBody("succeed", "", videos, ""),
			wantSecond: float64(5),
		},
		{
			name:    "既无扣减也无时长 → nil（不伪造 0）",
			body:    queryBody("succeed", "", `[{"id":"v-1","url":"https://cdn.example.com/a.mp4"}]`, ""),
			wantNil: true,
		},
		{
			name:      "失败终态也采集用量（供对账）",
			body:      queryBody("failed", "审核拒绝", "", "0.5"),
			wantUnits: float64(0.5),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondToQuery(http.StatusOK, tc.body))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
			require.NoError(t, err)
			require.NotNil(t, status)

			if tc.wantNil {
				assert.Nil(t, status.Usage, "拿不到用量必须留 nil")
				return
			}
			require.NotNil(t, status.Usage)
			assert.Equal(t, tc.wantUnits, status.Usage["final_unit_deduction"])
			if tc.wantSecond == nil {
				assert.NotContains(t, status.Usage, "duration")
				return
			}
			assert.Equal(t, tc.wantSecond, status.Usage["duration"])
		})
	}
}

// 测试目的：中间态不采集用量（final_unit_deduction 在中间态不是最终扣减，记进去会误导对账）。
func TestQuery_NonTerminalHasNoUsage(t *testing.T) {
	fake := startFakeUpstream(t, respondToQuery(http.StatusOK,
		queryBody("processing", "", "", "1.5")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, core.MediaStatusInProgress, status.Status)
	assert.Nil(t, status.Usage)
	assert.Empty(t, status.Progress, "Kling 不返回进度字段，不伪造")
}

// ---------------------------------------------------------------------------
// 传输 / 协议失败
// ---------------------------------------------------------------------------

// 测试目的：上游 404 → 包装 core.ErrMediaTaskNotFound（调用方据此停止轮询）。
func TestQuery_NotFoundWrapsSentinel(t *testing.T) {
	fake := startFakeUpstream(t, respondToQuery(http.StatusNotFound,
		`{"code":1201,"message":"task not found"}`))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
	require.Error(t, err)
	assert.Nil(t, status)
	assert.True(t, errors.Is(err, core.ErrMediaTaskNotFound),
		"404 必须包装 core.ErrMediaTaskNotFound，实际: %v", err)
	assert.Contains(t, err.Error(), "404")
}

// 测试目的：其它传输/协议失败（5xx / 401 / 非零 code / 非法 JSON / 缺 data）→ error
// 且不是 not_found（应被归类为可恢复）。
func TestQuery_TransportAndProtocolFailures(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		wantInErr  string
	}{
		{
			name:       "上游 500",
			statusCode: http.StatusInternalServerError,
			body:       `{"code":500,"message":"internal error"}`,
			wantInErr:  "500",
		},
		{
			name:       "上游 429",
			statusCode: http.StatusTooManyRequests,
			body:       `{"code":1303,"message":"rate limited"}`,
			wantInErr:  "429",
		},
		{
			name:       "上游 401",
			statusCode: http.StatusUnauthorized,
			body:       `{"code":1002,"message":"auth failed"}`,
			wantInErr:  "401",
		},
		{
			name:       "业务错误码",
			statusCode: http.StatusOK,
			body:       `{"code":1201,"message":"invalid task id","data":{}}`,
			wantInErr:  "1201",
		},
		{
			name:       "非法 JSON",
			statusCode: http.StatusOK,
			body:       `<html>502 Bad Gateway</html>`,
			wantInErr:  "JSON",
		},
		{
			name:       "缺 data.task_status",
			statusCode: http.StatusOK,
			body:       `{"code":0,"message":"SUCCEED","data":{}}`,
			wantInErr:  "task_status",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondToQuery(tc.statusCode, tc.body))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
			require.Error(t, err)
			assert.Nil(t, status)
			assert.Contains(t, err.Error(), tc.wantInErr)
			assert.False(t, errors.Is(err, core.ErrMediaTaskNotFound))
		})
	}
}

// 测试目的：上游响应体超长时错误文案按 rune 截断，不得把整个 body 塞进日志/错误。
func TestQuery_TruncatesHugeErrorBody(t *testing.T) {
	huge := make([]rune, 4096)
	for i := range huge {
		huge[i] = '错'
	}
	fake := startFakeUpstream(t, respondToQuery(http.StatusBadGateway, string(huge)))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), queryVendorTaskID(t, adapter))
	require.Error(t, err)
	assert.Nil(t, status)
	assert.Contains(t, err.Error(), "已截断")
	assert.Less(t, len(err.Error()), 2048, "错误文案必须被截断")
}

// 测试目的：请求上下文取消/超时时返回 error（可恢复语义），而不是伪造终态。
func TestQuery_ContextCanceledReturnsError(t *testing.T) {
	fake := startFakeUpstream(t, respondToQuery(http.StatusOK, queryBody("succeed", "", "", "")))
	adapter := newTestAdapter(t, fake)

	vendorTaskID := queryVendorTaskID(t, adapter)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	status, err := adapter.Query(ctx, vendorTaskID)
	require.Error(t, err)
	assert.Nil(t, status)
}
