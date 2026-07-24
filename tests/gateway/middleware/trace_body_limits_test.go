package middleware_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 冻结契约（docs/trace-body-memory-design.md D1）的默认值，测试内显式复刻一份：
// 小正文路径的 trace_req_body 必须与按这些上限 sanitize 后的结果逐字节一致。
const (
	traceMaxParseBytes = 1 << 20 // MaxParseBytesInTrace 默认 1 MB
	traceMaxStringLen  = 500_000 // MaxStringLenInTrace 默认 500_000 字符
	traceMaxBase64Len  = 64      // MaxBase64LenInTrace 默认 64 字符
)

// traceSmallBodyGolden 普通小请求体走 sanitize 路径后的确定性输出：
// json.Unmarshal→map→json.Marshal 会丢格式并按字典序重排键，这是改动前就有的行为。
const traceSmallBodyGolden = `{"messages":[{"content":"hi","role":"user"}],"model":"gpt-4o"}`

// traceCaptureProbe 收集一次 TraceCapture 执行后 context 中的正文键，
// 以及下游 handler 实际读到的请求体（验证中间件读完 body 后已恢复）。
type traceCaptureProbe struct {
	sanitized    []byte // trace_req_body
	sanitizedSet bool
	raw          []byte // trace_req_body_raw
	rawSet       bool
	handlerBody  []byte
}

// newTraceCaptureEngine 构造只挂 TraceCapture 的测试路由。
// readHandlerBody 为 true 时 handler 会读走并记录请求体；做分配量断言时不读，
// 避免 handler 侧 io.ReadAll 的分配干扰对中间件本身内存行为的度量。
func newTraceCaptureEngine(probe *traceCaptureProbe, readHandlerBody bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.TraceCapture())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		if v, ok := c.Get("trace_req_body"); ok {
			probe.sanitized, _ = v.([]byte)
			probe.sanitizedSet = true
		}
		if v, ok := c.Get("trace_req_body_raw"); ok {
			probe.raw, _ = v.([]byte)
			probe.rawSet = true
		}
		if readHandlerBody {
			probe.handlerBody, _ = io.ReadAll(c.Request.Body)
		}
		c.Status(http.StatusOK)
	})
	return r
}

// serveTraceCapture 用给定 body 与 Content-Length 跑一次请求并回传采集结果。
// contentLength < 0 时不覆盖 ContentLength（由 httptest 依据 reader 推断）。
func serveTraceCapture(t *testing.T, body []byte, contentLength int64) traceCaptureProbe {
	t.Helper()
	var probe traceCaptureProbe
	r := newTraceCaptureEngine(&probe, true)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if contentLength >= 0 {
		req.ContentLength = contentLength
		req.Header.Set("Content-Length", fmt.Sprintf("%d", contentLength))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	return probe
}

// jsonBodyOfSize 构造恰好 n 字节的合法 JSON 请求体（{"input":"<pad>"}）。
func jsonBodyOfSize(t *testing.T, n int) []byte {
	t.Helper()
	const prefix, suffix = `{"input":"`, `"}`
	require.GreaterOrEqual(t, n, len(prefix)+len(suffix), "n 太小，无法构造合法 JSON")
	return []byte(prefix + strings.Repeat("x", n-len(prefix)-len(suffix)) + suffix)
}

// TestTraceCapture_SmallBody_SanitizePathMatchesFrozenPolicy 验收标准 1（D1/D2）：
// 小请求体（<= 1 MB）仍走 sanitize 路径，trace_req_body 必须等于按冻结策略
// （MaxStringLenInTrace=500_000、MaxBase64LenInTrace=64）sanitize 后的结果，
// 即与改动前逐字节一致。
func TestTraceCapture_SmallBody_SanitizePathMatchesFrozenPolicy(t *testing.T) {
	// 含超长字符串（600_000 字符 > 500_000 上限）与超长 base64 data-URI 的小请求体
	longText := strings.Repeat("0123456789", 60_000)
	dataURI := "data:image/png;base64," + strings.Repeat("A", 200)

	original, err := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "user", "content": longText},
			map[string]any{"role": "user", "content": dataURI},
		},
	})
	require.NoError(t, err)
	require.LessOrEqual(t, len(original), traceMaxParseBytes, "用例前提：请求体必须 <= 1 MB")

	// 期望值按冻结策略手工推导：
	// 超长字符串保留首 35% + 尾 65% + 截断标记；base64 payload 换成大小占位符。
	startN := traceMaxStringLen * 35 / 100
	endN := traceMaxStringLen - startN
	wantLongText := longText[:startN] +
		fmt.Sprintf("...(truncated, skipped %d chars)...", len(longText)-startN-endN) +
		longText[len(longText)-endN:]
	// payload 200 字符 → 解码 150 字节 → 小于 1 KB，占位符以 B 为单位
	wantDataURI := "data:image/png;base64,[base64_data truncated: 150 B]"

	expected, err := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "user", "content": wantLongText},
			map[string]any{"role": "user", "content": wantDataURI},
		},
	})
	require.NoError(t, err)

	probe := serveTraceCapture(t, original, int64(len(original)))
	require.True(t, probe.sanitizedSet, "小请求体必须写入 trace_req_body")
	assert.False(t, probe.rawSet, "小请求体不得写入 trace_req_body_raw")
	// 逐字节一致：实现的 sanitize 结果 == 按冻结策略推导的期望值
	assert.Equal(t, string(expected), string(probe.sanitized))
	// sanitize 的两个关键动作都真实发生（避免恒真断言）
	assert.Contains(t, string(probe.sanitized), "...(truncated, skipped 100000 chars)...")
	assert.Contains(t, string(probe.sanitized), "data:image/png;base64,[base64_data truncated: 150 B]")
	// 下游 handler 仍能读到完整原始 body
	assert.Equal(t, string(original), string(probe.handlerBody))
}

// TestTraceCapture_SmallBody_GoldenBytesUnchanged 验收标准 3（小正文落库内容不变）：
// 对不含截断的普通小请求体，trace_req_body 必须是「对象树往返 + 键排序」后的
// 确定性输出，用固定 golden 锁住改动前的行为。
func TestTraceCapture_SmallBody_GoldenBytesUnchanged(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)

	probe := serveTraceCapture(t, body, int64(len(body)))
	require.True(t, probe.sanitizedSet)
	assert.False(t, probe.rawSet)
	assert.Equal(t, traceSmallBodyGolden, string(probe.sanitized))
	assert.Equal(t, string(body), string(probe.handlerBody), "body 必须原样恢复给下游 handler")
}

// TestTraceCapture_BodyKeysMutuallyExclusive 验收标准 3（D2）：
// 两条路径互斥，同一次请求只设置其中一个 context 键，且阈值边界为「<= 走 sanitize」。
func TestTraceCapture_BodyKeysMutuallyExclusive(t *testing.T) {
	tests := []struct {
		name        string
		description string
		bodySize    int
		wantRaw     bool
	}{
		{
			name:        "小正文 1KB",
			description: "远小于 1 MB 阈值，走 sanitize 路径：只设置 trace_req_body",
			bodySize:    1 << 10,
			wantRaw:     false,
		},
		{
			name:        "正好 1MB（阈值含等号）",
			description: "len(body) == MaxParseBytesInTrace 仍走 sanitize 路径：只设置 trace_req_body",
			bodySize:    traceMaxParseBytes,
			wantRaw:     false,
		},
		{
			name:        "1MB + 1 字节",
			description: "刚超过阈值即切到大正文直存：只设置 trace_req_body_raw",
			bodySize:    traceMaxParseBytes + 1,
			wantRaw:     true,
		},
		{
			name:        "超大正文 2MB",
			description: "远超阈值，走大正文直存路径：只设置 trace_req_body_raw",
			bodySize:    2 << 20,
			wantRaw:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := jsonBodyOfSize(t, tt.bodySize)
			require.Len(t, body, tt.bodySize)

			probe := serveTraceCapture(t, body, int64(len(body)))
			if tt.wantRaw {
				assert.True(t, probe.rawSet, tt.description)
				assert.False(t, probe.sanitizedSet, "两条路径互斥，不得同时设置 %s", tt.description)
			} else {
				assert.True(t, probe.sanitizedSet, tt.description)
				assert.False(t, probe.rawSet, "两条路径互斥，不得同时设置 %s", tt.description)
			}
			// 无论走哪条路径，body 都必须恢复给下游
			assert.Equal(t, string(body), string(probe.handlerBody))
		})
	}
}

// TestTraceCapture_LargeBody_SkipsJSONUnmarshalAndMarshal 验收标准 4：
// 大请求体（> 1 MB）不得触发 json.Unmarshal/json.Marshal。
// 用一个含 12 万个小字符串元素的大 body 放大反序列化的分配次数：
// 一旦发生 Unmarshal，仅 items 数组就会产生 10 万量级的分配，
// 而直存路径只有读取缓冲与 gin context 的常数级分配。
func TestTraceCapture_LargeBody_SkipsJSONUnmarshalAndMarshal(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"model":"gpt-4o","items":[`)
	const items = 120_000
	for i := 0; i < items; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`"abcdefg"`)
	}
	sb.WriteString(`]}`)
	body := []byte(sb.String())
	require.Greater(t, len(body), traceMaxParseBytes, "用例前提：请求体必须 > 1 MB")

	var probe traceCaptureProbe
	r := newTraceCaptureEngine(&probe, false)

	doRequest := func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}

	// 分配次数：若发生 Unmarshal，items 数组会产生 10 万量级分配；
	// 直存路径只有读取缓冲 + gin context 的常数级分配，阈值取 1000 留足余量。
	allocs := testing.AllocsPerRun(10, doRequest)
	assert.Less(t, allocs, 1000.0,
		"大正文路径不得做 JSON 双序列化（若 Unmarshal 则仅 items 数组就有 10 万量级分配），实测每请求 %v 次分配", allocs)

	// 分配字节：直存路径约 1×body（一份读取副本），双序列化路径约 3×body（对象树 + 输出副本）。
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const runs = 5
	for i := 0; i < runs; i++ {
		doRequest()
	}
	runtime.ReadMemStats(&after)
	perRun := int64(after.TotalAlloc-before.TotalAlloc) / runs
	t.Logf("大正文直存路径实测：每请求分配次数=%.0f，每请求分配字节=%d（body=%d 字节，双序列化约需 3×body）",
		allocs, perRun, len(body))
	assert.Less(t, perRun, int64(len(body))*2,
		"大正文路径每请求分配应约为 1×body，实测 %d 字节（body=%d 字节，双序列化约需 3×body）", perRun, len(body))

	assert.True(t, probe.rawSet)
	assert.False(t, probe.sanitizedSet)
	assert.Equal(t, body, probe.raw, "大正文必须原样直存（未经 sanitize）")
}

// TestTraceCapture_LiedContentLengthIsCapped 验收标准 4（预分配）：
// 客户端谎报超大 Content-Length（1<<40）时，预分配必须封顶，
// 不 panic、不 OOM，且读取结果与真实 body 逐字节一致。
func TestTraceCapture_LiedContentLengthIsCapped(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	const liedContentLength int64 = 1 << 40 // 1 TiB，远超任何真实 body

	var probe traceCaptureProbe
	r := newTraceCaptureEngine(&probe, true)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.ContentLength = liedContentLength
	req.Header.Set("Content-Length", fmt.Sprintf("%d", liedContentLength))

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	w := httptest.NewRecorder()
	// 若实现真的按谎报值 make 1 TiB，这里会直接 OOM/panic
	require.NotPanics(t, func() { r.ServeHTTP(w, req) })
	runtime.ReadMemStats(&after)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, string(body), string(probe.handlerBody), "读取结果必须与真实 body 逐字节一致")
	require.True(t, probe.sanitizedSet)
	assert.Equal(t, traceSmallBodyGolden, string(probe.sanitized))

	allocated := after.TotalAlloc - before.TotalAlloc
	// 单次请求分配量必须远小于谎报值（实现按 64 MB 封顶预分配）
	assert.Less(t, allocated, uint64(128<<20),
		"预分配必须封顶：不应按谎报的 %d 字节申请内存，实测单请求分配 %d 字节", liedContentLength, allocated)
}

// TestTraceCapture_UnknownContentLengthStillReadsWholeBody 验收标准 4（预分配）：
// ContentLength <= 0（长度未知，如分块传输）时不预分配，仍必须读全 body，
// 且小正文（< 1 MB）依旧走 sanitize 路径。
func TestTraceCapture_UnknownContentLengthStillReadsWholeBody(t *testing.T) {
	for _, cl := range []int64{0, -1} {
		t.Run(fmt.Sprintf("ContentLength=%d", cl), func(t *testing.T) {
			body := jsonBodyOfSize(t, 300<<10)

			var probe traceCaptureProbe
			r := newTraceCaptureEngine(&probe, true)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			req.ContentLength = cl
			req.Header.Del("Content-Length")

			w := httptest.NewRecorder()
			require.NotPanics(t, func() { r.ServeHTTP(w, req) })

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, len(body), len(probe.handlerBody), "未知长度时也必须读全 body")
			assert.True(t, probe.sanitizedSet)
			assert.False(t, probe.rawSet)
			assert.True(t, json.Valid(probe.sanitized))
		})
	}
}
