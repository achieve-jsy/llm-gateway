package litellm

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// LiteLLMError 通用 OpenAI 兼容 API 返回的错误结构
type LiteLLMError struct {
	HTTPStatus int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

// Error 实现 error interface
func (e *LiteLLMError) Error() string {
	return fmt.Sprintf("litellm error [%d] %s: %s", e.HTTPStatus, e.Code, e.Message)
}

// liteLLMErrorResponse OpenAI 兼容格式错误响应体
type liteLLMErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ToLiteLLMError 将 HTTP 响应解析为 LiteLLMError
func ToLiteLLMError(httpStatus int, body []byte) error {
	le := &LiteLLMError{
		HTTPStatus: httpStatus,
		Code:       http.StatusText(httpStatus),
		Message:    string(body),
	}

	var errResp liteLLMErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		le.Code = errResp.Error.Code
		le.Message = errResp.Error.Message
	}

	return le
}
