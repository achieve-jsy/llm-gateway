// Package kling_test 测试 internal/providers/kling 的 Kling 异步视频任务适配器。
//
// 测试原则（tests/README.md）：
//   - 外部测试包，只通过公开 API（kling.NewMediaAdapter）驱动；
//   - **测试内禁止联网**：BaseURL 一律指向 httptest 假上游，凭据是本地固定值；
//   - 报文用 golden 形状断言，状态/参数用表驱动。
package kling_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/kling"
)

const (
	// testAccessKey / testSecretKey 是本地固定凭据（控制面 credential_schema 的两列）。
	testAccessKey = "kling-test-access-key"
	testSecretKey = "kling-test-secret-key"
	// upstreamTaskID 是假上游返回的上游任务 id（Kling 真实形态是数字串）。
	upstreamTaskID = "890072193272676418"
	// testCredentials 是 providers.MediaAdapterParams.Credentials 的原始 JSON 形状。
	testCredentials = `{"access_key":"` + testAccessKey + `","secret_key":"` + testSecretKey + `"}`
	// testUserAgent 是上游要求的 UA（契约逐字值）。
	testUserAgent = "kling-sdk/1.0"
)

// recordedRequest 是一次被假上游记录下来的请求（供断言路径 / 请求头 / 请求体）。
type recordedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// fakeUpstream 是假上游：记录全部入站请求，并按 respond 脚本回响应。
//
// respond 为 nil 时回「提交成功」信封（task_status=submitted）。
type fakeUpstream struct {
	*httptest.Server

	mu       sync.Mutex
	requests []recordedRequest
}

// startFakeUpstream 起一个假上游；t.Cleanup 自动关闭（不泄漏端口）。
func startFakeUpstream(t *testing.T, respond func(w http.ResponseWriter, r *http.Request, body []byte)) *fakeUpstream {
	t.Helper()

	fake := &fakeUpstream{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 读 body 失败只可能是测试自身发错请求；这里不 fail（handler 在服务端 goroutine，
		// 不能调用 require 的 FailNow），记录为空 body 由断言暴露。
		body, _ := io.ReadAll(r.Body)

		fake.mu.Lock()
		fake.requests = append(fake.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Header: r.Header.Clone(),
			Body:   body,
		})
		fake.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if respond == nil {
			_, _ = w.Write([]byte(submitBody(upstreamTaskID, "submitted")))
			return
		}
		respond(w, r, body)
	}))
	t.Cleanup(fake.Close)

	return fake
}

// recorded 返回已记录请求的快照。
func (f *fakeUpstream) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// count 返回已记录请求数（用于断言「校验失败时根本没打上游」）。
func (f *fakeUpstream) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.requests)
}

// last 返回最后一个请求（并断言确有请求）。
func (f *fakeUpstream) last(t *testing.T) recordedRequest {
	t.Helper()

	requests := f.recorded()
	require.NotEmpty(t, requests, "假上游没有收到任何请求")
	return requests[len(requests)-1]
}

// respondWith 构造一个固定状态码 + 固定报文的响应脚本。
func respondWith(statusCode int, body string) func(w http.ResponseWriter, r *http.Request, _ []byte) {
	return func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}
}

// submitBody 组装提交响应（与上游同形的信封）。
func submitBody(taskID, taskStatus string) string {
	return fmt.Sprintf(
		`{"code":0,"message":"SUCCEED","request_id":"req-test","data":{"task_id":%q,"task_status":%q,"created_at":1730000000,"updated_at":1730000000}}`,
		taskID, taskStatus)
}

// queryBody 组装查询响应。
//
// videosJSON 传 "" 表示空数组；finalUnitDeduction 传 "" 表示响应里没有该字段。
func queryBody(taskStatus, statusMsg, videosJSON, finalUnitDeduction string) string {
	if videosJSON == "" {
		videosJSON = "[]"
	}
	deduction := ""
	if finalUnitDeduction != "" {
		deduction = `,"final_unit_deduction":` + finalUnitDeduction
	}
	return fmt.Sprintf(
		`{"code":0,"message":"SUCCEED","data":{"task_id":%q,"task_status":%q,"task_status_msg":%q,"task_result":{"videos":%s}%s,"created_at":1730000000,"updated_at":1730000100}}`,
		upstreamTaskID, taskStatus, statusMsg, videosJSON, deduction)
}

// newTestAdapter 构造一个指向假上游的适配器（正常凭据）。
func newTestAdapter(t *testing.T, fake *fakeUpstream) core.MediaTaskProvider {
	t.Helper()

	return newAdapterWith(t, providers.MediaAdapterParams{
		Type:        kling.MediaProviderType,
		Name:        "kling-test",
		BaseURL:     fake.URL,
		Credentials: testCredentials,
		Models:      []string{"kling-v1", "kling-v1-6", "kling-v2-master"},
	})
}

// newAdapterWith 按给定入参构造适配器并断言构造成功。
func newAdapterWith(t *testing.T, params providers.MediaAdapterParams) core.MediaTaskProvider {
	t.Helper()

	adapter, err := kling.NewMediaAdapter(params)
	require.NoError(t, err)
	require.NotNil(t, adapter, "构造器不得返回 (nil, nil)")
	return adapter
}

// submitRequest 构造一个「7 键齐全」的归一提交请求；mutate 用于按用例改写参数。
func submitRequest(action core.MediaAction, images []string, mutate func(params map[string]any)) *core.MediaSubmitRequest {
	params := map[string]any{
		"prompt":       "一只猫在弹钢琴",
		"images":       images,
		"duration":     5,
		"resolution":   "",
		"mode":         "pro",
		"aspect_ratio": "",
		"size":         "",
	}
	if mutate != nil {
		mutate(params)
	}
	return &core.MediaSubmitRequest{
		Kind:          core.MediaKindVideo,
		Action:        action,
		UpstreamModel: "kling-v1-6",
		Params:        params,
	}
}

// decodeBody 把上游收到的请求体解成 map（用于断言字段存在性）。
func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out), "上游请求体必须是合法 JSON: %s", string(body))
	return out
}

// assertBadRequest 断言 err 是 400 类错误（core.NewInvalidRequestError）。
func assertBadRequest(t *testing.T, err error) {
	t.Helper()

	require.Error(t, err)
	var gatewayErr *core.GatewayError
	require.ErrorAs(t, err, &gatewayErr)
	require.Equal(t, http.StatusBadRequest, gatewayErr.StatusCode,
		"必须是 400 类错误，实际: %v", err)
}

// queryVendorTaskID 走一次 Submit 拿到真实编码过的 vendorTaskID。
func queryVendorTaskID(t *testing.T, adapter core.MediaTaskProvider) string {
	t.Helper()

	result, err := adapter.Submit(
		t.Context(),
		submitRequest(core.MediaActionTextToVideo, nil, nil),
	)
	require.NoError(t, err)
	require.NotEmpty(t, result.VendorTaskID)
	return result.VendorTaskID
}
