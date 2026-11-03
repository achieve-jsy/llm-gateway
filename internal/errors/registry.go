package errors

import "net/http"

// ErrorCode defines a canonical, machine-readable error code with its metadata.
type ErrorCode struct {
	// Code is the machine-readable error code string, e.g. "quota_rpm_exceeded".
	Code string

	// Type is the high-level error category.
	Type ErrorType

	// HTTPStatus is the recommended HTTP status code for this error.
	HTTPStatus int

	// Message is the default human-readable message (Chinese).
	Message string

	// Fallback indicates that the request can be automatically retried with a
	// different model when this error occurs (e.g. context_window_exceeded).
	Fallback bool
}

// registry is the centralized map of all known error codes.
// It is unexported to prevent concurrent read/write from external packages.
// Use Lookup() for thread-safe read access.
var registry = map[string]ErrorCode{
	// ── Request validation errors ───────────────────────────────────────────
	"request_missing_model": {
		Code:       "request_missing_model",
		Type:       ErrorTypeInvalidRequest,
		HTTPStatus: http.StatusBadRequest,
		Message:    "model 字段是必填项",
	},
	"request_invalid_param": {
		Code:       "request_invalid_param",
		Type:       ErrorTypeInvalidRequest,
		HTTPStatus: http.StatusBadRequest,
		Message:    "请求参数无效",
	},
	"request_body_invalid": {
		Code:       "request_body_invalid",
		Type:       ErrorTypeInvalidRequest,
		HTTPStatus: http.StatusBadRequest,
		Message:    "请求体无效",
	},
	"unsupported_content_part": {
		Code:       "unsupported_content_part",
		Type:       ErrorTypeInvalidRequest,
		HTTPStatus: http.StatusBadRequest,
		Message:    "内容片段类型或字段不受支持",
	},

	// ── Authentication errors ───────────────────────────────────────────────
	"auth_key_invalid": {
		Code:       "auth_key_invalid",
		Type:       ErrorTypeAuthentication,
		HTTPStatus: http.StatusUnauthorized,
		Message:    "API Key 无效或缺失",
	},
	"auth_key_disabled": {
		Code:       "auth_key_disabled",
		Type:       ErrorTypeAuthentication,
		HTTPStatus: http.StatusUnauthorized,
		Message:    "API Key 已被禁用",
	},
	"auth_key_expired": {
		Code:       "auth_key_expired",
		Type:       ErrorTypeAuthentication,
		HTTPStatus: http.StatusUnauthorized,
		Message:    "API Key 已过期",
	},
	"auth_model_group_denied": {
		Code:       "auth_model_group_denied",
		Type:       ErrorTypePermissionDenied,
		HTTPStatus: http.StatusForbidden,
		Message:    "当前部门无权使用该模型组",
	},

	// ── Rate limit / quota errors ───────────────────────────────────────────
	"quota_rpm_exceeded": {
		Code:       "quota_rpm_exceeded",
		Type:       ErrorTypeRateLimit,
		HTTPStatus: http.StatusTooManyRequests,
		Message:    "请求频率(RPM)已达上限",
	},
	"quota_concurrency_exceeded": {
		Code:       "quota_concurrency_exceeded",
		Type:       ErrorTypeRateLimit,
		HTTPStatus: http.StatusTooManyRequests,
		Message:    "并发数已达上限",
	},

	// ── Model / group errors ────────────────────────────────────────────────
	"model_not_found": {
		Code:       "model_not_found",
		Type:       ErrorTypeNotFound,
		HTTPStatus: http.StatusNotFound,
		Message:    "请求的模型未注册",
	},
	"model_group_not_found": {
		Code:       "model_group_not_found",
		Type:       ErrorTypeNotFound,
		HTTPStatus: http.StatusNotFound,
		Message:    "请求的模型组不存在",
	},
	"model_unavailable": {
		Code:       "model_unavailable",
		Type:       ErrorTypeAPI,
		HTTPStatus: http.StatusServiceUnavailable,
		Message:    "请求的模型暂时不可用",
	},
	"model_capability_mismatch": {
		Code:       "model_capability_mismatch",
		Type:       ErrorTypeInvalidRequest,
		HTTPStatus: http.StatusBadRequest,
		Message:    "请求输入与模型能力不匹配",
	},

	// ── Upstream business errors (structured provider errors) ───────────────
	"provider_bad_request": {
		Code:       "provider_bad_request",
		Type:       ErrorTypeInvalidRequest,
		HTTPStatus: http.StatusBadRequest,
		Message:    "上游服务拒绝请求",
	},
	"provider_auth_error": {
		Code:       "provider_auth_error",
		Type:       ErrorTypeAuthentication,
		HTTPStatus: http.StatusBadGateway,
		Message:    "上游服务认证失败",
	},
	"provider_not_found": {
		Code:       "provider_not_found",
		Type:       ErrorTypeNotFound,
		HTTPStatus: http.StatusNotFound,
		Message:    "上游服务未找到资源",
	},
	"provider_rate_limited": {
		Code:       "provider_rate_limited",
		Type:       ErrorTypeRateLimit,
		HTTPStatus: http.StatusBadGateway,
		Message:    "上游服务触发限流",
	},
	"provider_overloaded": {
		Code:       "provider_overloaded",
		Type:       ErrorTypeUpstream,
		HTTPStatus: http.StatusBadGateway,
		Message:    "上游服务过载",
	},
	"provider_insufficient_quota": {
		Code:       "provider_insufficient_quota",
		Type:       ErrorTypeRateLimit,
		HTTPStatus: http.StatusBadGateway,
		Message:    "上游服务配额不足",
	},

	// ── Upstream infrastructure errors ──────────────────────────────────────
	"upstream_error": {
		Code:       "upstream_error",
		Type:       ErrorTypeUpstream,
		HTTPStatus: http.StatusBadGateway,
		Message:    "上游服务返回错误",
	},
	"upstream_unavailable": {
		Code:       "upstream_unavailable",
		Type:       ErrorTypeUpstream,
		HTTPStatus: http.StatusServiceUnavailable,
		Message:    "上游服务不可用",
	},
	"upstream_timeout": {
		Code:       "upstream_timeout",
		Type:       ErrorTypeUpstream,
		HTTPStatus: http.StatusGatewayTimeout,
		Message:    "上游服务超时",
	},

	// ── Content / context errors ────────────────────────────────────────────
	"content_policy_violation": {
		Code:       "content_policy_violation",
		Type:       ErrorTypeInvalidRequest,
		HTTPStatus: http.StatusBadRequest,
		Message:    "内容违反安全策略",
		Fallback:   true,
	},
	"context_window_exceeded": {
		Code:       "context_window_exceeded",
		Type:       ErrorTypeInvalidRequest,
		HTTPStatus: http.StatusBadRequest,
		Message:    "上下文窗口超出限制",
		Fallback:   true,
	},

	// ── Internal errors ─────────────────────────────────────────────────────
	"internal_error": {
		Code:       "internal_error",
		Type:       ErrorTypeAPI,
		HTTPStatus: http.StatusInternalServerError,
		Message:    "内部服务器错误",
	},

	// ── Fallback / catch-all ────────────────────────────────────────────────
	"provider_error": {
		Code:       "provider_error",
		Type:       ErrorTypeUpstream,
		HTTPStatus: http.StatusBadGateway,
		Message:    "上游服务返回错误",
	},
	"all_providers_failed": {
		Code:       "all_providers_failed",
		Type:       ErrorTypeUpstream,
		HTTPStatus: http.StatusBadGateway,
		Message:    "所有上游服务均已失败",
	},
}

// Lookup returns the ErrorCode for the given code string.
// If the code is not found, it returns the "internal_error" fallback.
// This is safe for concurrent use as the registry is never mutated after init.
func Lookup(code string) ErrorCode {
	ec, ok := registry[code]
	if !ok {
		return registry["internal_error"]
	}
	return ec
}

// Has reports whether the given code string is registered.
func Has(code string) bool {
	_, ok := registry[code]
	return ok
}
