package bailian

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// BailianError 百炼 API 返回的错误结构
type BailianError struct {
	HTTPStatus int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	RequestID  string `json:"request_id,omitempty"`
}

// Error 实现 error interface
func (e *BailianError) Error() string {
	return fmt.Sprintf("bailian error [%d] %s: %s", e.HTTPStatus, e.Code, e.Message)
}

// bailianErrorResponse 百炼 API 错误响应体结构
type bailianErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ToBailianError 将百炼 HTTP 响应解析为 BailianError
// httpStatus: HTTP 状态码; body: 响应体 JSON 字节
func ToBailianError(httpStatus int, body []byte) error {
	be := &BailianError{
		HTTPStatus: httpStatus,
		Code:       http.StatusText(httpStatus),
		Message:    string(body),
	}

	// 尝试解析结构化错误（百炼 OpenAI 兼容格式）
	var errResp bailianErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		be.Code = errResp.Error.Code
		be.Message = errResp.Error.Message
	}

	return be
}
