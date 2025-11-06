package deepseek

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// DeepSeekError DeepSeek API 返回的错误结构
type DeepSeekError struct {
	HTTPStatus int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

// Error 实现 error interface
func (e *DeepSeekError) Error() string {
	return fmt.Sprintf("deepseek error [%d] %s: %s", e.HTTPStatus, e.Code, e.Message)
}

// deepSeekErrorResponse DeepSeek API 错误响应体结构（OpenAI 兼容格式）
type deepSeekErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ToDeepSeekError 将 HTTP 响应解析为 DeepSeekError
func ToDeepSeekError(httpStatus int, body []byte) error {
	de := &DeepSeekError{
		HTTPStatus: httpStatus,
		Code:       http.StatusText(httpStatus),
		Message:    string(body),
	}

	var errResp deepSeekErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		de.Code = errResp.Error.Code
		de.Message = errResp.Error.Message
	}

	return de
}
