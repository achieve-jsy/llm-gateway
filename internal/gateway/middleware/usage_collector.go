package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
)

// UsageCollector 注入 Collector 到请求处理链
// 在 c.Next() 后构造 UsageEvent 并异步投递埋点
// 仅对 LLM 调用路径（/v1/chat/completions, /v1/embeddings 等）生效
func UsageCollector(collector *gwobs.Collector) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		// 仅采集 LLM 调用，跳过健康检查和管理接口
		if path == "/health" || path == "/ready" {
			c.Next()
			return
		}

		start := time.Now()
		c.Set("start_time", start)

		c.Next()

		// 埋点：调用完成后构造 UsageEvent 并异步投递
		if collector == nil {
			return
		}
		event := collector.NewUsageEvent(c)
		// 补充状态（c.Next() 之后才有 status）
		event.Status = c.Writer.Status()
		event.DurationMs = time.Since(start).Milliseconds()
		collector.Emit(event)
	}
}