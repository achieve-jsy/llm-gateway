package bedrock

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// BedrockError AWS Bedrock API 返回的错误结构
type BedrockError struct {
	HTTPStatus int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

// Error 实现 error interface
func (e *BedrockError) Error() string {
	return fmt.Sprintf("bedrock error [%d] %s: %s", e.HTTPStatus, e.Code, e.Message)
}

// bedrockErrorResponse Bedrock API 错误响应体结构
type bedrockErrorResponse struct {
	Message string `json:"message"`
	Type    string `json:"__type"`
}

// ToBedrockError 将 HTTP 响应解析为 BedrockError
func ToBedrockError(httpStatus int, body []byte) error {
	be := &BedrockError{
		HTTPStatus: httpStatus,
		Code:       http.StatusText(httpStatus),
		Message:    string(body),
	}

	var errResp bedrockErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
		be.Code = errResp.Type
		be.Message = errResp.Message
	}

	return be
}
