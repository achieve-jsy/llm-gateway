package orchestrator

import (
	"errors"

	"github.com/example/llm-gateway/internal/core"
)

// extractUpstreamErrorAttrs extracts structured upstream error fields from an error.
// If the error wraps *core.GatewayError, returns upstream_status, upstream_provider, upstream_body.
// Otherwise returns nil (no extra log fields).
func extractUpstreamErrorAttrs(err error) []any {
	var gwErr *core.GatewayError
	if errors.As(err, &gwErr) {
		return []any{
			"upstream_status", gwErr.StatusCode,
			"upstream_provider", gwErr.Provider,
			"upstream_body", gwErr.Message,
		}
	}
	return nil
}

// upstreamStatus 提取错误中的上游 HTTP 状态码，供写入 gin context 供 call_logs 记录。
// 非 *core.GatewayError 时返回 0（无上游 HTTP 状态）。
func upstreamStatus(err error) int {
	var gwErr *core.GatewayError
	if errors.As(err, &gwErr) {
		return gwErr.StatusCode
	}
	return 0
}
