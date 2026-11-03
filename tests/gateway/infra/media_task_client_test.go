package infra_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/stretchr/testify/require"
)

// 本文件用 fake 控制面 HTTP server 固化**冻结的内部 API 契约**（C3b）。
//
// 契约来源：docs/media-channels-integration-design.md §6.2 + Batch 1 冻结通知。
// 重点固化三件事：
//  1. 路径必须是 `/internal/v1/video/tasks`（**不是** `/internal/v1/media/tasks` ——
//     tests/contract/contracts.json 里写的 media-tasks 与冻结契约不一致，见交付报告）；
//  2. 请求体字段名与类型（尤其 private_data / usage 作为 **JSON 字符串** 上报）；
//  3. 幂等语义（200 = 命中既有任务，201 = 新建）与 CAS 字段（expected_status）。

// capturedRequest 记录 fake server 收到的一次请求。
type capturedRequest struct {
	Method string
	Path   string
	Token  string
	Body   []byte
}

type fakeControlPlane struct {
	mu       sync.Mutex
	requests []capturedRequest

	status int
	body   string
}

func (f *fakeControlPlane) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, capturedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Token:  r.Header.Get("X-Internal-Token"),
			Body:   raw,
		})
		status, body := f.status, f.body
		f.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func (f *fakeControlPlane) last(t *testing.T) capturedRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.requests, "expected the control plane to receive a request")
	return f.requests[len(f.requests)-1]
}

func (f *fakeControlPlane) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func newFakeControlPlane(t *testing.T, status int, body string) (*fakeControlPlane, *infra.VideoTaskClient) {
	t.Helper()
	cp := &fakeControlPlane{status: status, body: body}
	server := httptest.NewServer(cp.handler())
	t.Cleanup(server.Close)
	return cp, infra.NewVideoTaskClient(server.URL, "internal-token-xyz")
}

// fullTaskJSON 是一份「控制面应当回传」的全量任务 JSON（字段与冻结契约一致）。
const fullTaskJSON = `{
	"task_id": "task_abc",
	"kind": "video",
	"action": "image_to_video",
	"status": "in_progress",
	"progress": "42",
	"vendor_task_id": "vendor_777",
	"model": "kling-group",
	"provider_name": "Kling",
	"provider_type": "kling",
	"api_key_id": "key-1",
	"api_key_name": "owner",
	"api_key_type": "default",
	"user_id": "user-1",
	"user_name": "owner",
	"dept_id": "dept-1",
	"studio_id": "studio-1",
	"video_url": "https://cdn.example.com/v.mp4",
	"request_body": "{\"prompt\":\"p\"}",
	"response_body": "{\"status\":\"in_progress\"}",
	"error_message": "",
	"usage": "{\"duration\":8}",
	"private_data": "{\"upstream_model\":\"kling-v1-6\"}",
	"cost_micro_usd": 800000,
	"billed_at": "2026-01-02T03:04:05Z",
	"poll_failures": 2,
	"created_at": "2026-01-02T03:00:00Z",
	"updated_at": "2026-01-02T03:04:05Z"
}`

func TestVideoTaskClient_CreateMediaTask_FrozenRequestShape(t *testing.T) {
	perCP, client := newFakeControlPlane(t, http.StatusCreated, `{"task":`+fullTaskJSON+`}`)

	req := infra.MediaCreateRequest{
		TaskID:         "task_abc",
		APIKeyID:       "key-1",
		APIKeyName:     "owner",
		APIKeyType:     "default",
		UserID:         "user-1",
		UserName:       "owner",
		Model:          "kling-group",
		ProviderName:   "Kling",
		ProviderType:   "kling",
		Status:         "queued",
		RequestBody:    `{"prompt":"p"}`,
		Kind:           "video",
		Action:         "image_to_video",
		VendorTaskID:   "vendor_777",
		Progress:       "0",
		IdempotencyKey: "idem-1",
		PrivateData:    `{"upstream_model":"kling-v1-6","provider_type":"kling"}`,
		DeptID:         "dept-1",
		StudioID:       "studio-1",
	}
	res, err := client.CreateMediaTask(context.Background(), req)
	require.NoError(t, err)

	// ① 路径与鉴权头：创建路径必须是既有的 /internal/v1/video/tasks
	// （滚动发布窗口内控制面只注册了这一个路径；新建 /internal/v1/media/tasks 会 404）。
	last := perCP.last(t)
	require.Equal(t, http.MethodPost, last.Method)
	require.Equal(t, "/internal/v1/video/tasks", last.Path)
	require.NotEqual(t, "/internal/v1/media/tasks", last.Path)
	require.Equal(t, "internal-token-xyz", last.Token)

	// ② 请求体：字段名逐个固化（含新增 delta 字段），private_data 是 JSON **字符串**。
	var sent map[string]any
	require.NoError(t, json.Unmarshal(last.Body, &sent))
	require.Equal(t, "task_abc", sent["task_id"])
	require.Equal(t, "video", sent["kind"])
	require.Equal(t, "image_to_video", sent["action"])
	require.Equal(t, "vendor_777", sent["vendor_task_id"])
	require.Equal(t, "0", sent["progress"])
	require.Equal(t, "idem-1", sent["idempotency_key"])
	require.Equal(t, "dept-1", sent["dept_id"])
	require.Equal(t, "studio-1", sent["studio_id"])
	require.Equal(t, "kling", sent["provider_type"])
	require.Equal(t, "queued", sent["status"])
	privateData, ok := sent["private_data"].(string)
	require.True(t, ok, "private_data 必须以 JSON 字符串上报（jsonb 列），实际类型 %T", sent["private_data"])
	require.Contains(t, privateData, "kling-v1-6")

	// ③ 201 → Created=true 且任务字段被解析（含 jsonb 文本字段）。
	require.Equal(t, http.StatusCreated, res.StatusCode)
	require.True(t, res.Created)
	require.NotNil(t, res.Task)
	require.Equal(t, "task_abc", res.Task.TaskID)
	require.Equal(t, "in_progress", res.Task.Status)
	require.Equal(t, "vendor_777", res.Task.VendorTaskID)
	require.Equal(t, int64(800000), res.Task.CostMicroUSD)
	require.Equal(t, 2, res.Task.PollFailures)
	require.Equal(t, `{"duration":8}`, res.Task.Usage.String())
	require.Equal(t, `{"upstream_model":"kling-v1-6"}`, res.Task.PrivateData.String())
	require.NotNil(t, res.Task.BilledAt)
	require.True(t, res.Task.BilledAt.UTC().Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
	require.Equal(t, "studio-1", res.Task.StudioID)
}

func TestVideoTaskClient_CreateMediaTask_IdempotencyHitIs200(t *testing.T) {
	// 200 = (api_key_id, idempotency_key) 已存在 → 调用方必须直接返回既有任务、不得打上游。
	perCP, client := newFakeControlPlane(t, http.StatusOK, `{"task":`+fullTaskJSON+`}`)
	res, err := client.CreateMediaTask(context.Background(), infra.MediaCreateRequest{
		TaskID: "task_new", APIKeyID: "key-1", IdempotencyKey: "idem-dup",
	})
	require.NoError(t, err)
	require.False(t, res.Created, "200 表示幂等命中")
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NotNil(t, res.Task)
	require.Equal(t, "task_abc", res.Task.TaskID, "必须能读到既有任务（否则 handler 无法返回幂等结果）")
	require.Equal(t, 1, perCP.count(), "幂等命中只应发一次请求")
}

func TestVideoTaskClient_CreateMediaTask_BareTaskBody(t *testing.T) {
	// 容忍裸任务体（控制面直接返回 task 对象，不带 {task:...} 信封）。
	_, client := newFakeControlPlane(t, http.StatusCreated, fullTaskJSON)
	res, err := client.CreateMediaTask(context.Background(), infra.MediaCreateRequest{TaskID: "task_abc"})
	require.NoError(t, err)
	require.NotNil(t, res.Task)
	require.Equal(t, "task_abc", res.Task.TaskID)
}

func TestVideoTaskClient_CreateMediaTask_ErrorStatuses(t *testing.T) {
	cases := []struct {
		status     int
		body       string
		wantClient bool
	}{
		{http.StatusBadRequest, `{"error":"private_data must be object"}`, true},
		{http.StatusConflict, `{"error":"duplicate"}`, true},
		{http.StatusInternalServerError, `boom`, false},
		{http.StatusBadGateway, `bad gateway`, false},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			_, client := newFakeControlPlane(t, tc.status, tc.body)
			res, err := client.CreateMediaTask(context.Background(), infra.MediaCreateRequest{TaskID: "t"})
			require.Error(t, err)
			require.Nil(t, res)
			var cpErr *infra.ControlPlaneError
			require.True(t, errors.As(err, &cpErr), "错误必须是 *ControlPlaneError：%v", err)
			require.Equal(t, tc.status, cpErr.Status)
			require.Equal(t, tc.wantClient, cpErr.IsClientError())
			require.Contains(t, cpErr.Body, tc.body)
		})
	}
}

func TestVideoTaskClient_CreateMediaTask_TransportError(t *testing.T) {
	client := infra.NewVideoTaskClient("http://127.0.0.1:1", "t")
	_, err := client.CreateMediaTask(context.Background(), infra.MediaCreateRequest{TaskID: "t"})
	require.Error(t, err)
	var cpErr *infra.ControlPlaneError
	require.True(t, errors.As(err, &cpErr))
	require.Zero(t, cpErr.Status, "传输层失败 Status=0（与 4xx 契约漂移区分）")
	require.False(t, cpErr.IsClientError())
}

func TestVideoTaskClient_GetMediaTask(t *testing.T) {
	perCP, client := newFakeControlPlane(t, http.StatusOK, `{"task":`+fullTaskJSON+`}`)
	task, err := client.GetMediaTask(context.Background(), "task_abc")
	require.NoError(t, err)
	require.Equal(t, "task_abc", task.TaskID)

	last := perCP.last(t)
	require.Equal(t, http.MethodGet, last.Method)
	require.Equal(t, "/internal/v1/video/tasks/task_abc", last.Path)
	require.Equal(t, "internal-token-xyz", last.Token)
}

func TestVideoTaskClient_GetMediaTask_NotFound(t *testing.T) {
	_, client := newFakeControlPlane(t, http.StatusNotFound, `{"error":"not found"}`)
	_, err := client.GetMediaTask(context.Background(), "task_missing")
	require.ErrorIs(t, err, infra.ErrMediaTaskNotFound)
}

func TestVideoTaskClient_GetMediaTask_EmptyBodyIsProtocolFailure(t *testing.T) {
	_, client := newFakeControlPlane(t, http.StatusOK, ``)
	_, err := client.GetMediaTask(context.Background(), "task_x")
	require.Error(t, err)
	require.NotErrorIs(t, err, infra.ErrMediaTaskNotFound, "空响应体不是「任务不存在」")
}

func TestVideoTaskClient_GetMediaTask_TolerantFieldRepresentations(t *testing.T) {
	// private_data / usage 若对端用 datatypes.JSON（裸对象）而不是字符串，也必须能读出来。
	body := `{"task":{"task_id":"task_obj","kind":"video","status":"queued",
		"usage":{"duration":8},
		"private_data":{"upstream_model":"kling-v1-6","submit_params":{"duration":8}},
		"created_at":"2026-01-02T03:00:00Z"}}`
	_, client := newFakeControlPlane(t, http.StatusOK, body)
	task, err := client.GetMediaTask(context.Background(), "task_obj")
	require.NoError(t, err)
	require.JSONEq(t, `{"duration":8}`, task.Usage.String())
	require.JSONEq(t, `{"upstream_model":"kling-v1-6","submit_params":{"duration":8}}`, task.PrivateData.String())
}

func TestVideoTaskClient_UpdateMediaTask_FrozenRequestShape(t *testing.T) {
	perCP, client := newFakeControlPlane(t, http.StatusOK, `{"updated":true,"task":`+fullTaskJSON+`}`)

	cost := int64(800000)
	failures := 3
	res, err := client.UpdateMediaTask(context.Background(), "task_abc", infra.MediaUpdateRequest{
		Status:         "completed",
		Progress:       "100",
		VideoURL:       "https://cdn.example.com/v.mp4",
		ResponseBody:   `{"status":"completed"}`,
		Usage:          `{"output_seconds":8}`,
		CostMicroUSD:   &cost,
		BilledAt:       "2026-01-02T03:04:05Z",
		PollFailures:   &failures,
		VendorTaskID:   "vendor_777",
		ExpectedStatus: "in_progress",
	})
	require.NoError(t, err)
	require.True(t, res.Updated)
	require.NotNil(t, res.Task)

	last := perCP.last(t)
	require.Equal(t, http.MethodPatch, last.Method)
	require.Equal(t, "/internal/v1/video/tasks/task_abc", last.Path)
	require.Equal(t, "internal-token-xyz", last.Token)

	var sent map[string]any
	require.NoError(t, json.Unmarshal(last.Body, &sent))
	require.Equal(t, "completed", sent["status"])
	require.Equal(t, "in_progress", sent["expected_status"], "CAS 字段：仅在当前状态相等时更新")
	require.Equal(t, "100", sent["progress"])
	require.Equal(t, "vendor_777", sent["vendor_task_id"])
	require.EqualValues(t, 800000, sent["cost_micro_usd"], "cost_micro_usd 是整数微美元")
	require.Equal(t, "2026-01-02T03:04:05Z", sent["billed_at"], "billed_at 是 RFC3339")
	require.EqualValues(t, 3, sent["poll_failures"])
	usage, ok := sent["usage"].(string)
	require.True(t, ok, "usage 必须以 JSON 字符串上报，实际 %T", sent["usage"])
	require.Contains(t, usage, "output_seconds")
}

func TestVideoTaskClient_UpdateMediaTask_CASMissMeansUpdatedFalse(t *testing.T) {
	// updated=false：状态已被并发写入者推进，调用方必须丢弃本次计算结果（含成本）。
	_, client := newFakeControlPlane(t, http.StatusOK, `{"updated":false,"task":{"task_id":"task_abc","status":"completed"}}`)
	res, err := client.UpdateMediaTask(context.Background(), "task_abc", infra.MediaUpdateRequest{Status: "failed"})
	require.NoError(t, err)
	require.False(t, res.Updated)
	require.NotNil(t, res.Task)
	require.Equal(t, "completed", res.Task.Status)
}

func TestVideoTaskClient_UpdateMediaTask_MissingUpdatedFieldIsConservative(t *testing.T) {
	// 响应里没有 updated 字段（老控制面）：保守按 false 处理，宁可少记一次也不重复记账。
	_, client := newFakeControlPlane(t, http.StatusOK, `{"task":{"task_id":"task_abc","status":"completed"}}`)
	res, err := client.UpdateMediaTask(context.Background(), "task_abc", infra.MediaUpdateRequest{Status: "completed"})
	require.NoError(t, err)
	require.False(t, res.Updated)
}

func TestVideoTaskClient_UpdateMediaTask_NotFound(t *testing.T) {
	_, client := newFakeControlPlane(t, http.StatusNotFound, `{"error":"nope"}`)
	_, err := client.UpdateMediaTask(context.Background(), "task_gone", infra.MediaUpdateRequest{Status: "failed"})
	require.ErrorIs(t, err, infra.ErrMediaTaskNotFound)
}

func TestVideoTaskClient_UpdateMediaTask_OmitsUnsetPointers(t *testing.T) {
	// 未设置 cost_micro_usd / poll_failures 时不得写 0（否则会把「未知」写成「免费」）。
	perCP, client := newFakeControlPlane(t, http.StatusOK, `{"updated":true,"task":{"task_id":"t","status":"in_progress"}}`)
	_, err := client.UpdateMediaTask(context.Background(), "t", infra.MediaUpdateRequest{Status: "in_progress"})
	require.NoError(t, err)

	var sent map[string]any
	require.NoError(t, json.Unmarshal(perCP.last(t).Body, &sent))
	require.NotContains(t, sent, "cost_micro_usd")
	require.NotContains(t, sent, "poll_failures")
	require.NotContains(t, sent, "billed_at")
	require.NotContains(t, sent, "usage")
}
