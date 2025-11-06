package middleware

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "llm-gateway"

// Tracing 创建入口 span 中间件
// 为每个请求创建一个根 span，记录 HTTP 方法、路径、状态码等信息
func Tracing() gin.HandlerFunc {
	tracer := otel.Tracer(tracerName)
	propagator := otel.GetTextMapPropagator()

	return func(c *gin.Context) {
		// 从请求头中提取上游 trace context（支持 W3C TraceContext 传播）
		ctx := propagator.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		// 创建入口 span
		spanName := c.Request.Method + " " + c.FullPath()
		if c.FullPath() == "" {
			spanName = c.Request.Method + " " + c.Request.URL.Path
		}

		ctx, span := tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPMethod(c.Request.Method),
				semconv.HTTPTarget(c.Request.URL.Path),
				attribute.String("http.request_id", c.GetString("X-Request-ID")),
			),
		)
		defer span.End()

		// 将带 span 的 context 注入请求
		c.Request = c.Request.WithContext(ctx)

		// 执行后续 handler
		c.Next()

		// 记录响应状态
		status := c.Writer.Status()
		span.SetAttributes(semconv.HTTPStatusCode(status))

		if status >= 400 {
			span.SetAttributes(attribute.Bool("error", true))
		}

		// 记录模型名（如果有）
		if model, exists := c.Get("model"); exists {
			span.SetAttributes(attribute.String("llm.model", model.(string)))
		}
	}
}
