package errormap

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/example/llm-gateway/internal/core"
	gatewayerrors "github.com/example/llm-gateway/internal/errors"
	"github.com/example/llm-gateway/internal/gateway/model"
)

// MapProviderError 将 provider 返回的原始错误映射为统一错误
// 使用新的 errors.MapProviderError 声明式映射引擎替代旧的字符串匹配。
func MapProviderError(err error) *model.UnifiedError {
	if err == nil {
		return nil
	}

	// 已经是 UnifiedError 则直接返回
	var ue *model.UnifiedError
	if errors.As(err, &ue) {
		return ue
	}

	// 超时检测：context 超时
	if errors.Is(err, context.DeadlineExceeded) {
		return model.ErrProviderTimeout
	}

	// 上下文取消
	if errors.Is(err, context.Canceled) {
		return model.NewUnifiedError(model.ErrorTypeTempFailure, "request_canceled", "request was canceled by client", 499)
	}

	// 尝试从 GatewayError 提取 provider/status/body 信息，走声明式映射
	var gwErr *core.GatewayError
	if errors.As(err, &gwErr) {
		provider := gwErr.Provider
		status := gwErr.StatusCode
		body := formatErrorBody(gwErr)

		code, _ := gatewayerrors.MapProviderError(provider, status, body, nil)
		if ec, ok := gatewayerrors.Registry[code]; ok {
			// 构建 UnifiedError，保留原始错误信息
			msg := ec.Message
			if gwErr.Message != "" {
				msg = gwErr.Message
			}
			return &model.UnifiedError{
				Type:       model.ErrorType(ec.Type),
				Code:       ec.Code,
				Message:    msg,
				HTTPStatus: ec.HTTPStatus,
			}
		}
	}

	// Fallback: 返回通用上游错误
	return model.ErrProviderError
}

// formatErrorBody 构造一个适合关键词匹配的 JSON body。
func formatErrorBody(gwErr *core.GatewayError) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": gwErr.Message,
			"type":    gwErr.Type,
		},
	})
	return body
}