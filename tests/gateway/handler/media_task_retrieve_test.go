package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// 查询链路（C5/C6/C7/C8）：归属 → 终态缓存 → 上游查询 → CAS → 记账
// ─────────────────────────────────────────────────────────────────────────────

func TestMediaTaskRetrieve_OwnershipMismatchIs404(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:       "task_other_owner",
		Status:       "in_progress",
		VendorTaskID: "vendor-other",
		APIKeyID:     "key-someone-else",
	})
	rec := rig.retrieve(t, "task_other_owner")
	// 404（不是 403）：403 会泄露「该 task_id 存在」，可被用来枚举他人任务。
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Equal(t, "not_found_error", errorCode(t, rec))
	require.Zero(t, rig.adapter.queryCalls, "归属校验失败时不得查询上游")
}

func TestMediaTaskRetrieve_UnknownTaskIs404(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rec := rig.retrieve(t, "task_does_not_exist")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Equal(t, "not_found_error", errorCode(t, rec))
}

func TestMediaTaskRetrieve_StoreUnavailableIs5xx(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.store.getErr = &infra.ControlPlaneError{Op: "get_media_task", Status: 500, Body: "cp down"}
	rec := rig.retrieve(t, "task_x")
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	require.Equal(t, "upstream_error", errorCode(t, rec))
}

func TestMediaTaskRetrieve_TerminalCacheSkipsUpstreamAndBilling(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:       "task_done",
		Status:       "succeed", // 存储层历史词表也要归一为 completed
		VendorTaskID: "vendor-done",
		VideoURL:     "https://cdn.example.com/out.mp4",
		CostMicroUSD: 800000,
	})
	rec := rig.retrieve(t, "task_done")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	resp := decodeJSON(t, rec)
	require.Equal(t, "completed", resp["status"])
	require.Equal(t, "task_done", resp["id"])
	require.Zero(t, rig.adapter.queryCalls, "终态必须走缓存，不再打上游")
	require.Zero(t, rig.store.updateCount(), "终态不得再次写库（避免重复记账）")
	_, hasUsage := rig.capture.mediaUsage()
	require.False(t, hasUsage, "终态缓存命中不得再次记账")
}

func TestMediaTaskRetrieve_CompletedBillsExactlyOnce(t *testing.T) {
	adapter := &fakeMediaAdapter{}
	rig := newMediaTestRig(t, adapter, nil)
	rig.seedMediaTask(newQueuedTask("task_bill"))
	adapter.queryStatus = &core.MediaTaskStatus{
		Status:   core.MediaStatusCompleted,
		Progress: "100",
		Artifacts: []core.MediaArtifact{
			{Kind: core.MediaKindVideo, URL: "https://cdn.example.com/a.mp4"},
		},
		Usage: map[string]any{"duration": float64(8), "final_unit_deduction": "0.5"},
	}

	rec := rig.retrieve(t, "task_bill")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodeJSON(t, rec)
	require.Equal(t, "completed", resp["status"])
	require.Equal(t, "100", resp["progress"])

	// 记账只发生一次，且成本按「实际时长 × 每秒单价」（8s × 0.1 = 0.8 USD = 800000 微美元）。
	require.Equal(t, 2, rig.store.updateCount(), "先 CAS 到 in_progress（兼容单步语义），再写终态")
	final := rig.store.lastUpdate(t).req
	require.NotNil(t, final.CostMicroUSD)
	require.Equal(t, int64(800000), *final.CostMicroUSD)
	require.NotEmpty(t, final.BilledAt)
	require.Equal(t, "https://cdn.example.com/a.mp4", final.VideoURL)
	require.Contains(t, final.Usage, `"output_seconds":8`, "usage jsonb 用 snake_case 键")

	var usagePayload map[string]any
	require.NoError(t, json.Unmarshal([]byte(final.Usage), &usagePayload))
	costInput, _ := usagePayload["cost_input"].(map[string]any)
	require.EqualValues(t, 8, costInput["output_seconds"])

	// 埋点上下文：**精确类型** core.MediaCostInput。
	mediaUsage, ok := rig.capture.mediaUsage()
	require.True(t, ok, "终态成功必须写 gwobs.CtxKeyMediaUsage（值类型 core.MediaCostInput）")
	require.EqualValues(t, 8, mediaUsage.OutputSeconds)
	require.Zero(t, mediaUsage.Images)

	// 第二次 retrieve：任务已是终态 → 走缓存、不再打上游、不再记账。
	before := rig.store.updateCount()
	rec2 := rig.retrieve(t, "task_bill")
	require.Equal(t, http.StatusOK, rec2.Code)
	require.Equal(t, 1, adapter.queryCalls, "第二次查询不得再打上游")
	require.Equal(t, before, rig.store.updateCount(), "重复 retrieve 不得重复写库/记账")
}

func TestMediaTaskRetrieve_CASLostDoesNotBill(t *testing.T) {
	adapter := &fakeMediaAdapter{}
	rig := newMediaTestRig(t, adapter, nil)
	rig.seedMediaTask(newQueuedTask("task_cas"))
	adapter.queryStatus = &core.MediaTaskStatus{
		Status:    core.MediaStatusCompleted,
		Artifacts: []core.MediaArtifact{{Kind: core.MediaKindVideo, URL: "https://cdn.example.com/a.mp4"}},
		Usage:     map[string]any{"duration": float64(8)},
	}
	// 模拟并发写入者已经推进了状态：本次 CAS 全部未命中。
	rig.store.forceCASMiss = true
	before := rig.store.updateCount()

	rec := rig.retrieve(t, "task_cas")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Greater(t, rig.store.updateCount(), before, "仍应尝试 CAS")
	_, hasUsage := rig.capture.mediaUsage()
	require.False(t, hasUsage, "CAS 未命中说明不是本次推进到终态 → 绝不能记账")
}

func TestMediaTaskRetrieve_FailedTaskIsNotBilled(t *testing.T) {
	adapter := &fakeMediaAdapter{}
	rig := newMediaTestRig(t, adapter, nil)
	rig.seedMediaTask(newQueuedTask("task_fail"))
	adapter.queryStatus = &core.MediaTaskStatus{
		Status:     core.MediaStatusFailed,
		FailReason: "内容审核未通过",
	}

	rec := rig.retrieve(t, "task_fail")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodeJSON(t, rec)
	require.Equal(t, "failed", resp["status"])
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok, "失败任务必须带 error 字段")
	require.Equal(t, "内容审核未通过", errObj["message"])

	final := rig.store.lastUpdate(t).req
	require.Nil(t, final.CostMicroUSD, "失败任务不得写成本")
	require.Empty(t, final.BilledAt)
	_, hasUsage := rig.capture.mediaUsage()
	require.False(t, hasUsage, "失败任务不得记账")
}

func TestMediaTaskRetrieve_StorageExpiredMapsToFailed(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:       "task_expired",
		Status:       "expired", // 控制面 sweeper 的存储层词表
		VendorTaskID: "vendor-expired",
		ErrorMessage: "任务超时",
	})
	rec := rig.retrieve(t, "task_expired")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodeJSON(t, rec)
	require.Equal(t, "failed", resp["status"], "expired 对外必须归一为 failed")
	errObj, _ := resp["error"].(map[string]any)
	require.Equal(t, "任务超时", errObj["message"])
	require.Zero(t, rig.adapter.queryCalls)
}

func TestMediaTaskRetrieve_StatusVocabularyMapping(t *testing.T) {
	// 存量 Seedance 词表（submitted/processing/succeed）与控制面新增词表都要归一到
	// OpenAI 四态；判定的可观测结果有两个：终态直接走缓存、非终态必须打上游。
	cases := []struct {
		stored   string
		expected string // 终态时对外应答的状态
		terminal bool
	}{
		{"submitted", "", false},
		{"processing", "", false},
		{"running", "", false},
		{"unknown_status_word", "", false}, // 未知存储值保守当非终态（宁可多轮询也不误判终态）
		{"succeed", "completed", true},
		{"completed", "completed", true},
		{"failed", "failed", true},
		{"expired", "failed", true},
	}
	for _, tc := range cases {
		t.Run(tc.stored, func(t *testing.T) {
			adapter := &fakeMediaAdapter{queryStatus: &core.MediaTaskStatus{
				Status: core.MediaStatusQueued, Progress: "1",
			}}
			rig := newMediaTestRig(t, adapter, nil)
			rig.seedMediaTask(&infra.MediaTask{
				TaskID:       "task_vocab",
				Status:       tc.stored,
				VendorTaskID: "vendor-vocab",
			})
			rec := rig.retrieve(t, "task_vocab")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			resp := decodeJSON(t, rec)
			if tc.terminal {
				require.Zero(t, adapter.queryCalls, "终态必须走缓存")
				require.Equal(t, tc.expected, resp["status"])
				return
			}
			require.Equal(t, 1, adapter.queryCalls, "非终态必须查询上游")
			require.Equal(t, "queued", resp["status"], "对外状态取自上游归一结果")
		})
	}
}

func TestMediaTaskRetrieve_SetsCallLogContext(t *testing.T) {
	adapter := &fakeMediaAdapter{queryStatus: &core.MediaTaskStatus{Status: core.MediaStatusInProgress}}
	rig := newMediaTestRig(t, adapter, nil)
	rig.seedMediaTask(newQueuedTask("task_ctx"))

	rec := rig.retrieve(t, "task_ctx")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, testMediaGroup, rig.capture.str("model"), "埋点 model 必须是模型组名")
	require.Equal(t, "Kling", rig.capture.str("provider"))
	require.Equal(t, testMediaUpstream, rig.capture.str("routed_model"), "采集器按 routed_model 查价表")
	require.NotContains(t, rig.capture.str("model"), ":GetTask", "不得引入伪模型后缀（call_type 由路径推导）")
}

func TestMediaTaskRetrieve_AdapterNotWiredReturns501(t *testing.T) {
	rig := newMediaTestRig(t, nil, func(cfg *handler.MediaTaskHandlerConfig) {
		cfg.Registry = providers.NewMediaAdapterRegistry()
	})
	rig.seedMediaTask(newQueuedTask("task_unwired"))
	rec := rig.retrieve(t, "task_unwired")
	require.Equal(t, http.StatusNotImplemented, rec.Code, rec.Body.String())
	require.Equal(t, "not_implemented", errorCode(t, rec))
}

func TestMediaTaskRetrieve_EmptyVendorTaskIDReturnsCurrentState(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{TaskID: "task_reserved", Status: "queued"})
	rec := rig.retrieve(t, "task_reserved")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "queued", decodeJSON(t, rec)["status"])
	require.Zero(t, rig.adapter.queryCalls, "没有上游 id 时不得查询上游")
}

// ─────────────────────────────────────────────────────────────────────────────
// 轮询失败分类（C7）
// ─────────────────────────────────────────────────────────────────────────────

func TestMediaTaskRetrieve_PollFailureClassification(t *testing.T) {
	cases := []struct {
		name         string
		queryErr     error
		pollFailure  int
		wantStatus   int
		wantState    string // 期望落库的状态；空表示状态不变
		wantFailures int
	}{
		{
			name:       "not_found 立即放弃（业务失败，不记账）",
			queryErr:   core.ErrMediaTaskNotFound,
			wantStatus: http.StatusOK,
			wantState:  "failed",
		},
		{
			name:         "auth 失败：计数 + 5xx",
			queryErr:     &core.GatewayError{StatusCode: http.StatusUnauthorized, Message: "invalid access key"},
			wantStatus:   http.StatusBadGateway,
			wantFailures: 1,
		},
		{
			name:         "transient（超时）：计数 + 5xx",
			queryErr:     context.DeadlineExceeded,
			wantStatus:   http.StatusBadGateway,
			wantFailures: 1,
		},
		{
			name:         "unrecognized（未知状态词）：计数 + 5xx",
			queryErr:     errors.New("adapter returned invalid status word"),
			wantStatus:   http.StatusBadGateway,
			wantFailures: 1,
		},
		{
			name:         "达到失败上限：放弃并置 failed",
			queryErr:     errors.New("connection reset by peer"),
			pollFailure:  2, // MaxPollFailures=3，本次是第 3 次
			wantStatus:   http.StatusOK,
			wantState:    "failed",
			wantFailures: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &fakeMediaAdapter{queryErr: tc.queryErr}
			rig := newMediaTestRig(t, adapter, nil)
			task := newQueuedTask("task_poll")
			task.PollFailures = tc.pollFailure
			rig.seedMediaTask(task)

			rec := rig.retrieve(t, "task_poll")
			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())

			stored := rig.store.task("task_poll")
			if tc.wantState != "" {
				require.Equal(t, tc.wantState, stored.Status)
			} else {
				require.Equal(t, "queued", stored.Status, "未达上限不得改状态")
				require.Equal(t, tc.wantFailures, stored.PollFailures)
			}
			_, hasUsage := rig.capture.mediaUsage()
			require.False(t, hasUsage, "轮询失败不得记账")
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 任务超时（C7，仅前台判定；后台 sweeper 在控制面）
// ─────────────────────────────────────────────────────────────────────────────

func TestMediaTaskRetrieve_TimeoutMarksExpiredWithoutBilling(t *testing.T) {
	adapter := &fakeMediaAdapter{}
	rig := newMediaTestRig(t, adapter, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:       "task_slow",
		Status:       "in_progress",
		VendorTaskID: "vendor-slow",
		CreatedAt:    time.Now().Add(-31 * time.Minute),
	})

	rec := rig.retrieve(t, "task_slow")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodeJSON(t, rec)
	require.Equal(t, "failed", resp["status"], "超时对外归一为 failed")
	errObj, _ := resp["error"].(map[string]any)
	require.Contains(t, errObj["message"], "超时")
	require.Equal(t, "expired", rig.store.task("task_slow").Status, "存储层写 expired（sweeper 口径）")
	require.Zero(t, adapter.queryCalls, "已超时就不再打上游")
	_, hasUsage := rig.capture.mediaUsage()
	require.False(t, hasUsage, "超时任务不记账")
}

func TestMediaTaskRetrieve_NotTimedOutStillQueriesUpstream(t *testing.T) {
	adapter := &fakeMediaAdapter{queryStatus: &core.MediaTaskStatus{Status: core.MediaStatusInProgress}}
	rig := newMediaTestRig(t, adapter, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:       "task_fresh",
		Status:       "in_progress",
		VendorTaskID: "vendor-fresh",
		CreatedAt:    time.Now().Add(-29 * time.Minute),
	})
	rec := rig.retrieve(t, "task_fresh")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, adapter.queryCalls)
	require.Equal(t, "in_progress", rig.store.task("task_fresh").Status)
}

// ─────────────────────────────────────────────────────────────────────────────
// 产物（C5 Phase 1：302 直跳）
// ─────────────────────────────────────────────────────────────────────────────

func TestMediaTaskContent_RedirectsToUpstream(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:   "task_artifact",
		Status:   "completed",
		VideoURL: "https://cdn.example.com/out.mp4?sig=abc",
	})
	rec := rig.content(t, "task_artifact")
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	require.Equal(t, "https://cdn.example.com/out.mp4?sig=abc", rec.Header().Get("Location"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"), "签名链接禁止中间层缓存")
}

func TestMediaTaskContent_FallsBackToResponseBodyArtifacts(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:       "task_artifact2",
		Status:       "completed",
		ResponseBody: `{"status":"completed","artifacts":[{"kind":"video","url":"https://cdn.example.com/b.mp4"}]}`,
	})
	rec := rig.content(t, "task_artifact2")
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	require.Equal(t, "https://cdn.example.com/b.mp4", rec.Header().Get("Location"))
}

func TestMediaTaskContent_NoArtifactIs404(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{TaskID: "task_noart", Status: "in_progress"})
	rec := rig.content(t, "task_noart")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Equal(t, "not_found_error", errorCode(t, rec))
}

func TestMediaTaskContent_RejectsNonHTTPArtifactURL(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:   "task_badscheme",
		Status:   "completed",
		VideoURL: "javascript:alert(1)",
	})
	rec := rig.content(t, "task_badscheme")
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	require.Equal(t, "upstream_error", errorCode(t, rec))
}

func TestMediaTaskContent_CrossKeyIs404(t *testing.T) {
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:   "task_other",
		Status:   "completed",
		VideoURL: "https://cdn.example.com/secret.mp4",
		APIKeyID: "another-key",
	})
	rec := rig.content(t, "task_other")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Empty(t, rec.Header().Get("Location"))
}

func TestMediaTaskContent_UserFallbackOwnership(t *testing.T) {
	// 历史数据只有 user_id（没有 api_key_id）时退化为「同一用户」判定。
	rig := newMediaTestRig(t, nil, nil)
	rig.seedMediaTask(&infra.MediaTask{
		TaskID:   "task_useronly",
		Status:   "completed",
		VideoURL: "https://cdn.example.com/u.mp4",
		UserID:   testMediaUserID,
	})
	rec := rig.content(t, "task_useronly")
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())

	// 同一用户但另一个 key 也不能越权读，因为 api_key_id 优先。
	rig.store.seed(&infra.MediaTask{
		TaskID:   "task_mixed",
		Status:   "completed",
		VideoURL: "https://cdn.example.com/m.mp4",
		APIKeyID: "other-key",
		UserID:   testMediaUserID,
	})
	rec2 := rig.content(t, "task_mixed")
	require.Equal(t, http.StatusNotFound, rec2.Code, rec2.Body.String())
}

func TestMediaTaskActionDerivationIsCoveredEverywhere(t *testing.T) {
	// 汇总性 sanity check：四种 action 都被 handler 产出过（避免冻结语义退化成单一分支）。
	seen := map[core.MediaAction]bool{}
	for _, body := range []string{
		`{"model":"kling-group","prompt":"p"}`,
		`{"model":"kling-group","image":"https://x/1.png"}`,
		`{"model":"kling-group","images":["https://x/1.png","https://x/2.png"]}`,
		`{"model":"kling-group","images":["https://x/1.png","https://x/2.png","https://x/3.png"]}`,
	} {
		rig := newMediaTestRig(t, nil, nil)
		rec := doJSON(t, rig.router, http.MethodPost, "/v1/videos", body, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		seen[rig.adapter.lastSubmit.Action] = true
	}
	require.Len(t, seen, 4)
}
