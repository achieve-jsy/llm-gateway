package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// bodyBudgetFallbackBytesForTest 是 D7 中分块传输（ContentLength <= 0）的保守预留值（4 MB）。
const bodyBudgetFallbackBytesForTest int64 = 4 << 20

// bodyBudgetEnvelope 是 D7 规定的 503 错误信封，必须与 handler.respondError 完全一致。
const bodyBudgetEnvelope = `{"error":{"type":"model_unavailable","code":"model_unavailable","message":"the requested model is temporarily unavailable"}}`

// newBodyBudgetRouter 构造挂了 BodyBudgetLimiter 的路由：
//   - POST /v1/chat/completions：阻塞型 handler，进入时通知 entered 并阻塞在 hold 上，
//     用于精确控制「谁在持有预算」。
//   - POST /v1/embeddings：直通 handler，用于观察请求是否放行。
func newBodyBudgetRouter(budget int64, hold <-chan struct{}, entered chan<- struct{}, handled *atomic.Int32) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.BodyBudgetLimiter(budget))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		if handled != nil {
			handled.Add(1)
		}
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		<-hold
		c.Status(http.StatusOK)
	})
	r.POST("/v1/embeddings", func(c *gin.Context) {
		if handled != nil {
			handled.Add(1)
		}
		c.Status(http.StatusOK)
	})
	return r
}

// newBudgetPostRequest 构造 POST 请求并谎报 Content-Length：
// contentLength >= 0 时直接写入字段与头；为 -1 时模拟分块传输（未知长度）。
// 注意：body 实际都很小，这里只影响中间件用来申请预算的权重。
func newBudgetPostRequest(t *testing.T, path string, contentLength int64) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	if contentLength >= 0 {
		req.ContentLength = contentLength
		req.Header.Set("Content-Length", fmt.Sprintf("%d", contentLength))
	} else {
		// net/http 用 ContentLength = -1 表示长度未知（Transfer-Encoding: chunked）
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
		req.Header.Del("Content-Length")
	}
	return req
}

// closedChan 返回一个已关闭的 channel，用作「不阻塞」的 hold 信号。
func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// TestBodyBudgetLimiter_ChunkedRequestConsumesBudget 验收标准 7（D7）：
// 分块传输在 net/http 里是 ContentLength = -1（不是 0），也必须走预算——
// 按 4 MB 保守预留。这里预算正好 4 MB：第一个 chunked 请求占满预算后，
// 第二个 chunked 请求必须排队等待，而不是被跳过预算直接放行。
func TestBodyBudgetLimiter_ChunkedRequestConsumesBudget(t *testing.T) {
	const budget = bodyBudgetFallbackBytesForTest

	entered := make(chan struct{}, 1)
	hold := make(chan struct{})
	var handled atomic.Int32
	r := newBodyBudgetRouter(budget, hold, entered, &handled)

	holderReq := newBudgetPostRequest(t, "/v1/chat/completions", -1)
	waiterReq := newBudgetPostRequest(t, "/v1/embeddings", -1)

	holderDone := make(chan struct{})
	go func() {
		r.ServeHTTP(httptest.NewRecorder(), holderReq)
		close(holderDone)
	}()
	<-entered // 第一个请求已持满整份预算

	waiterCode := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, waiterReq)
		waiterCode <- w.Code
	}()

	// 预算已被占满：第二个 chunked 请求不得立即完成
	select {
	case code := <-waiterCode:
		t.Fatalf("预算已被占满，第二个 chunked 请求不应立即完成（code=%d），说明它没有参与预算", code)
	case <-time.After(500 * time.Millisecond):
	}

	close(hold)
	select {
	case code := <-waiterCode:
		assert.Equal(t, http.StatusOK, code, "释放预算后 chunked 请求必须能通过")
	case <-time.After(5 * time.Second):
		t.Fatal("释放预算后第二个请求仍未完成")
	}
	select {
	case <-holderDone:
	case <-time.After(5 * time.Second):
		t.Fatal("持有预算的请求未结束")
	}
}

// TestBodyBudgetLimiter_ZeroContentLengthBypassesBudget 记录 D7 的边界语义：
// 预算只对「POST 且 ContentLength != 0」生效，ContentLength == 0 视为无请求体，
// 即使预算已被占满也直接放行。分块传输是 -1 而不是 0，走预算路径（见 chunked 用例）。
func TestBodyBudgetLimiter_ZeroContentLengthBypassesBudget(t *testing.T) {
	const budget = bodyBudgetFallbackBytesForTest

	entered := make(chan struct{}, 1)
	hold := make(chan struct{})
	r := newBodyBudgetRouter(budget, hold, entered, nil)

	holderReq := newBudgetPostRequest(t, "/v1/chat/completions", budget)
	holderDone := make(chan struct{})
	go func() {
		r.ServeHTTP(httptest.NewRecorder(), holderReq)
		close(holderDone)
	}()
	<-entered // 整份预算已被占满

	zeroReq := newBudgetPostRequest(t, "/v1/embeddings", 0)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, zeroReq)
	assert.Equal(t, http.StatusOK, w.Code, "ContentLength == 0（无请求体）不占预算，必须直接放行")

	close(hold)
	select {
	case <-holderDone:
	case <-time.After(5 * time.Second):
		t.Fatal("持有预算的请求未结束")
	}
}

// TestBodyBudgetLimiter_ExhaustedReturns503AndRecovers 验收标准 7（D7）：
// 预算耗尽时申请等待 10s 超时后必须返回 503 model_unavailable（绝不是 413），
// 错误体与规范一致，且预算释放后同样的请求可以再次通过。
func TestBodyBudgetLimiter_ExhaustedReturns503AndRecovers(t *testing.T) {
	if testing.Short() {
		t.Skip("该用例需要等待 D7 的 10s 预算申请超时")
	}

	const budget = int64(8 << 20)

	entered := make(chan struct{}, 1)
	hold := make(chan struct{})
	var handled atomic.Int32
	r := newBodyBudgetRouter(budget, hold, entered, &handled)

	holderReq := newBudgetPostRequest(t, "/v1/chat/completions", budget) // 权重 = 整份预算
	holderDone := make(chan struct{})
	go func() {
		r.ServeHTTP(httptest.NewRecorder(), holderReq)
		close(holderDone)
	}()
	<-entered

	blockedReq := newBudgetPostRequest(t, "/v1/embeddings", budget)
	w := httptest.NewRecorder()
	start := time.Now()
	r.ServeHTTP(w, blockedReq)
	elapsed := time.Since(start)

	// 超时返回 503，错误码与 OOM 期间客户端已会重试的错误码一致
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.NotEqual(t, http.StatusRequestEntityTooLarge, w.Code, "D7 明确要求绝不返回 413")
	assert.GreaterOrEqual(t, elapsed, 9*time.Second, "预算耗尽时必须先排队等待申请超时，而不是立即拒绝")
	assert.JSONEq(t, bodyBudgetEnvelope, w.Body.String())
	assert.Equal(t, int32(1), handled.Load(), "被 503 拒绝的请求不得进入 handler")

	// 预算释放后必须能再次通过
	close(hold)
	select {
	case <-holderDone:
	case <-time.After(5 * time.Second):
		t.Fatal("持有预算的请求未结束")
	}

	retryReq := newBudgetPostRequest(t, "/v1/chat/completions", budget)
	retryW := httptest.NewRecorder()
	r.ServeHTTP(retryW, retryReq)
	assert.Equal(t, http.StatusOK, retryW.Code, "预算释放后同样的请求必须能通过")
	assert.Equal(t, int32(2), handled.Load())
}

// TestBodyBudgetLimiter_OversizedSingleRequestIsCappedNotRejected D7：
// 单个请求体超过总预算时按满额预算申请（权重封顶），而不是直接失败/永久阻塞，
// 因此仍然应该放行。
func TestBodyBudgetLimiter_OversizedSingleRequestIsCappedNotRejected(t *testing.T) {
	const budget = int64(1 << 20)

	r := newBodyBudgetRouter(budget, closedChan(), nil, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newBudgetPostRequest(t, "/v1/embeddings", budget*4))

	assert.Equal(t, http.StatusOK, w.Code, "超出总预算的单请求应封顶申请并放行，而不是失败")
}
