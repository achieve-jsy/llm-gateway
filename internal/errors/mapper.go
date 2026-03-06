package errors

import (
	"encoding/json"
	"net/http"
	"strings"
)

// MapProviderError maps an upstream provider error to a canonical error code
// and enriched UpstreamInfo. The mapping follows a priority chain:
//
//  1. Structured field extraction from the JSON body (e.g. error.type, error.code)
//  2. Keyword matching on the error message string
//  3. HTTP status code fallback
//  4. Generic fallback ("provider_error")
//
// Parameters:
//   - provider: the upstream provider name, e.g. "openai", "anthropic"
//   - status: the HTTP status code returned by the upstream
//   - body: the raw response body bytes from the upstream
//   - headers: the response headers from the upstream (may be nil)
func MapProviderError(provider string, status int, body []byte, headers map[string]string) (code string, upstream UpstreamInfo) {
	upstream = UpstreamInfo{
		Provider: provider,
		Status:   status,
		Headers:  headers,
	}

	// Try to parse the body as structured JSON for display and matching.
	var parsedBody any
	if len(body) > 0 {
		if json.Valid(body) {
			var parsed any
			if err := json.Unmarshal(body, &parsed); err == nil {
				parsedBody = parsed
				upstream.Body = parsed
			}
		} else {
			upstream.Body = string(body)
		}
	}

	// If we couldn't parse JSON, fall through to keyword/status-based matching.
	bodyStr := string(body)
	msgLower := strings.ToLower(bodyStr)

	// Step 1: try structured field matching.
	if rule, ok := providerRules[normalizeProvider(provider)]; ok {
		if code := matchStructured(parsedBody, rule); code != "" {
			return code, upstream
		}
	}

	// Step 2: try keyword matching across all providers.
	if code := matchKeywords(msgLower); code != "" {
		return code, upstream
	}

	// Step 3: HTTP status fallback.
	if code := statusFallback(status); code != "" {
		return code, upstream
	}

	// Step 4: generic fallback.
	return "provider_error", upstream
}

// normalizeProvider maps provider aliases to canonical names for rule lookup.
func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	aliases := map[string]string{
		"azure-openai": "azure",
		"aws":          "bedrock",
		"aws-bedrock":  "bedrock",
		"gcp":          "vertex",
		"google":       "vertex",
		"anthropic-bedrock": "bedrock",
	}
	if canonical, ok := aliases[provider]; ok {
		return canonical
	}
	return provider
}

// ── Provider rule definitions ────────────────────────────────────────────────

// providerRule defines how to map structured error fields from a specific
// provider's error response body to canonical error codes.
type providerRule struct {
	Structured []structField
	Keywords   []keywordRule
}

// structField maps a JSON field path in the error body to canonical codes.
// The Field is a dot-separated path, e.g. "error.type" or "error.code".
// Mapping maps provider-specific values to canonical error codes.
type structField struct {
	Field   string
	Mapping map[string]string
}

// keywordRule maps a keyword pattern found in the error message to a
// canonical error code.
type keywordRule struct {
	Keywords []string // case-insensitive substrings to match
	Code     string   // canonical error code
}

// providerRules holds the provider-specific structured error mapping rules.
// More providers can be added as needed.
var providerRules = map[string]providerRule{
	"openai": {
		Structured: []structField{
			{
				Field: "error.type",
				Mapping: map[string]string{
					"invalid_request_error":        "provider_bad_request",
					"authentication_error":         "provider_auth_error",
					"permission_denied_error":      "provider_auth_error",
					"not_found_error":              "provider_not_found",
					"rate_limit_error":             "provider_rate_limited",
					"insufficient_quota":           "provider_insufficient_quota",
					"server_error":                 "upstream_error",
					"tokens_exceeded":              "context_window_exceeded",
					"content_policy_violation":     "content_policy_violation",
				},
			},
			{
				Field: "error.code",
				Mapping: map[string]string{
					"invalid_api_key":              "provider_auth_error",
					"insufficient_quota":           "provider_insufficient_quota",
					"rate_limit_exceeded":          "provider_rate_limited",
					"context_length_exceeded":      "context_window_exceeded",
					"content_filter":               "content_policy_violation",
					"model_not_found":              "provider_not_found",
					"token_limit_exceeded":         "context_window_exceeded",
					"billing_hard_limit_reached":   "provider_insufficient_quota",
				},
			},
		},
		Keywords: []keywordRule{
			{Keywords: []string{"prompt is too long", "context length", "token limit", "maximum context", "reduce the length"}, Code: "context_window_exceeded"},
			{Keywords: []string{"content policy", "content filter", "safety", "content moderation"}, Code: "content_policy_violation"},
			{Keywords: []string{"overloaded", "over capacity", "currently overloaded"}, Code: "provider_overloaded"},
			{Keywords: []string{"rate limit", "too many requests", "rate_limit"}, Code: "provider_rate_limited"},
			{Keywords: []string{"insufficient_quota", "quota exceeded", "billing"}, Code: "provider_insufficient_quota"},
			{Keywords: []string{"invalid api key", "incorrect api key", "invalid x-api-key"}, Code: "provider_auth_error"},
			{Keywords: []string{"does not exist", "not found", "no such model", "unknown model"}, Code: "provider_not_found"},
			{Keywords: []string{"timeout", "timed out", "deadline exceeded"}, Code: "upstream_timeout"},
			{Keywords: []string{"service unavailable", "maintenance", "temporarily unavailable"}, Code: "upstream_unavailable"},
		},
	},

	"anthropic": {
		Structured: []structField{
			{
				Field: "error.type",
				Mapping: map[string]string{
					"invalid_request_error":      "provider_bad_request",
					"authentication_error":       "provider_auth_error",
					"permission_error":           "provider_auth_error",
					"not_found_error":            "provider_not_found",
					"rate_limit_error":           "provider_rate_limited",
					"overloaded_error":           "provider_overloaded",
					"api_error":                  "upstream_error",
				},
			},
		},
		Keywords: []keywordRule{
			{Keywords: []string{"prompt is too long", "context length", "token limit", "maximum context"}, Code: "context_window_exceeded"},
			{Keywords: []string{"overloaded", "over capacity"}, Code: "provider_overloaded"},
			{Keywords: []string{"rate limit", "too many requests"}, Code: "provider_rate_limited"},
			{Keywords: []string{"invalid x-api-key", "invalid api key", "authentication"}, Code: "provider_auth_error"},
			{Keywords: []string{"not found", "does not exist", "no such model"}, Code: "provider_not_found"},
			{Keywords: []string{"timeout", "timed out"}, Code: "upstream_timeout"},
			{Keywords: []string{"unavailable", "maintenance"}, Code: "upstream_unavailable"},
		},
	},

	"azure": {
		Structured: []structField{
			{
				Field: "error.code",
				Mapping: map[string]string{
					"InvalidParameter":           "provider_bad_request",
					"InvalidRequest":             "provider_bad_request",
					"Unauthorized":               "provider_auth_error",
					"AccessDenied":               "provider_auth_error",
					"NotFound":                   "provider_not_found",
					"RateLimitExceeded":          "provider_rate_limited",
					"QuotaExceeded":              "provider_insufficient_quota",
					"ServiceUnavailable":         "upstream_unavailable",
					"OperationNotAllowed":        "provider_auth_error",
					"content_filter":             "content_policy_violation",
				},
			},
		},
		Keywords: []keywordRule{
			{Keywords: []string{"content filter", "content policy", "safety"}, Code: "content_policy_violation"},
			{Keywords: []string{"context length", "token limit", "maximum context", "prompt is too long"}, Code: "context_window_exceeded"},
			{Keywords: []string{"rate limit", "too many requests"}, Code: "provider_rate_limited"},
			{Keywords: []string{"quota", "insufficient quota"}, Code: "provider_insufficient_quota"},
			{Keywords: []string{"overloaded", "over capacity"}, Code: "provider_overloaded"},
			{Keywords: []string{"unauthorized", "access denied", "invalid key"}, Code: "provider_auth_error"},
			{Keywords: []string{"not found", "does not exist"}, Code: "provider_not_found"},
			{Keywords: []string{"timeout", "timed out"}, Code: "upstream_timeout"},
			{Keywords: []string{"unavailable", "maintenance", "service unavailable"}, Code: "upstream_unavailable"},
		},
	},

	"bedrock": {
		Structured: []structField{
			{
				Field: "error.code",
				Mapping: map[string]string{
					"AccessDeniedException":       "provider_auth_error",
					"ValidationException":         "provider_bad_request",
					"ResourceNotFoundException":   "provider_not_found",
					"ThrottlingException":         "provider_rate_limited",
					"ModelNotReadyException":      "model_unavailable",
					"ServiceUnavailableException": "upstream_unavailable",
					"InternalServerException":     "upstream_error",
				},
			},
		},
		Keywords: []keywordRule{
			{Keywords: []string{"prompt is too long", "context length", "token limit", "maximum context"}, Code: "context_window_exceeded"},
			{Keywords: []string{"content filter", "content policy", "safety", "blocked"}, Code: "content_policy_violation"},
			{Keywords: []string{"throttl", "rate limit", "too many requests"}, Code: "provider_rate_limited"},
			{Keywords: []string{"access denied", "unauthorized", "forbidden", "signature"}, Code: "provider_auth_error"},
			{Keywords: []string{"not found", "does not exist", "resource not found"}, Code: "provider_not_found"},
			{Keywords: []string{"timeout", "timed out"}, Code: "upstream_timeout"},
			{Keywords: []string{"unavailable", "maintenance", "service unavailable"}, Code: "upstream_unavailable"},
			{Keywords: []string{"overloaded", "over capacity", "model not ready"}, Code: "provider_overloaded"},
		},
	},
}

// matchStructured tries to extract a canonical error code from the structured
// JSON fields of an error response body.
func matchStructured(body any, rule providerRule) string {
	if body == nil {
		return ""
	}
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return ""
	}

	for _, field := range rule.Structured {
		value := extractField(bodyMap, field.Field)
		if value == "" {
			continue
		}
		lower := strings.ToLower(value)
		for pattern, code := range field.Mapping {
			if strings.ToLower(pattern) == lower {
				return code
			}
		}
	}
	return ""
}

// extractField extracts a dot-separated field path from a nested map.
// e.g. "error.type" -> bodyMap["error"]["type"].
func extractField(m map[string]any, path string) string {
	parts := strings.Split(path, ".")
	var current any = m
	for _, part := range parts {
		cm, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = cm[part]
		if current == nil {
			return ""
		}
	}
	s, _ := current.(string)
	return s
}

// matchKeywords checks the error message against all known keyword rules
// across all providers and returns the first matching canonical code.
func matchKeywords(msgLower string) string {
	for _, rule := range providerRules {
		for _, kw := range rule.Keywords {
			for _, keyword := range kw.Keywords {
				if strings.Contains(msgLower, keyword) {
					return kw.Code
				}
			}
		}
	}
	return ""
}

// statusFallback maps an HTTP status code to a canonical error code.
func statusFallback(status int) string {
	switch {
	case status == http.StatusBadRequest:
		return "provider_bad_request"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "provider_auth_error"
	case status == http.StatusNotFound:
		return "provider_not_found"
	case status == http.StatusTooManyRequests:
		return "provider_rate_limited"
	case status == http.StatusGatewayTimeout:
		return "upstream_timeout"
	case status == http.StatusServiceUnavailable:
		return "upstream_unavailable"
	case status >= 500:
		return "upstream_error"
	default:
		return ""
	}
}