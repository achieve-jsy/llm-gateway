package vidu_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/core"
)

// 测试目的：5 个上游状态词逐字映射到对外词表（created/queueing→queued、
// processing→in_progress、success→completed、failed→failed）。
func TestQuery_StatusMapping(t *testing.T) {
	cases := []struct {
		name        string
		state       string
		creations   string
		want        core.MediaStatus
		wantArtifac bool
	}{
		{
			name: "created → queued", state: "created", want: core.MediaStatusQueued,
		},
		{
			name: "queueing → queued", state: "queueing", want: core.MediaStatusQueued,
		},
		{
			name: "processing → in_progress", state: "processing", want: core.MediaStatusInProgress,
		},
		{
			name: "success → completed", state: "success",
			creations:   creationsJSON([2]string{"https://cdn.example.com/out.mp4", "https://cdn.example.com/cover.jpg"}),
			want:        core.MediaStatusCompleted,
			wantArtifac: true,
		},
		{
			name: "failed → failed", state: "failed",
			want: core.MediaStatusFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondWith(200, queryBody(tc.state, "", tc.creations, "")))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), upstreamTaskID)
			require.NoError(t, err)
			require.NotNil(t, status)
			assert.Equal(t, tc.want, status.Status)
			if tc.wantArtifac {
				require.NotEmpty(t, status.Artifacts)
				return
			}
			assert.Empty(t, status.Artifacts)
		})
	}
}

// 测试目的：**未知状态词必须报错**（core/media_task.go 契约 2）——既不当成功，也不当在途。
//
// 特别覆盖 `queued`（上游拼写是 `queueing`，双 e）与大小写变体：不做近义词兜底、
// 不做大小写折叠（折叠会把上游新增的未知词吞成已知词）。
func TestQuery_UnknownStatusIsError(t *testing.T) {
	cases := []struct {
		name  string
		state string
	}{
		{name: "queued（上游拼写是 queueing）", state: "queued"},
		{name: "queuing（单 e）", state: "queuing"},
		{name: "succeed（那是 Kling 的词）", state: "succeed"},
		{name: "success 大写变体", state: "SUCCESS"},
		{name: "Success 首字母大写", state: "Success"},
		{name: "带前后空白的未知词", state: " queued "},
		{name: "空字符串", state: ""},
		{name: "未知新词", state: "moderation_blocked"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondWith(200, queryBody(tc.state, "", "", "")))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), upstreamTaskID)
			require.Error(t, err, "未知状态词必须报错，绝不当成功")
			assert.Nil(t, status)
			assert.NotErrorIs(t, err, core.ErrMediaTaskNotFound)
		})
	}
}

// 测试目的：**多产物全部取回**（New API 只取 creations[0] 是它的缺陷）。
func TestQuery_MultipleArtifactsAllReturned(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200, queryBody("success", "",
		creationsJSON(
			[2]string{"https://cdn.example.com/a.mp4", "https://cdn.example.com/a.jpg"},
			[2]string{"https://cdn.example.com/b.mp4", "https://cdn.example.com/b.jpg"},
			[2]string{"https://cdn.example.com/c.mov", "https://cdn.example.com/c.jpg"},
		), "")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), upstreamTaskID)
	require.NoError(t, err)
	require.Len(t, status.Artifacts, 3, "多产物必须全取")
	assert.Equal(t, "https://cdn.example.com/a.mp4", status.Artifacts[0].URL)
	assert.Equal(t, "https://cdn.example.com/b.mp4", status.Artifacts[1].URL)
	assert.Equal(t, "https://cdn.example.com/c.mov", status.Artifacts[2].URL)
	for i, artifact := range status.Artifacts {
		assert.Equal(t, core.MediaKindVideo, artifact.Kind, "artifacts[%d].Kind", i)
	}
	assert.Equal(t, "video/mp4", status.Artifacts[0].MimeType)
	assert.Equal(t, "video/quicktime", status.Artifacts[2].MimeType)
}

// 测试目的：creations 里混有空 url 的条目时跳过它、**保留其余条目**（而不是整体放弃）。
func TestQuery_SkipsEmptyArtifactURLs(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200, queryBody("success", "",
		`[{"id":"c0","url":""},{"id":"c1","url":"https://cdn.example.com/ok.mp4"}]`, "")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), upstreamTaskID)
	require.NoError(t, err)
	require.Len(t, status.Artifacts, 1)
	assert.Equal(t, "https://cdn.example.com/ok.mp4", status.Artifacts[0].URL)
}

// 测试目的：**业务失败返回 (status=failed, nil) 而不是 error**（core 契约 1，最重要）。
//
// 把业务失败返回成 error 会被上层重试放大成重复计费。
func TestQuery_BusinessFailureReturnsStatusNotError(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200, queryBody("failed", "content_moderation", "", "")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), upstreamTaskID)
	require.NoError(t, err, "业务失败必须是 (status, nil)，不是 error")
	require.NotNil(t, status)
	assert.Equal(t, core.MediaStatusFailed, status.Status)
	assert.Equal(t, "content_moderation", status.FailReason, "失败原因取 err_code")
	assert.Empty(t, status.Artifacts)
	assert.True(t, status.Status.IsTerminal())
}

// 测试目的：上游 failed 但不给 err_code 时，FailReason 仍必须非空（core 契约）。
func TestQuery_FailedWithoutErrCodeStillHasReason(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200, queryBody("failed", "", "", "")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), upstreamTaskID)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, core.MediaStatusFailed, status.Status)
	assert.NotEmpty(t, status.FailReason)
}

// 测试目的：任务不存在（HTTP 404）→ 包装 core.ErrMediaTaskNotFound 的 error。
func TestQuery_NotFoundWrapsSentinel(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(404, `{"message":"task not found"}`))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), upstreamTaskID)
	require.Error(t, err)
	assert.Nil(t, status)
	assert.ErrorIs(t, err, core.ErrMediaTaskNotFound)
	assert.Contains(t, err.Error(), "404", "错误文案保留 HTTP 状态码数字")
}

// 测试目的：非 2xx（除 404）→ error 且文案保留状态码数字；不得误报 not_found。
func TestQuery_TransportFailures(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		wantText   string
	}{
		{name: "401 鉴权失败", statusCode: 401, body: `{"message":"unauthorized"}`, wantText: "401"},
		{name: "403 无权限", statusCode: 403, body: `{"message":"forbidden"}`, wantText: "403"},
		{name: "429 限流", statusCode: 429, body: `{"message":"too many requests"}`, wantText: "429"},
		{name: "500 上游故障", statusCode: 500, body: `{"message":"internal error"}`, wantText: "500"},
		{name: "400 参数错误", statusCode: 400, body: `{"message":"bad request"}`, wantText: "400"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondWith(tc.statusCode, tc.body))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), upstreamTaskID)
			require.Error(t, err)
			assert.Nil(t, status)
			assert.Contains(t, err.Error(), tc.wantText)
			assert.NotErrorIs(t, err, core.ErrMediaTaskNotFound)
		})
	}
}

// 测试目的：非法 JSON → error（协议失败，不降级成 completed/failed）。
func TestQuery_InvalidJSONIsError(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200, `{"state":`))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), upstreamTaskID)
	require.Error(t, err)
	assert.Nil(t, status)
	assert.Contains(t, err.Error(), "JSON")
}

// 测试目的：success 却给不出产物 → error（协议失败），**不得**降级成 completed
// （否则会落库一个「成功但没有交付物」的任务并按用量记账）。
func TestQuery_SuccessWithoutArtifactsIsError(t *testing.T) {
	cases := []struct {
		name      string
		creations string
	}{
		{name: "creations 缺失", creations: ""},
		{name: "creations 为空数组", creations: "[]"},
		{name: "creations 里 url 全为空", creations: `[{"id":"c0","url":""}]`},
		{name: "creations 元素缺 url 字段", creations: `[{"id":"c0"}]`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondWith(200, queryBody("success", "", tc.creations, "")))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), upstreamTaskID)
			require.Error(t, err, "success 但无产物属协议失败")
			assert.Nil(t, status)
		})
	}
}

// 测试目的：查询请求的形状——路径 `/ent/v2/tasks/{id}/creations`（**不含 action 段**）、
// 鉴权头 `Token ` 前缀、Accept 头；vendorTaskID 逐字使用（不解析、不强加前缀）。
func TestQuery_RequestShape(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200, queryBody("processing", "", "", "")))
	adapter := newTestAdapter(t, fake)

	const vendorTaskID = "v_custom_task_id_7f3a"
	_, err := adapter.Query(t.Context(), vendorTaskID)
	require.NoError(t, err)

	request := fake.last(t)
	assert.Equal(t, "GET", request.Method)
	assert.Equal(t, "/ent/v2/tasks/"+vendorTaskID+"/creations", request.Path)
	assert.NotContains(t, request.Path, "img2video", "Vidu 查询路径不含 action 段")
	assert.Equal(t, "Token "+testAPIKey, request.Header.Get("Authorization"))
	assert.NotContains(t, request.Header.Get("Authorization"), "Bearer")
	assert.Equal(t, "application/json", request.Header.Get("Accept"))
	assert.Empty(t, request.Body)
}

// 测试目的：vendorTaskID 首尾空白被归一；**空 id 报错**且不得谎报 not_found
// （谎报会让调用方立刻停止轮询并计入失败分类）。
func TestQuery_EmptyVendorTaskIDIsError(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200, queryBody("created", "", "", "")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), "   ")
	require.Error(t, err)
	assert.Nil(t, status)
	assert.NotErrorIs(t, err, core.ErrMediaTaskNotFound)
	assert.Zero(t, fake.count(), "空 id 不得请求上游")

	// 带空白的合法 id 会被归一后照常查询。
	_, err = adapter.Query(t.Context(), " "+upstreamTaskID+" ")
	require.NoError(t, err)
	assert.Equal(t, "/ent/v2/tasks/"+upstreamTaskID+"/creations", fake.last(t).Path)
}

// 测试目的：用量口径 —— **上游没给就留 nil，绝不伪造**（同一形状的 Kling 因有
// final_unit_deduction 而能采集，Vidu 没有）。
func TestQuery_UsageNotFabricated(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantUsage map[string]any
	}{
		{
			name:      "响应里没有任何用量字段 → Usage 为 nil",
			body:      `{"task_id":"` + upstreamTaskID + `","state":"success","creations":[{"url":"https://cdn.example.com/a.mp4"}]}`,
			wantUsage: nil,
		},
		{
			name: "上游返回 credits（数字）→ 原样采集",
			body: `{"task_id":"` + upstreamTaskID + `","state":"success","credits":12,` +
				`"creations":[{"url":"https://cdn.example.com/a.mp4"}]}`,
			wantUsage: map[string]any{"credits": float64(12)},
		},
		{
			name: "上游返回 credits（数字字符串）→ 归一为数字",
			body: `{"task_id":"` + upstreamTaskID + `","state":"success","credits":"3.5",` +
				`"creations":[{"url":"https://cdn.example.com/a.mp4"}]}`,
			wantUsage: map[string]any{"credits": float64(3.5)},
		},
		{
			name: "上游返回 duration + resolution → 一并采集（供退化计价对照）",
			body: `{"task_id":"` + upstreamTaskID + `","state":"success","duration":8,"resolution":"720p",` +
				`"creations":[{"url":"https://cdn.example.com/a.mp4"}]}`,
			wantUsage: map[string]any{"duration": float64(8), "resolution": "720p"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondWith(200, tc.body))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), upstreamTaskID)
			require.NoError(t, err)
			if tc.wantUsage == nil {
				assert.Nil(t, status.Usage, "上游没给用量时必须留 nil（空 map 会被当成『上游给了用量』）")
				return
			}
			require.NotNil(t, status.Usage)
			for key, want := range tc.wantUsage {
				assert.Equal(t, want, status.Usage[key], "usage[%s]", key)
			}
		})
	}
}

// 测试目的：用量只在**终态**采集（中间态的 credits 不是最终扣减，记进去会误导对账）。
func TestQuery_UsageOnlyCollectedAtTerminalState(t *testing.T) {
	body := `{"task_id":"` + upstreamTaskID + `","state":"processing","credits":12}`
	fake := startFakeUpstream(t, respondWith(200, body))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), upstreamTaskID)
	require.NoError(t, err)
	assert.Equal(t, core.MediaStatusInProgress, status.Status)
	assert.Nil(t, status.Usage, "中间态不采集用量")
}

// 测试目的：progress 原样透传（数字 → 十进制文本、字符串 → 原文、缺失 → 空串），
// 认不出时**绝不猜测**（猜 100 会让调用方以为完成了）。
func TestQuery_ProgressPassthrough(t *testing.T) {
	cases := []struct {
		name         string
		progressJSON string
		want         string
	}{
		{name: "数字百分比", progressJSON: "42", want: "42"},
		{name: "小数百分比", progressJSON: "42.5", want: "42.5"},
		{name: "字符串百分比", progressJSON: `"45%"`, want: "45%"},
		{name: "阶段名", progressJSON: `"processing"`, want: "processing"},
		{name: "缺失 → 空串", progressJSON: "", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := startFakeUpstream(t, respondWith(200,
				queryBody("processing", "", "", tc.progressJSON)))
			adapter := newTestAdapter(t, fake)

			status, err := adapter.Query(t.Context(), upstreamTaskID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, status.Progress)
		})
	}
}

// 测试目的：查询响应体读取有上限（1 MiB），异常大响应不得把内存打满；
// 超限后 JSON 必然截断 → 协议失败 error（而不是 OOM 或静默成功）。
func TestQuery_OversizeResponseIsBounded(t *testing.T) {
	huge := `{"state":"success","creations":[{"url":"` +
		strings.Repeat("x", 1<<20+1024) + `"}]}`
	fake := startFakeUpstream(t, respondWith(200, huge))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), upstreamTaskID)
	require.Error(t, err, "超过 1 MiB 的响应被截断后属协议失败")
	assert.Nil(t, status)
}

// 测试目的：查询路径里对上游 id 做 URL 转义（避免 id 里的特殊字符改变路径结构）。
func TestQuery_EscapesVendorTaskID(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200, queryBody("processing", "", "", "")))
	adapter := newTestAdapter(t, fake)

	_, err := adapter.Query(t.Context(), "v_a/b c")
	require.NoError(t, err)
	// 线上形态必须是转义后的（服务端 Path 是解码形态，这里断言 EscapedPath）。
	assert.Equal(t, "/ent/v2/tasks/v_a%2Fb%20c/creations", fake.last(t).EscapedPath)
}

// 测试目的：成功终态的产物 URL 带查询串时 MIME 推断仍正确（`?sign=...` 不能干扰扩展名）。
func TestQuery_ArtifactMimeFromSignedURL(t *testing.T) {
	fake := startFakeUpstream(t, respondWith(200, queryBody("success", "",
		`[{"url":"https://cdn.example.com/out.mp4?sign=abc&t=1"}]`, "")))
	adapter := newTestAdapter(t, fake)

	status, err := adapter.Query(t.Context(), upstreamTaskID)
	require.NoError(t, err)
	require.Len(t, status.Artifacts, 1)
	assert.Equal(t, "video/mp4", status.Artifacts[0].MimeType)
	assert.Equal(t, "https://cdn.example.com/out.mp4?sign=abc&t=1", status.Artifacts[0].URL)
}

// 测试目的：409/422 之类「既非 2xx 也非 404」的常见上游状态一律 error（保留状态码数字）。
func TestQuery_NonStandardErrorStatus(t *testing.T) {
	for _, code := range []int{http.StatusConflict, http.StatusUnprocessableEntity} {
		fake := startFakeUpstream(t, respondWith(code, `{"message":"nope"}`))
		adapter := newTestAdapter(t, fake)

		status, err := adapter.Query(t.Context(), upstreamTaskID)
		require.Error(t, err)
		assert.Nil(t, status)
		assert.Contains(t, err.Error(), strconv.Itoa(code))
	}
}
