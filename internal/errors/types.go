// Package errors provides a unified error infrastructure for the LLM gateway.
//
// It defines canonical error types aligned with OpenAI's error taxonomy,
// a centralized error code registry, a declarative provider error mapper,
// and renderer functions that replace hand-written gin.H error responses.
package errors

// ErrorType represents the high-level error category, aligned with OpenAI's
// official error type naming conventions.
type ErrorType string

const (
	// ErrorTypeInvalidRequest indicates a malformed or missing request parameter.
	// HTTP 400 Bad Request.
	ErrorTypeInvalidRequest ErrorType = "invalid_request_error"

	// ErrorTypeAuthentication indicates missing or invalid credentials.
	// HTTP 401 Unauthorized.
	ErrorTypeAuthentication ErrorType = "authentication_error"

	// ErrorTypePermissionDenied indicates the caller lacks permission for the
	// requested resource. HTTP 403 Forbidden.
	ErrorTypePermissionDenied ErrorType = "permission_denied_error"

	// ErrorTypeNotFound indicates the requested resource does not exist.
	// HTTP 404 Not Found.
	ErrorTypeNotFound ErrorType = "not_found_error"

	// ErrorTypeRateLimit indicates the caller has exceeded a rate or quota limit.
	// HTTP 429 Too Many Requests.
	ErrorTypeRateLimit ErrorType = "rate_limit_error"

	// ErrorTypeAPI indicates an internal gateway error.
	// HTTP 500 Internal Server Error.
	ErrorTypeAPI ErrorType = "api_error"

	// ErrorTypeUpstream indicates an error originating from an upstream provider.
	// Externally surfaced as "api_error" to match OpenAI conventions.
	ErrorTypeUpstream ErrorType = "api_error"
)