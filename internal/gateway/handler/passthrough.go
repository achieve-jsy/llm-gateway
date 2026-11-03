package handler

import (
	"net/http"
	"strings"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

// IsPassthroughMode checks whether the request has the X-Error-Format: passthrough header.
// When passthrough mode is active, upstream error responses are returned as-is without
// normalization to the gateway's canonical error format.
func IsPassthroughMode(c *gin.Context) bool {
	return c.GetHeader("X-Error-Format") == "passthrough"
}

// writePassthroughError writes the upstream status and exact response body when
// the provider response was retained. If the error was generated locally (for
// example, a timeout or an empty model group), it falls back to the gateway
// error envelope because there is no upstream response to copy.
func writePassthroughError(c *gin.Context, ue *model.UnifiedError) {
	if ue == nil {
		return
	}

	// Check for stashed upstream headers from proxy handling.
	if h, ok := c.Get("passthrough_headers"); ok {
		if headers, ok := h.(map[string]string); ok {
			for k, v := range headers {
				c.Header(k, v)
			}
		}
	}

	status := ue.HTTPStatus
	contentType := strings.TrimSpace(c.GetHeader("Content-Type"))
	if ue.Upstream != nil {
		// The normalized mapper deliberately uses 502/503 for gateway policy.
		// Passthrough mode must instead expose the status returned by the provider.
		if ue.Upstream.Status >= 100 && ue.Upstream.Status <= 599 {
			status = ue.Upstream.Status
		}
		if headers, ok := c.Get("passthrough_headers"); ok {
			if values, ok := headers.(map[string]string); ok && contentType == "" {
				contentType = values["Content-Type"]
			}
		}
		if len(ue.Upstream.RawBody) > 0 {
			if contentType == "" {
				contentType = "application/json"
			}
			c.Data(status, contentType, ue.Upstream.RawBody)
			return
		}
	}

	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    string(ue.Type),
			"code":    ue.Code,
			"message": ue.Message,
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
