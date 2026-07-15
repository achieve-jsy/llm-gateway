package errormap

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

func TestMapProviderError_UpstreamInfoPreserved(t *testing.T) {
	tests := []struct {
		name         string
		inputErr     error
		wantCode     string
		wantHTTP     int
		wantUpstream bool // 是否应该有 UpstreamInfo
	}{
		{
			name:         "GatewayError with upstream timeout",
			inputErr:     core.NewProviderError("deepseek", http.StatusGatewayTimeout, "timeout awaiting response headers", nil),
			wantCode:     "upstream_timeout",
			wantHTTP:     http.StatusGatewayTimeout,
			wantUpstream: true,
		},
		{
			name:         "GatewayError with upstream 429",
			inputErr:     core.NewProviderError("openai", http.StatusTooManyRequests, "rate limit exceeded", nil),
			wantCode:     "provider_rate_limited",
			wantHTTP:     http.StatusBadGateway,
			wantUpstream: true,
		},
		{
			name:         "GatewayError with upstream 500",
			inputErr:     core.NewProviderError("azure", http.StatusInternalServerError, "internal server error", nil),
			wantCode:     "upstream_error",
			wantHTTP:     http.StatusBadGateway,
			wantUpstream: true,
		},
		{
			name: "context.DeadlineExceeded",
			inputErr: func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 0)
				defer cancel()
				<-ctx.Done()
				return ctx.Err()
			}(),
			wantCode:     "provider_timeout",
			wantHTTP:     http.StatusGatewayTimeout,
			wantUpstream: true,
		},
		{
			name: "context.Canceled",
			inputErr: func() error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				<-ctx.Done()
				return ctx.Err()
			}(),
			wantCode:     "request_canceled",
			wantHTTP:     499,
			wantUpstream: true,
		},
		{
			name:         "plain error falls back to provider_error",
			inputErr:     errors.New("some random error"),
			wantCode:     "provider_error",
			wantHTTP:     http.StatusBadGateway,
			wantUpstream: false, // 非 GatewayError, 无 upstream
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ue := MapProviderError(tt.inputErr)
			if ue == nil {
				t.Fatal("MapProviderError returned nil")
			}
			if ue.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", ue.Code, tt.wantCode)
			}
			if ue.HTTPStatus != tt.wantHTTP {
				t.Errorf("HTTPStatus = %d, want %d", ue.HTTPStatus, tt.wantHTTP)
			}
			if tt.wantUpstream && ue.Upstream == nil {
				t.Errorf("Upstream is nil, expected non-nil UpstreamInfo")
			}
			if !tt.wantUpstream && ue.Upstream != nil {
				t.Errorf("Upstream is not nil, expected nil UpstreamInfo")
			}
			if ue.Upstream != nil {
				if ue.Upstream.Body == nil && ue.Upstream.Provider == "" && ue.Upstream.Status == 0 {
					t.Errorf("Upstream has no meaningful data: %+v", ue.Upstream)
				}
			}
		})
	}
}

func TestMapProviderError_UpstreamContent(t *testing.T) {
	// 验证上游错误内容完整透传
	gwErr := core.NewProviderError("deepseek", http.StatusGatewayTimeout, "upstream request timeout", nil)
	ue := MapProviderError(gwErr)

	if ue.Upstream == nil {
		t.Fatal("Upstream is nil")
	}
	if ue.Upstream.Provider != "deepseek" {
		t.Errorf("upstream provider = %q, want %q", ue.Upstream.Provider, "deepseek")
	}
	if ue.Upstream.Status != http.StatusGatewayTimeout {
		t.Errorf("upstream status = %d, want %d", ue.Upstream.Status, http.StatusGatewayTimeout)
	}
	// Body 应该是 JSON 解析后的 map
	bodyMap, ok := ue.Upstream.Body.(map[string]any)
	if !ok {
		t.Fatalf("upstream body is not map[string]any, got %T: %v", ue.Upstream.Body, ue.Upstream.Body)
	}
	errObj, ok := bodyMap["error"].(map[string]any)
	if !ok {
		t.Fatalf("upstream body.error is not map[string]any, got %T", bodyMap["error"])
	}
	if errObj["message"] != "upstream request timeout" {
		t.Errorf("upstream body.error.message = %q, want %q", errObj["message"], "upstream request timeout")
	}
}

func TestMapProviderError_RawUpstreamBodyPreserved(t *testing.T) {
	// ParseProviderError 构造的错误携带原始 body 时，upstream.body 应忠实呈现
	// 上游真实结构（含 code/type/param），而非合成假体。
	raw := `{"error":{"message":"Incorrect API key provided: sk-abc123","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`
	gwErr := core.ParseProviderError("openai", http.StatusUnauthorized, []byte(raw), nil)

	ue := MapProviderError(gwErr)
	if ue == nil {
		t.Fatal("MapProviderError returned nil")
	}
	// 映射仍应正确识别为认证错误（用重建 body，而非原始 type）
	if ue.Code != "provider_auth_error" {
		t.Errorf("code = %q, want %q", ue.Code, "provider_auth_error")
	}
	if ue.Upstream == nil {
		t.Fatal("Upstream is nil")
	}
	bodyMap, ok := ue.Upstream.Body.(map[string]any)
	if !ok {
		t.Fatalf("upstream body is not map[string]any, got %T: %v", ue.Upstream.Body, ue.Upstream.Body)
	}
	errObj, ok := bodyMap["error"].(map[string]any)
	if !ok {
		t.Fatalf("upstream body.error is not map[string]any, got %T", bodyMap["error"])
	}
	// 原始 code/type 必须保留
	if errObj["code"] != "invalid_api_key" {
		t.Errorf("upstream body.error.code = %v, want %q", errObj["code"], "invalid_api_key")
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("upstream body.error.type = %v, want %q", errObj["type"], "invalid_request_error")
	}
	if errObj["message"] != "Incorrect API key provided: sk-abc123" {
		t.Errorf("upstream body.error.message = %v, want %q", errObj["message"], "Incorrect API key provided: sk-abc123")
	}
}

func TestMapProviderError_RawNonJSONBodyPreserved(t *testing.T) {
	// 非 JSON body 应作为原始字符串透传
	gwErr := core.ParseProviderError("custom", http.StatusBadGateway, []byte("Bad Gateway: upstream crashed"), nil)
	ue := MapProviderError(gwErr)
	if ue == nil {
		t.Fatal("MapProviderError returned nil")
	}
	if ue.Upstream == nil {
		t.Fatal("Upstream is nil")
	}
	if body, ok := ue.Upstream.Body.(string); !ok || body != "Bad Gateway: upstream crashed" {
		t.Errorf("upstream body = %#v, want string %q", ue.Upstream.Body, "Bad Gateway: upstream crashed")
	}
}
func TestMapProviderError_RawBodyAvailableForPassthrough(t *testing.T) {
	raw := []byte(`{"error":{"type":"server_error","message":"temporarily overloaded","code":"capacity_exceeded"}}`)
	gwErr := core.ParseProviderError("bedrock", http.StatusServiceUnavailable, raw, nil)

	ue := MapProviderError(gwErr)
	if ue.Upstream == nil {
		t.Fatal("Upstream is nil")
	}
	if ue.Upstream.Status != http.StatusServiceUnavailable {
		t.Fatalf("upstream status = %d, want %d", ue.Upstream.Status, http.StatusServiceUnavailable)
	}
	if string(ue.Upstream.RawBody) != string(raw) {
		t.Fatalf("raw upstream body = %q, want %q", ue.Upstream.RawBody, raw)
	}
}
