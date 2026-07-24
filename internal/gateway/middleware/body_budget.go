package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/semaphore"
)

// DefaultBodyBudgetBytes 是 /v1 在途请求体的默认字节预算（96 MB）。
//
// 取值依据（端到端实测：真实 gateway 二进制 + 真实 Postgres + 8 并发 × 30 MB 请求）：
//   - 预算 256 MB：峰值 RSS 1456 MB；且 8 个请求的正文总量 240 MB 直接冲垮 128 MB 的
//     trace 队列，丢弃 3 条 trace（既费内存又丢数据）。
//   - 预算  96 MB：峰值 RSS 923 MB；8 条 trace 全部落库、0 丢弃、0 超时。
//
// 实测放大倍数约 9.6×（每 1 字节在途正文 → 约 9.6 字节常驻内存，含 Go GC 的 2× 目标
// 与转发路径的 JSON 双序列化），远高于早期按 2.5× 的估算。按 9.6× 反推：
//   - 2g 容器：(2048×0.8 − 400) / 9.6 ≈ 129 MB，96 MB 有安全余量；
//   - 1g 容器：(1024×0.8 − 400) / 9.6 ≈  43 MB，1g 下必须调得更小。
//
// 调整本常量时必须同步复核容器内存上限（deploy/blue-green/docker-compose.*.yml）。
const DefaultBodyBudgetBytes int64 = 96 << 20

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
