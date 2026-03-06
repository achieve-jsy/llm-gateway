package middleware

import "github.com/gin-gonic/gin"

// SetTraceResponseObject 在 handler 写出 JSON 响应前登记响应体，供 UsageCollector 写入 call_traces。
func SetTraceResponseObject(c *gin.Context, obj any) {
	c.Set("stream", false)
	c.Set("resp_object", obj)
}
