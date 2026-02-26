package errors

import (
	"net/http"
	"testing"
)

// TestAllCodesHaveValidType ensures every registered error code has a
// non-empty ErrorType.
func TestAllCodesHaveValidType(t *testing.T) {
	for code, ec := range Registry {
		if ec.Type == "" {
			t.Errorf("error code %q has empty Type", code)
		}
	}
}

// TestAllCodesHaveValidHTTPStatus ensures every registered error code has a
// recognizable HTTP status code (4xx or 5xx).
func TestAllCodesHaveValidHTTPStatus(t *testing.T) {
	for code, ec := range Registry {
		if ec.HTTPStatus < 400 || ec.HTTPStatus >= 600 {
			t.Errorf("error code %q has unexpected HTTPStatus %d (expected 4xx or 5xx)", code, ec.HTTPStatus)
		}
	}
}

// TestTypeStatusCorrespondence verifies that ErrorType values map to
// reasonable HTTP status codes.
//
// Upstream provider errors (codes starting with "provider_" or "upstream_")
// proxy the upstream's status code, so they may use any 4xx/5xx status.
// Non-upstream errors must have tighter type-to-status correspondence.
func TestTypeStatusCorrespondence(t *testing.T) {
	validRanges := map[ErrorType][2]int{
		ErrorTypeInvalidRequest:    {400, 499},
		ErrorTypeAuthentication:    {401, 401},
		ErrorTypePermissionDenied:  {403, 403},
		ErrorTypeNotFound:          {404, 404},
		ErrorTypeRateLimit:         {429, 429},
	}
	// ErrorTypeAPI and ErrorTypeUpstream share the same string value "api_error",
	// so they share the same range.
	apiErrorRange := [2]int{500, 599}

	for code, ec := range Registry {
		// Upstream/provider errors may use any 4xx/5xx status since they
		// proxy the upstream's error code.
		if isUpstreamCode(code) {
			if ec.HTTPStatus < 400 || ec.HTTPStatus >= 600 {
				t.Errorf("upstream code %q has HTTPStatus %d, expected 4xx or 5xx", code, ec.HTTPStatus)
			}
			continue
		}

		rng, ok := validRanges[ec.Type]
		if !ok {
			// ErrorTypeAPI and ErrorTypeUpstream share the same string value.
			if ec.Type == ErrorTypeAPI || ec.Type == ErrorTypeUpstream {
				rng = apiErrorRange
			} else {
				t.Errorf("error code %q has unrecognized Type %q", code, ec.Type)
				continue
			}
		}
		if ec.HTTPStatus < rng[0] || ec.HTTPStatus > rng[1] {
			t.Errorf("error code %q: type %q expects HTTP status in [%d, %d], got %d",
				code, ec.Type, rng[0], rng[1], ec.HTTPStatus)
		}
	}
}

// TestFallbackCodesHaveReasonableStatus ensures that codes marked as Fallback
// have an HTTP status that makes sense for a retryable error.
func TestFallbackCodesHaveReasonableStatus(t *testing.T) {
	for code, ec := range Registry {
		if !ec.Fallback {
			continue
		}
		// Fallback codes should be 4xx (client errors that can be retried
		// with a different model), not 5xx.
		if ec.HTTPStatus < 400 || ec.HTTPStatus >= 500 {
			t.Errorf("fallback code %q has HTTPStatus %d; fallback codes should be 4xx", code, ec.HTTPStatus)
		}
	}
}

// TestFallbackCodesAreMarkedCorrectly verifies the specific codes that should
// have Fallback=true.
func TestFallbackCodesAreMarkedCorrectly(t *testing.T) {
	expectedFallback := map[string]bool{
		"content_policy_violation": true,
		"context_window_exceeded":  true,
	}

	for code, ec := range Registry {
		expected, exists := expectedFallback[code]
		if !exists {
			if ec.Fallback {
				t.Errorf("unexpected fallback code %q: only %v should be marked Fallback", code, expectedFallback)
			}
			continue
		}
		if ec.Fallback != expected {
			t.Errorf("code %q: Fallback=%v, expected %v", code, ec.Fallback, expected)
		}
	}
}

// TestNoDuplicateCodes ensures the Registry has no duplicate Code values.
func TestNoDuplicateCodes(t *testing.T) {
	seen := make(map[string]string) // Code -> first key
	for key, ec := range Registry {
		if firstKey, exists := seen[ec.Code]; exists {
			t.Errorf("duplicate Code %q: keys %q and %q", ec.Code, firstKey, key)
		}
		seen[ec.Code] = key
	}
}

// TestAllCodesHaveMessage ensures every registered error code has a non-empty
// default message.
func TestAllCodesHaveMessage(t *testing.T) {
	for code, ec := range Registry {
		if ec.Message == "" {
			t.Errorf("error code %q has empty Message", code)
		}
	}
}

// TestAllCodesMatchRegistryKey ensures the Code field matches the registry key.
func TestAllCodesMatchRegistryKey(t *testing.T) {
	for key, ec := range Registry {
		if key != ec.Code {
			t.Errorf("registry key %q does not match Code field %q", key, ec.Code)
		}
	}
}

// TestRegistrySize verifies the expected number of error codes.
func TestRegistrySize(t *testing.T) {
	expected := 26
	if len(Registry) != expected {
		t.Errorf("Registry has %d codes, expected %d", len(Registry), expected)
	}
}

// TestRateLimitCodesHave429 ensures rate limit codes return HTTP 429.
// Upstream provider rate limit codes are excluded: they use 502 because
// the gateway proxies the error upstream.
func TestRateLimitCodesHave429(t *testing.T) {
	for code, ec := range Registry {
		if isUpstreamCode(code) {
			continue
		}
		if ec.Type == ErrorTypeRateLimit && ec.HTTPStatus != http.StatusTooManyRequests {
			t.Errorf("rate limit code %q has HTTPStatus %d, expected 429", code, ec.HTTPStatus)
		}
	}
}

// TestAuthenticationCodesHave401 ensures authentication codes return HTTP 401
// or 403 (for permission denied). Upstream provider auth codes are excluded.
func TestAuthenticationCodesHave401(t *testing.T) {
	for code, ec := range Registry {
		if isUpstreamCode(code) {
			continue
		}
		if ec.Type == ErrorTypeAuthentication {
			if ec.HTTPStatus != http.StatusUnauthorized {
				// Some auth codes may legitimately be 403
				if ec.HTTPStatus != http.StatusForbidden {
					t.Errorf("auth code %q has HTTPStatus %d, expected 401 or 403", code, ec.HTTPStatus)
				}
			}
		}
	}
}

// isUpstreamCode returns true for error codes that represent upstream provider
// errors. These codes use 502/503/504 regardless of their logical ErrorType,
// because the gateway is proxying the error rather than originating it.
func isUpstreamCode(code string) bool {
	upstreamPrefixes := []string{"provider_", "upstream_"}
	for _, prefix := range upstreamPrefixes {
		if len(code) > len(prefix) && code[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// TestBuiltinCodesExist verifies that the most critical error codes are
// present in the Registry.
func TestBuiltinCodesExist(t *testing.T) {
	required := []string{
		"request_missing_model",
		"request_invalid_param",
		"request_body_invalid",
		"auth_key_invalid",
		"auth_key_disabled",
		"auth_key_expired",
		"auth_model_group_denied",
		"quota_rpm_exceeded",
		"quota_concurrency_exceeded",
		"model_not_found",
		"model_group_not_found",
		"model_unavailable",
		"provider_bad_request",
		"provider_auth_error",
		"provider_not_found",
		"provider_rate_limited",
		"provider_overloaded",
		"provider_insufficient_quota",
		"upstream_error",
		"upstream_unavailable",
		"upstream_timeout",
		"content_policy_violation",
		"context_window_exceeded",
		"internal_error",
		"provider_error",
		"all_providers_failed",
	}

	for _, code := range required {
		if _, ok := Registry[code]; !ok {
			t.Errorf("required error code %q is missing from Registry", code)
		}
	}
}