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