package errormap

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/model"
)

// TestMapProviderError_NilInput 处理 nil 错误
func TestMapProviderError_NilInput(t *testing.T) {
	result := MapProviderError(nil)
	if result != nil {
		t.Fatal("expected nil for nil input")
	}
}

// TestMapProviderError_AlreadyUnifiedError 已经是 UnifiedError
func TestMapProviderError_AlreadyUnifiedError(t *testing.T) {
	original := model.NewUnifiedError(model.ErrorTypeInvalidRequest, "custom_error", "custom message", http.StatusBadRequest)
	result := MapProviderError(original)

	if result != original {
		t.Fatal("expected same UnifiedError instance")
	}

	if result.Type != model.ErrorTypeInvalidRequest {
		t.Fatalf("expected type InvalidRequest, got %s", result.Type)
	}

	if result.Code != "custom_error" {
		t.Fatalf("expected code custom_error, got %s", result.Code)
	}
}

// TestMapProviderError_DeadlineExceeded 超时错误（DeadlineExceeded）
func TestMapProviderError_DeadlineExceeded(t *testing.T) {
	err := context.DeadlineExceeded
	result := MapProviderError(err)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result != model.ErrProviderTimeout {
		t.Fatalf("expected ErrProviderTimeout, got %v", result)
	}

	if result.Type != model.ErrorTypeTempFailure {
		t.Fatalf("expected type TempFailure, got %s", result.Type)
	}

	if result.HTTPStatus != http.StatusGatewayTimeout {
		t.Fatalf("expected status %d, got %d", http.StatusGatewayTimeout, result.HTTPStatus)
	}
}

// TestMapProviderError_TimeoutString 超时错误（字符串匹配）
func TestMapProviderError_TimeoutString(t *testing.T) {
	err := errors.New("request timeout after 30s")
	result := MapProviderError(err)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result != model.ErrProviderTimeout {
		t.Fatalf("expected ErrProviderTimeout, got %v", result)
	}

	if result.HTTPStatus != http.StatusGatewayTimeout {
		t.Fatalf("expected status %d, got %d", http.StatusGatewayTimeout, result.HTTPStatus)
	}
}

// TestMapProviderError_ContextCanceled 上下文取消
func TestMapProviderError_ContextCanceled(t *testing.T) {
	err := context.Canceled
	result := MapProviderError(err)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Type != model.ErrorTypeTempFailure {
		t.Fatalf("expected type TempFailure, got %s", result.Type)
	}

	if result.HTTPStatus != 499 {
		t.Fatalf("expected status 499, got %d", result.HTTPStatus)
	}

	if result.Code != "request_canceled" {
		t.Fatalf("expected code request_canceled, got %s", result.Code)
	}
}

// TestMapProviderError_InvalidRequest 无效请求（400）
func TestMapProviderError_InvalidRequest(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		keyword  string
	}{
		{"invalid keyword", "invalid request format", "invalid"},
		{"bad request", "bad request body", "bad request"},
		{"400 status", "error 400: invalid", "400"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.errMsg)
			result := MapProviderError(err)

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if result.Type != model.ErrorTypeInvalidRequest {
				t.Fatalf("expected type InvalidRequest, got %s", result.Type)
			}

			if result.HTTPStatus != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, result.HTTPStatus)
			}

			if result.Code != "invalid_request_body" {
				t.Fatalf("expected code invalid_request_body, got %s", result.Code)
			}
		})
	}
}

// TestMapProviderError_ModelNotFound 模型不存在（404）
func TestMapProviderError_ModelNotFound(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
	}{
		{"not found keyword", "model not found"},
		{"404 status", "error 404: not found"},
		{"not found phrase", "resource not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.errMsg)
			result := MapProviderError(err)

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if result.Type != model.ErrorTypeModelNotFound {
				t.Fatalf("expected type ModelNotFound, got %s", result.Type)
			}

			if result.HTTPStatus != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d", http.StatusNotFound, result.HTTPStatus)
			}
		})
	}
}

// TestMapProviderError_ModelUnavailable 模型不可用（503）
func TestMapProviderError_ModelUnavailable(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
	}{
		{"unavailable keyword", "model unavailable"},
		{"503 status", "error 503: service unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.errMsg)
			result := MapProviderError(err)

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if result.Type != model.ErrorTypeModelUnavail {
				t.Fatalf("expected type ModelUnavail, got %s", result.Type)
			}

			if result.HTTPStatus != http.StatusServiceUnavailable {
				t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, result.HTTPStatus)
			}
		})
	}
}

// TestMapProviderError_AuthenticationError 认证错误（401）
func TestMapProviderError_AuthenticationError(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
	}{
		{"unauthorized", "unauthorized"},
		{"401 status", "error 401: unauthorized"},
		{"authentication", "authentication failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.errMsg)
			result := MapProviderError(err)

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if result != model.ErrUnauthorized {
				t.Fatalf("expected ErrUnauthorized, got %v", result)
			}

			if result.Type != model.ErrorTypeAuthError {
				t.Fatalf("expected type AuthError, got %s", result.Type)
			}

			if result.HTTPStatus != http.StatusUnauthorized {
				t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, result.HTTPStatus)
			}
		})
	}
}

// TestMapProviderError_DefaultTempFailure 默认为临时失败（502）
func TestMapProviderError_DefaultTempFailure(t *testing.T) {
	err := errors.New("unknown provider error")
	result := MapProviderError(err)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result != model.ErrProviderError {
		t.Fatalf("expected ErrProviderError, got %v", result)
	}

	if result.Type != model.ErrorTypeTempFailure {
		t.Fatalf("expected type TempFailure, got %s", result.Type)
	}

	if result.HTTPStatus != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, result.HTTPStatus)
	}
}

// TestMapProviderError_CaseSensitivity 不区分大小写
func TestMapProviderError_CaseSensitivity(t *testing.T) {
	tests := []struct {
		errMsg string
		expect model.ErrorType
	}{
		{"INVALID REQUEST", model.ErrorTypeInvalidRequest},
		{"Bad Request", model.ErrorTypeInvalidRequest},
		{"NOT FOUND", model.ErrorTypeModelNotFound},
		{"UNAUTHORIZED", model.ErrorTypeAuthError},
		{"UNAVAILABLE", model.ErrorTypeModelUnavail},
		{"TIMEOUT", model.ErrorTypeTempFailure},
	}

	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			err := errors.New(tt.errMsg)
			result := MapProviderError(err)

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if result.Type != tt.expect {
				t.Fatalf("expected type %s, got %s", tt.expect, result.Type)
			}
		})
	}
}

// TestMapProviderError_PriorityOrdering 错误分类优先级测试
func TestMapProviderError_PriorityOrdering(t *testing.T) {
	// timeout 检测应该在前面
	err := errors.New("timeout - invalid request 400")
	result := MapProviderError(err)

	// 应该被识别为 timeout，而不是 invalid
	if result.Type != model.ErrorTypeTempFailure {
		t.Fatalf("expected timeout to be recognized, got type %s", result.Type)
	}
}

// TestMapProviderError_ExactUnifiedErrorMatch 确切的 UnifiedError 匹配
func TestMapProviderError_ExactUnifiedErrorMatch(t *testing.T) {
	original := model.ErrModelNotFound
	result := MapProviderError(original)

	if result != original {
		t.Fatal("expected same instance for predefined errors")
	}

	if result.Type != model.ErrorTypeModelNotFound {
		t.Fatalf("expected ModelNotFound type, got %s", result.Type)
	}
}

// TestMapProviderError_WrappedError 包装的错误
func TestMapProviderError_WrappedError(t *testing.T) {
	originalErr := errors.New("invalid request body")
	wrappedErr := errors.New("failed: " + originalErr.Error())

	result := MapProviderError(wrappedErr)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// 应该仍然能识别 "invalid"
	if result.Type != model.ErrorTypeInvalidRequest {
		t.Fatalf("expected InvalidRequest type for wrapped error, got %s", result.Type)
	}
}

// TestMapProviderError_EmptyMessage 空错误消息
func TestMapProviderError_EmptyMessage(t *testing.T) {
	err := errors.New("")
	result := MapProviderError(err)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// 空消息应该默认为临时失败
	if result.Type != model.ErrorTypeTempFailure {
		t.Fatalf("expected TempFailure for empty message, got %s", result.Type)
	}
}

// TestMapProviderError_MultipleKeywords 包含多个关键字的错误消息
func TestMapProviderError_MultipleKeywords(t *testing.T) {
	// 包含 "invalid" 和 "timeout" 的错误应该优先匹配 timeout
	err := errors.New("timeout error: invalid request parameters")
	result := MapProviderError(err)

	if result.Type != model.ErrorTypeTempFailure {
		t.Fatalf("expected timeout priority, got type %s", result.Type)
	}
}

// TestMapProviderError_HTTPStatusMapping HTTP 状态码映射正确性
func TestMapProviderError_HTTPStatusMapping(t *testing.T) {
	tests := []struct {
		errMsg       string
		expectedCode int
	}{
		{"invalid", http.StatusBadRequest},
		{"not found", http.StatusNotFound},
		{"unavailable", http.StatusServiceUnavailable},
		{"unauthorized", http.StatusUnauthorized},
		{"timeout", http.StatusGatewayTimeout},
		{"unknown", http.StatusBadGateway},
	}

	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			err := errors.New(tt.errMsg)
			result := MapProviderError(err)

			if result.HTTPStatus != tt.expectedCode {
				t.Fatalf("expected status %d, got %d for error: %s", tt.expectedCode, result.HTTPStatus, tt.errMsg)
			}
		})
	}
}

// TestMapProviderError_ErrorCodeMapping 错误代码映射
func TestMapProviderError_ErrorCodeMapping(t *testing.T) {
	tests := []struct {
		errMsg       string
		expectedCode string
	}{
		{"invalid", "invalid_request_body"},
		{"not found", "unknown_model"},
		{"unavailable", "model_unavailable"},
		{"unauthorized", "invalid_api_key"},
		{"timeout", "provider_timeout"},
		{"unknown", "provider_error"},
	}

	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			err := errors.New(tt.errMsg)
			result := MapProviderError(err)

			if result.Code != tt.expectedCode {
				t.Fatalf("expected code %s, got %s for error: %s", tt.expectedCode, result.Code, tt.errMsg)
			}
		})
	}
}

// TestMapProviderError_LowercaseConversion 小写转换
func TestMapProviderError_LowercaseConversion(t *testing.T) {
	err := errors.New("INVALID REQUEST")
	result := MapProviderError(err)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Type != model.ErrorTypeInvalidRequest {
		t.Fatalf("expected InvalidRequest after lowercase conversion, got %s", result.Type)
	}
}

// TestMapProviderError_SpecialCharacters 特殊字符处理
func TestMapProviderError_SpecialCharacters(t *testing.T) {
	err := errors.New("error [503] - service:unavailable!")
	result := MapProviderError(err)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Type != model.ErrorTypeModelUnavail {
		t.Fatalf("expected ModelUnavail despite special characters, got %s", result.Type)
	}
}
