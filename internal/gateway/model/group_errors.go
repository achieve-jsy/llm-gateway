package model

import (
	"errors"
	"net/http"
)

var (
	ErrGroupNotFound         = errors.New("模型组不存在")
	ErrNoAvailableModel      = errors.New("该模型组当前无可用模型")
	ErrNoOtherModelAvailable = errors.New("该模型组无其他可用模型")

	// ErrVideoInputNotSupported 请求带视频内容片段，但路由到的模型组内没有
	// 任何声明支持视频输入的成员。400 而非 503：这是请求与模型能力不匹配，
	// 换模型或去掉视频即可成功，重试同一请求不会变好。
	ErrVideoInputNotSupported = &UnifiedError{
		Type:       ErrorTypeInvalidRequest,
		Code:       "video_input_not_supported",
		Message:    "当前模型未声明支持视频输入；请改用支持视频的模型组，或移除视频内容片段",
		HTTPStatus: http.StatusBadRequest,
	}
)
