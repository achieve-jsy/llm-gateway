package errors

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrorResponse is the JSON structure returned to API consumers.
// It follows the OpenAI-compatible error envelope format.
type ErrorResponse struct {
	Type    ErrorType       `json:"type"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Param   *string         `json:"param,omitempty"`
	Details map[string]any  `json:"details,omitempty"`
	Upstream *UpstreamInfo  `json:"upstream,omitempty"`
	Upstreams []UpstreamInfo `json:"upstreams,omitempty"`
}

// errorOptions holds the accumulated options for constructing an error response.
type errorOptions struct {
	message      string
	details      map[string]any
	param        *string
	upstream     *UpstreamInfo
	upstreams    []UpstreamInfo
	retryAfter   int
}

// ErrorOption is a functional option for customizing error responses.
type ErrorOption func(*errorOptions)

// WithMessage overrides the default error message from the registry.
func WithMessage(msg string) ErrorOption {
	return func(o *errorOptions) {
		o.message = msg
	}
}

// WithDetails attaches additional structured details to the error response.
func WithDetails(details map[string]any) ErrorOption {
	return func(o *errorOptions) {
		o.details = details
	}
}

// WithUpstream attaches a single upstream provider error diagnostic.
func WithUpstream(upstream UpstreamInfo) ErrorOption {
	return func(o *errorOptions) {
		o.upstream = &upstream
	}
}

// WithUpstreams attaches multiple upstream provider error diagnostics
// (e.g. when all providers in a failover chain have been exhausted).
func WithUpstreams(upstreams []UpstreamInfo) ErrorOption {
	return func(o *errorOptions) {
		o.upstreams = upstreams
	}
}

// WithParam annotates the error with the offending request parameter name.
func WithParam(param string) ErrorOption {
	return func(o *errorOptions) {
		o.param = &param
	}
}

// WithRetryAfter sets the Retry-After header value (in seconds) for rate-limit
// responses. Only meaningful for 429 responses.
func WithRetryAfter(seconds int) ErrorOption {
	return func(o *errorOptions) {
		o.retryAfter = seconds
	}
}

// WriteError is the canonical error rendering function. It replaces all
// hand-written gin.H error responses with a single, consistent output path.
//
// WriteError looks up the error code in the Registry and produces a JSON
// response (non-streaming) or an SSE error event (streaming), depending on
// whether the gin context carries a "streaming" marker.
//
// If the code is not found in the Registry, it falls back to "internal_error".
func WriteError(c *gin.Context, code string, opts ...ErrorOption) {
	ec, ok := Registry[code]
	if !ok {
		ec = Registry["internal_error"]
	}

	o := buildOptions(opts...)

	// Retrieve the request ID injected by RequestID middleware.
	requestID, _ := c.Get("request_id")
	rid, _ := requestID.(string)

	// Determine the effective message.
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

	status := ec.HTTPStatus

	// Set Retry-After header for rate-limit responses.
	if o.retryAfter > 0 && status == http.StatusTooManyRequests {
		c.Header("Retry-After", http.StatusText(o.retryAfter))
	}

	// Inject request_id into the response header if we have one.
	if rid != "" {
		c.Header("X-Request-ID", rid)
	}

	// Detect streaming mode: if the gin context has a "streaming" marker set
	// to true, send the error as SSE events instead of a JSON body.
	if isStreaming(c) {
		writeSSEErrorResponse(c, resp)
		return
	}

	c.JSON(status, gin.H{"error": resp})
}

// buildOptions applies all functional options and returns the accumulated
// errorOptions.
func buildOptions(opts ...ErrorOption) errorOptions {
	o := errorOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// isStreaming checks whether the current request is a streaming (SSE) request.
// It reads the "streaming" key from the gin context, which should be set by
// the handler before invoking the upstream call.
func isStreaming(c *gin.Context) bool {
	v, exists := c.Get("streaming")
	if !exists {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// writeSSEErrorResponse sends an error as SSE events to the client.
// This is used mid-stream when an error occurs after the SSE connection
// has already been established.
func writeSSEErrorResponse(c *gin.Context, resp ErrorResponse) {
	// Write the error as a named SSE event so clients can distinguish it
	// from data events.
	c.SSEvent("error", resp)
	// Signal the end of the stream.
	c.SSEvent("", "[DONE]")
	c.Writer.Flush()
}