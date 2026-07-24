package quota

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// countingReadCloser 统计被读取的字节数，用于证明某个路径「没有」重复读取请求体。
type countingReadCloser struct {
	body   string
	offset int
	read   int
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	if c.offset >= len(c.body) {
		return 0, io.EOF
	}
	n := copy(p, c.body[c.offset:])
	c.offset += n
	c.read += n
	return n, nil
}

func (c *countingReadCloser) Close() error { return nil }

// newModelParamCtx 构造一个带真实 body 的 gin.Context。
func newModelParamCtx(t *testing.T, body string) (*gin.Context, *countingReadCloser) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	rc := &countingReadCloser{body: body}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Body = rc
	return c, rc
}

const modelParamBody = `{"model":"BF_ULTRA","messages":[{"role":"user","content":"hi"}]}`

// TestExtractModelParam_UsesRawTraceBodyWithoutRereading 是本文件的回归守卫。
//
// 背景：TraceCapture 对 > MaxParseBytesInTrace 的大正文只设置 trace_req_body_raw、
// 不再设置 trace_req_body。若这里只认 trace_req_body，model 提取就会落到「自行读取 body」
// 兜底分支，对同一份 30MB 级正文再全量 io.ReadAll 一遍——凭空多出 30MB+ 的分配与拷贝。
// 该缺陷在单元测试与顺序压测中都看不出来，只有真实并发压测才会暴露。
func TestExtractModelParam_UsesRawTraceBodyWithoutRereading(t *testing.T) {
	cases := []struct {
		name string
		set  func(c *gin.Context)
	}{
		{
			name: "小正文走 trace_req_body",
			set: func(c *gin.Context) {
				c.Set("trace_req_body", []byte(modelParamBody))
			},
		},
		{
			name: "大正文走 trace_req_body_raw",
			set: func(c *gin.Context) {
				c.Set("trace_req_body_raw", []byte(modelParamBody))
			},
		},
		{
			name: "两者都设置时优先 trace_req_body",
			set: func(c *gin.Context) {
				c.Set("trace_req_body", []byte(modelParamBody))
				c.Set("trace_req_body_raw", []byte(`{"model":"WRONG"}`))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rc := newModelParamCtx(t, modelParamBody)
			tc.set(c)

			got := extractModelParam(c)
			if got != "BF_ULTRA" {
				t.Fatalf("extractModelParam = %q, 期望 BF_ULTRA", got)
			}
			if rc.read != 0 {
				t.Fatalf("复用了 context 中的正文，就不应再读取请求体；实测读了 %d 字节", rc.read)
			}
		})
	}
}

// TestExtractModelParam_FallsBackToBodyAndRestores 覆盖两个 key 都缺失的兜底路径：
// 必须自行读取 body，且读完要恢复给下游 handler。
func TestExtractModelParam_FallsBackToBodyAndRestores(t *testing.T) {
	c, rc := newModelParamCtx(t, modelParamBody)

	got := extractModelParam(c)
	if got != "BF_ULTRA" {
		t.Fatalf("extractModelParam = %q, 期望 BF_ULTRA", got)
	}
	if rc.read != len(modelParamBody) {
		t.Fatalf("兜底路径应读取整个 body（%d 字节），实测 %d 字节", len(modelParamBody), rc.read)
	}

	// body 必须被恢复，handler 仍能完整读到
	restored, err := io.ReadAll(c.Request.Body)
	if err != nil {
		t.Fatalf("读取恢复后的 body 失败: %v", err)
	}
	if string(restored) != modelParamBody {
		t.Fatalf("body 未被正确恢复\n  得到: %s\n  期望: %s", restored, modelParamBody)
	}
}

// TestExtractModelParam_RequestModelTakesPrecedence 覆盖第 1 步的旧约定优先级。
func TestExtractModelParam_RequestModelTakesPrecedence(t *testing.T) {
	c, rc := newModelParamCtx(t, modelParamBody)
	c.Set("request_model", "EXPLICIT")
	c.Set("trace_req_body_raw", []byte(modelParamBody))

	if got := extractModelParam(c); got != "EXPLICIT" {
		t.Fatalf("extractModelParam = %q, 期望 EXPLICIT（request_model 优先）", got)
	}
	if rc.read != 0 {
		t.Fatalf("不应读取请求体，实测读了 %d 字节", rc.read)
	}
}

// TestExtractModelParam_FallsBackToQuery 覆盖最后的 query 兜底。
func TestExtractModelParam_FallsBackToQuery(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?model=FROM_QUERY", nil)
	c.Request.Body = io.NopCloser(strings.NewReader(`{"not_model":"x"}`))

	if got := extractModelParam(c); got != "FROM_QUERY" {
		t.Fatalf("extractModelParam = %q, 期望 FROM_QUERY", got)
	}
}
