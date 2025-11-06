package model

import "net/http"

// ErrorType 统一错误类型
type ErrorType string

const (
	ErrorTypeInvalidRequest ErrorType = "invalid_request"    // 400 输入错误
	ErrorTypeModelNotFound  ErrorType = "model_not_found"    // 404 模型不存在
	ErrorTypeModelUnavail   ErrorType = "model_unavailable"  // 503 模型不可用
	ErrorTypeTempFailure    ErrorType = "temporary_failure"  // 502/504 临时失败
	ErrorTypeAuthError      ErrorType = "authentication_error" // 401 认证失败
	ErrorTypeInternal       ErrorType = "internal_error"     // 500 内部错误
)

// UnifiedError 统一错误结构
type UnifiedError struct {
	Type       ErrorType `json:"type"`
	Code       string    `json:"code"`
	Message    string    `json:"message"`
	HTTPStatus int       `json:"-"`
}

func (e *UnifiedError) Error() string {
	return e.Message
}

// 预定义错误
var (
	ErrInvalidRequestBody = &UnifiedError{
		Type: ErrorTypeInvalidRequest, Code: "invalid_request_body",
		Message: "request body is invalid", HTTPStatus: http.StatusBadRequest,
	}
	ErrModelRequired = &UnifiedError{
		Type: ErrorTypeInvalidRequest, Code: "model_required",
		Message: "model field is required", HTTPStatus: http.StatusBadRequest,
	}
	ErrMessagesRequired = &UnifiedError{
		Type: ErrorTypeInvalidRequest, Code: "messages_required",
		Message: "messages field is required and must not be empty", HTTPStatus: http.StatusBadRequest,
	}
	ErrModelNotFound = &UnifiedError{
		Type: ErrorTypeModelNotFound, Code: "unknown_model",
		Message: "the requested model is not registered", HTTPStatus: http.StatusNotFound,
	}
	ErrModelUnavailable = &UnifiedError{
		Type: ErrorTypeModelUnavail, Code: "model_unavailable",
		Message: "the requested model is temporarily unavailable", HTTPStatus: http.StatusServiceUnavailable,
	}
	ErrProviderTimeout = &UnifiedError{
		Type: ErrorTypeTempFailure, Code: "provider_timeout",
		Message: "upstream provider did not respond in time", HTTPStatus: http.StatusGatewayTimeout,
	}
	ErrProviderError = &UnifiedError{
		Type: ErrorTypeTempFailure, Code: "provider_error",
		Message: "upstream provider returned an error", HTTPStatus: http.StatusBadGateway,
	}
	ErrUnauthorized = &UnifiedError{
		Type: ErrorTypeAuthError, Code: "invalid_api_key",
		Message: "invalid or missing API key", HTTPStatus: http.StatusUnauthorized,
	}
	ErrKeyDisabled = &UnifiedError{
		Type: ErrorTypeAuthError, Code: "key_disabled",
		Message: "the API key has been disabled", HTTPStatus: http.StatusUnauthorized,
	}
	ErrAuthServiceDown = &UnifiedError{
		Type: "service_unavailable", Code: "auth_service_unavailable",
		Message: "auth service temporarily unavailable", HTTPStatus: http.StatusServiceUnavailable,
	}
	ErrInternal = &UnifiedError{
		Type: ErrorTypeInternal, Code: "internal",
		Message: "internal server error", HTTPStatus: http.StatusInternalServerError,
	}
)

// NewUnifiedError 创建自定义统一错误
func NewUnifiedError(errType ErrorType, code, message string, httpStatus int) *UnifiedError {
	return &UnifiedError{
		Type:       errType,
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
	}
}
