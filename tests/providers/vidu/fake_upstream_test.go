// Package vidu_test 测试 internal/providers/vidu 的 Vidu 异步视频任务适配器。
//
// 测试原则（tests/README.md）：
//   - 外部测试包，只通过公开 API（vidu.NewMediaAdapter）驱动；
//   - **测试内禁止联网**：BaseURL 一律指向 httptest 假上游，凭据是本地固定值；
//   - 报文用 golden 形状断言，状态/参数用表驱动。
package vidu_test

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
	"github.com/example/llm-gateway/internal/providers/vidu"
)

const (
	// testAPIKey 是本地固定凭据（控制面单列 schema 的取值）。
	testAPIKey = "vda-test-api-key-0123456789"
	// upstreamTaskID 是假上游返回的上游任务 id（Vidu 真实形态是 `v_` 前缀的串）。
	upstreamTaskID = "v_test_task_0001"
	// testCredentialsJSON 是控制面 credential_schema 的原始 JSON 形态。
	testCredentialsJSON = `{"api_key":"` + testAPIKey + `"}`
	// testCredentialsBare 是**生产实际形态**：控制面对非 aksk 渠道调用 extractAPIKey，
	// 会把 `{"api_key":"..."}` 抽成纯字符串后放进 MediaAdapterParams.Credentials。
	testCredentialsBare = testAPIKey
)

// recordedRequest 是一次被假上游记录下来的请求（供断言路径 / 请求头 / 请求体）。
type recordedRequest struct {
	Method string
	Path   string
	// EscapedPath 是**线上形态**的路径（含 %2F/%20 等转义）；Path 是服务端解码后的形态。
	EscapedPath string
	Header      http.Header
	Body        []byte
}

// fakeUpstream 是假上游：记录全部入站请求，并按 respond 脚本回响应。
//
// respond 为 nil 时回「提交成功」报文（state=created）。
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
			Method:      r.Method,
			Path:        r.URL.Path,
			EscapedPath: r.URL.EscapedPath(),
			Header:      r.Header.Clone(),
			Body:        body,
		})
		fake.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if respond == nil {
			_, _ = w.Write([]byte(submitBody(upstreamTaskID, "created")))
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

// submitBody 组装提交响应（与上游同形的顶层结构）。
func submitBody(taskID, state string) string {
	return fmt.Sprintf(`{"task_id":%q,"state":%q,"model":"viduq2","created_at":"1730000000"}`, taskID, state)
}

// queryBody 组装查询响应。
//
// creationsJSON 传 "" 表示不带 creations 字段；progressJSON 传 "" 表示不带 progress 字段。
func queryBody(state, errCode, creationsJSON, progressJSON string) string {
	creations := ""
	if creationsJSON != "" {
		creations = `,"creations":` + creationsJSON
	}
	progress := ""
	if progressJSON != "" {
		progress = `,"progress":` + progressJSON
	}
	return fmt.Sprintf(
		`{"task_id":%q,"state":%q,"err_code":%q,"model":"viduq2"%s%s}`,
		upstreamTaskID, state, errCode, creations, progress)
}

// creationsJSON 按 (url, coverURL) 列表组装 creations 数组。
func creationsJSON(items ...[2]string) string {
	out := "["
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf(`{"id":"c%d","url":%q,"cover_url":%q}`, i, item[0], item[1])
	}
	return out + "]"
}

// newTestAdapter 构造一个指向假上游的适配器（裸 API Key 形态，即生产形态）。
func newTestAdapter(t *testing.T, fake *fakeUpstream) core.MediaTaskProvider {
	t.Helper()

	return newAdapterWith(t, providers.MediaAdapterParams{
		Type:        vidu.MediaProviderType,
		Name:        "vidu-test",
		BaseURL:     fake.URL,
		Credentials: testCredentialsBare,
		Models:      []string{"viduq2", "viduq1", "vidu2.0", "vidu1.5"},
	})
}

// newAdapterWith 按给定入参构造适配器并断言构造成功。
func newAdapterWith(t *testing.T, params providers.MediaAdapterParams) core.MediaTaskProvider {
	t.Helper()

	adapter, err := vidu.NewMediaAdapter(params)
	require.NoError(t, err)
	require.NotNil(t, adapter, "构造器不得返回 (nil, nil)")
	return adapter
}

// submitRequest 构造一个「7 键齐全」的归一提交请求；mutate 用于按用例改写参数。
//
// 与 gateway handler 的 mediaSubmitParams 一致：**7 个键恒定存在**（缺省为零值）。
func submitRequest(action core.MediaAction, images []string, mutate func(params map[string]any)) *core.MediaSubmitRequest {
	params := map[string]any{
		"prompt":       "一只猫在弹钢琴",
		"images":       images,
		"duration":     0,
		"resolution":   "",
		"mode":         "",
		"aspect_ratio": "",
		"size":         "",
	}
	if mutate != nil {
		mutate(params)
	}
	return &core.MediaSubmitRequest{
		Kind:          core.MediaKindVideo,
		Action:        action,
		UpstreamModel: "viduq2",
		Params:        params,
	}
}

// textToVideoRequest 是最常用的一种请求：文生（0 张图）。
func textToVideoRequest(mutate func(params map[string]any)) *core.MediaSubmitRequest {
	return submitRequest(core.MediaActionTextToVideo, nil, mutate)
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
