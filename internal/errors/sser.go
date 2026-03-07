package errors

import (
	"github.com/gin-gonic/gin"
)

// WriteSSEError sends an error event during an active SSE stream.
//
// Unlike WriteError, which decides between JSON and SSE based on the
// "streaming" context key, WriteSSEError unconditionally sends the error
// as SSE events. This is useful when the stream has already been initiated
// (headers already sent) and an error occurs mid-stream.
//
// It sends an "error" event with the error payload, followed by a "[DONE]"
// sentinel to signal the end of the stream.
func WriteSSEError(c *gin.Context, code string, opts ...ErrorOption) {
	ec := Lookup(code)

	o := buildOptions(opts...)

	message := ec.Message
	if o.message != "" {
		message = o.message
	}

	resp := ErrorResponse{
		Type:    ec.Type,
		Code:    ec.Code,
		Message: message,
		Param:   o.param,
		Details: o.details,
	}
	if o.upstream != nil {
		resp.Upstream = o.upstream
	}
	if len(o.upstreams) > 0 {
		resp.Upstreams = o.upstreams
	}

	c.SSEvent("error", resp)
	c.SSEvent("", "[DONE]")
	c.Writer.Flush()
}