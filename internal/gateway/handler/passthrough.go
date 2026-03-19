package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// IsPassthroughMode checks whether the request has the X-Error-Format: passthrough header.
// When passthrough mode is active, upstream error responses are returned as-is without
// normalization to the gateway's canonical error format.
func IsPassthroughMode(c *gin.Context) bool {
	return c.GetHeader("X-Error-Format") == "passthrough"
}

// writePassthroughError writes an upstream error response as-is from GatewayError fields,
// bypassing the gateway's canonical error normalization.
func writePassthroughError(c *gin.Context, status int, errType string, code string, message string) {
	// Check for stashed upstream headers from proxy handling.
	if h, ok := c.Get("passthrough_headers"); ok {
		if headers, ok := h.(map[string]string); ok {
			for k, v := range headers {
				c.Header(k, v)
			}
		}
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"code":    code,
			"message": message,
		},
	})
}

// SetPassthroughSourceHeaders copies upstream response headers to the gin context
// for later use in passthrough error responses. Only stashes when passthrough mode is active.
func SetPassthroughSourceHeaders(c *gin.Context, headers http.Header) {
	if !IsPassthroughMode(c) {
		return
	}
	h := make(map[string]string)
	for k, v := range headers {
		if len(v) > 0 {
			h[k] = v[0]
		}
	}
	c.Set("passthrough_headers", h)
}