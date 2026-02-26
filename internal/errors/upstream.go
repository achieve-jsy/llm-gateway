package errors

// UpstreamInfo carries diagnostic information about an upstream provider error
// that is surfaced to gateway operators for debugging without exposing
// sensitive internals to end users.
type UpstreamInfo struct {
	// Provider is the upstream provider name, e.g. "openai", "anthropic".
	Provider string `json:"provider"`

	// Status is the HTTP status code returned by the upstream provider.
	Status int `json:"status"`

	// Headers contains the response headers from the upstream provider.
	// Only safe-to-expose headers should be included.
	Headers map[string]string `json:"headers,omitempty"`

	// Body is the parsed (or raw) response body from the upstream provider.
	Body any `json:"body,omitempty"`

	// RequestID is the upstream request ID for correlating with the provider.
	RequestID string `json:"request_id,omitempty"`
}