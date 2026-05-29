package errormap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

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
		return &model.UnifiedError{
			Type: model.ErrorTypeTempFailure, Code: "provider_timeout",
			Message:    "upstream provider did not respond in time",
			HTTPStatus: http.StatusGatewayTimeout,
			Upstream: &gatewayerrors.UpstreamInfo{
				Body: err.Error(),
			},
		}
	}

	// 上下文取消
	if errors.Is(err, context.Canceled) {
		return &model.UnifiedError{
			Type: model.ErrorTypeTempFailure, Code: "request_canceled",
			Message:    "request was canceled by client",
			HTTPStatus: 499,
			Upstream: &gatewayerrors.UpstreamInfo{
				Body: err.Error(),
			},
		}
	}

	// 尝试从 GatewayError 提取 provider/status/body 信息，走声明式映射
	var gwErr *core.GatewayError
	if errors.As(err, &gwErr) {
		provider := gwErr.Provider
		status := gwErr.StatusCode
		body := formatErrorBody(gwErr)

		code, upstream := gatewayerrors.MapProviderError(provider, status, body, nil)
		// 展示用原始上游 body 覆盖，让客户端看到真实错误结构（含 code/type/param）；
		// 映射仍用重建 body（含网关分类后的 error.type），避免 openai 401 等被原始 type 误判为 bad_request。
		if len(gwErr.Body) > 0 {
			upstream.Body = parseErrorBody(gwErr.Body)
		}
		if gatewayerrors.Has(code) {
			ec := gatewayerrors.Lookup(code)
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
				Upstream:   &upstream,
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

// parseErrorBody 将原始上游 body 解析为 JSON 结构，非 JSON 时回退为原始字符串。
// 与 internal/errors 映射引擎的展示逻辑保持一致。
func parseErrorBody(body []byte) any {
	if len(body) == 0 {
		return nil
	}
	if json.Valid(body) {
		var parsed any
		if err := json.Unmarshal(body, &parsed); err == nil {
			return parsed
		}
	}
	return string(body)
}
