package orchestrator

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

func TestUpstreamStatus(t *testing.T) {
	// 直接 *core.GatewayError
	gwErr := core.ParseProviderError("openai", http.StatusTooManyRequests,
		[]byte(`{"error":{"message":"rate limit exceeded"}}`), nil)
	if got := upstreamStatus(gwErr); got != http.StatusTooManyRequests {
		t.Errorf("upstreamStatus(GatewayError) = %d, want %d", got, http.StatusTooManyRequests)
	}

	// %w 包装后的 GatewayError
	wrapped := fmt.Errorf("upstream call failed: %w", gwErr)
	if got := upstreamStatus(wrapped); got != http.StatusTooManyRequests {
		t.Errorf("upstreamStatus(wrapped) = %d, want %d", got, http.StatusTooManyRequests)
	}

	// 非 GatewayError
	if got := upstreamStatus(errors.New("boom")); got != 0 {
		t.Errorf("upstreamStatus(plain) = %d, want 0", got)
	}

	// nil
	if got := upstreamStatus(nil); got != 0 {
		t.Errorf("upstreamStatus(nil) = %d, want 0", got)
	}
}
