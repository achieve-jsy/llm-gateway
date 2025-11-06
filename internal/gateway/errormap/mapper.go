package errormap

import (
	"context"
	"errors"
	"strings"

	"github.com/example/llm-gateway/internal/gateway/model"
)

// MapProviderError 将 provider 返回的原始错误映射为统一错误
// 按错误内容分为三类：输入错误(400)、不可用(404/503)、临时失败(502/504)
func MapProviderError(err error) *model.UnifiedError {
	if err == nil {
		return nil
	}

	// 已经是 UnifiedError 则直接返回
	var ue *model.UnifiedError
	if errors.As(err, &ue) {
		return ue
	}

	msg := strings.ToLower(err.Error())

	// 超时检测
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "timeout") {
		return model.ErrProviderTimeout
	}

	// 上下文取消
	if errors.Is(err, context.Canceled) {
		return model.NewUnifiedError(model.ErrorTypeTempFailure, "request_canceled", "request was canceled by client", 499)
	}

	// 输入类错误 (400)
	if strings.Contains(msg, "invalid") || strings.Contains(msg, "bad request") || strings.Contains(msg, "400") {
		return model.NewUnifiedError(model.ErrorTypeInvalidRequest, "invalid_request_body", err.Error(), 400)
	}

	// 模型不存在 (404)
	if strings.Contains(msg, "not found") || strings.Contains(msg, "404") {
		return model.NewUnifiedError(model.ErrorTypeModelNotFound, "unknown_model", err.Error(), 404)
	}

	// 模型不可用 (503)
	if strings.Contains(msg, "unavailable") || strings.Contains(msg, "503") {
		return model.NewUnifiedError(model.ErrorTypeModelUnavail, "model_unavailable", err.Error(), 503)
	}

	// 认证错误 (401)
	if strings.Contains(msg, "unauthorized") || strings.Contains(msg, "401") || strings.Contains(msg, "authentication") {
		return model.ErrUnauthorized
	}

	// 默认归类为临时失败 (502)
	return model.ErrProviderError
}
