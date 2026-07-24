package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/semaphore"
)

// DefaultBodyBudgetBytes 是 /v1 在途请求体的默认字节预算（256 MB）。
//
// 它与容器内存上限（gateway 的 2g）是配套的：256 MB × 转发路径约 2.5× ≈ 640 MB，
// 加上进程基线约 380 MB，最坏同时存在约 1.0 GB，需要 2g 才有安全余量。
const DefaultBodyBudgetBytes int64 = 256 << 20

const (
	// bodyBudgetFallbackBytes 在 Content-Length 缺失（分块传输，值为 -1）时的保守预留。
	bodyBudgetFallbackBytes int64 = 4 << 20

	// bodyBudgetAcquireTimeout 申请在途预算的最长等待时间；超时即返回 503，不无限排队。
	bodyBudgetAcquireTimeout = 10 * time.Second
)

// BodyBudgetLimiter 限制同时「在途」的请求体总字节数，为 并发数 × 体积 提供硬上界。
//
// 与「入口总量上限」的区别：本中间件**不是请求体大小上限**——不读 body、不截断任何内容、
// 也永不返回 413。它是一个按字节计量的信号量：请求以自己的 Content-Length 为权重，
// 向 budgetBytes 的全局预算「排队取号」，拿到才继续往下走，并在整个请求生命周期内持有
// （defer Release），因此覆盖 body 读取、trace 捕获与转发上游的全过程。
//
// 请求体过大时的表现是**等待**（最长 10s 后返回 503 model_unavailable），而不是被拒绝或截断；
// 503 model_unavailable 与 OOM 重启期间客户端已经会重试的错误码一致。
//
// 注册位置见 server/router.go：必须在 AuthMiddleware 之后（未鉴权请求不占用预算）、
// TraceCapture 之前（请求体读取发生在 TraceCapture）。
func BodyBudgetLimiter(budgetBytes int64) gin.HandlerFunc {
	sem := semaphore.NewWeighted(budgetBytes)
	return func(c *gin.Context) {
		// 只对带请求体的 POST 生效；无 body 的请求（GET/HEAD、ContentLength == 0）直接放行。
		if c.Request.Method != http.MethodPost || c.Request.ContentLength == 0 {
			c.Next()
			return
		}

		weight := c.Request.ContentLength
		if weight <= 0 {
			// 分块传输（Transfer-Encoding: chunked）拿不到长度，按 4 MB 保守预留。
			weight = bodyBudgetFallbackBytes
		}
		// 权重必须封顶到总容量：semaphore.Acquire 对 weight > size 的请求不会排队，
		// 而是直接失败（或永久阻塞），所以超预算的单请求按满额预算申请。
		if weight > budgetBytes {
			weight = budgetBytes
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), bodyBudgetAcquireTimeout)
		defer cancel()
		if err := sem.Acquire(ctx, weight); err != nil {
			slog.WarnContext(c.Request.Context(), "body budget acquire timeout",
				"content_length", c.Request.ContentLength,
				"weight", weight,
				"budget_bytes", budgetBytes,
			)
			// 错误信封与 handler.respondError 输出一致（type/code/message 三项）
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{
					"type":    "model_unavailable",
					"code":    "model_unavailable",
					"message": "the requested model is temporarily unavailable",
				},
			})
			return
		}
		defer sem.Release(weight)

		c.Next()
	}
}
