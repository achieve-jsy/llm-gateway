package errors

// UpstreamInfo carries diagnostic information about an upstream provider error
// that is surfaced to gateway operators for debugging without exposing
// sensitive internals to end users.
//
// IMPORTANT: Be careful with what you set on the Body field. UpstreamInfo is
// serialized into the JSON error response returned to API consumers. Setting
// Body to the raw upstream response body can leak API keys, internal tokens,
// or other provider secrets to end users. Only populate Body when you are
// sure the content is safe to expose (e.g. in a debug/operator-only endpoint).
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