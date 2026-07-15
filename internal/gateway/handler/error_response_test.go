package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gatewayerrors "github.com/example/llm-gateway/internal/errors"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ─── respondError 端到端测试 ────────────────────────────────────────────────

func TestRespondError_WithoutUpstream_BackwardCompatible(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	ue := model.ErrProviderTimeout
	respondError(c, ue)

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)

	errObj := body["error"].(map[string]any)
	assert.Equal(t, "temporary_failure", errObj["type"])
	assert.Equal(t, "provider_timeout", errObj["code"])
	// 没有 Upstream 时不应出现 upstream 字段
	_, hasUpstream := errObj["upstream"]
	assert.False(t, hasUpstream, "upstream field should not exist when Upstream is nil")
}

func TestRespondError_WithUpstream_IncludesUpstreamInResponse(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	ue := &model.UnifiedError{
		Type:       model.ErrorTypeTempFailure,
		Code:       "upstream_timeout",
		Message:    "upstream provider did not respond in time",
		HTTPStatus: http.StatusGatewayTimeout,
		Upstream: &gatewayerrors.UpstreamInfo{
			Provider: "deepseek",
			Status:   504,
			Body: map[string]any{
				"error": map[string]any{
					"type":    "server_error",
					"message": "upstream request timeout",
				},
			},
		},
	}
	respondError(c, ue)

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)

	errObj := body["error"].(map[string]any)
	assert.Equal(t, "upstream_timeout", errObj["code"])

	// 验证 upstream 字段存在且内容正确
	upstream, hasUpstream := errObj["upstream"]
	require.True(t, hasUpstream, "upstream field must exist when Upstream is set")
	upstreamMap := upstream.(map[string]any)
	assert.Equal(t, "deepseek", upstreamMap["provider"])
	assert.Equal(t, float64(504), upstreamMap["status"])

	upBody := upstreamMap["body"].(map[string]any)
	upErr := upBody["error"].(map[string]any)
	assert.Equal(t, "upstream request timeout", upErr["message"])
}

func TestRespondError_WithUpstream_429RateLimit(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	ue := &model.UnifiedError{
		Type:       model.ErrorTypeTempFailure,
		Code:       "provider_rate_limited",
		Message:    "rate limit exceeded",
		HTTPStatus: http.StatusBadGateway,
		Upstream: &gatewayerrors.UpstreamInfo{
			Provider: "openai",
			Status:   429,
			Body: map[string]any{
				"error": map[string]any{
					"type":    "rate_limit_error",
					"message": "You exceeded your current quota",
				},
			},
		},
	}
	respondError(c, ue)

	assert.Equal(t, http.StatusBadGateway, w.Code)

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	errObj := body["error"].(map[string]any)
	upstream := errObj["upstream"].(map[string]any)

	assert.Equal(t, "openai", upstream["provider"])
	assert.Equal(t, float64(429), upstream["status"])
}

// ─── writePassthroughError 端到端测试 ────────────────────────────────────────

func TestWritePassthroughError_IncludesCode(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Error-Format", "passthrough")

	ue := model.ErrProviderTimeout
	respondError(c, ue)

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)

	errObj := body["error"].(map[string]any)
	// passthrough 模式现在应该有 code 字段
	code, hasCode := errObj["code"]
	assert.True(t, hasCode, "passthrough error should have code field")
	assert.Equal(t, "provider_timeout", code)
}

// ─── respondAnthropicError 端到端测试 ────────────────────────────────────────

func TestRespondAnthropicError_WithUpstream(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)

	h := &MessagesHandler{}
	h.respondAnthropicError(c, http.StatusBadGateway, "overloaded_error",
		"upstream_timeout", "upstream provider timeout",
		&gatewayerrors.UpstreamInfo{
			Provider: "anthropic",
			Status:   529,
			Body:     "overloaded",
		},
	)

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)

	assert.Equal(t, "error", body["type"])

	errObj := body["error"].(map[string]any)
	assert.Equal(t, "overloaded_error", errObj["type"])
	assert.Equal(t, "upstream_timeout", errObj["code"])

	upstream, hasUpstream := errObj["upstream"]
	require.True(t, hasUpstream)
	upstreamMap := upstream.(map[string]any)
	assert.Equal(t, "anthropic", upstreamMap["provider"])
	assert.Equal(t, float64(529), upstreamMap["status"])
}

func TestRespondAnthropicError_WithoutUpstream_NoUpstreamField(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)

	h := &MessagesHandler{}
	h.respondAnthropicError(c, http.StatusBadRequest, "invalid_request",
		"invalid_request_body", "request body is invalid", nil)

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)

	errObj := body["error"].(map[string]any)
	_, hasUpstream := errObj["upstream"]
	assert.False(t, hasUpstream, "upstream should not exist when nil")
}

// ─── respondAnthropicUnifiedError 端到端测试 ─────────────────────────────────

func TestRespondAnthropicUnifiedError_WithUpstream(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)

	h := &MessagesHandler{}
	ue := &model.UnifiedError{
		Type:       model.ErrorTypeTempFailure,
		Code:       "upstream_timeout",
		Message:    "upstream provider did not respond in time",
		HTTPStatus: http.StatusGatewayTimeout,
		Upstream: &gatewayerrors.UpstreamInfo{
			Provider: "deepseek",
			Status:   504,
			Body:     "timeout awaiting response",
		},
	}
	h.respondAnthropicUnifiedError(c, ue)

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	errObj := body["error"].(map[string]any)

	upstream, hasUpstream := errObj["upstream"]
	require.True(t, hasUpstream)
	upstreamMap := upstream.(map[string]any)
	assert.Equal(t, "deepseek", upstreamMap["provider"])
	assert.Equal(t, float64(504), upstreamMap["status"])
}

func TestRespondError_PassthroughPreservesUpstreamStatusAndBody(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Error-Format", "passthrough")

	raw := []byte(`{"error":{"message":"upstream overloaded","type":"server_error","code":"capacity_exceeded"}}`)
	ue := &model.UnifiedError{
		Type:       model.ErrorTypeTempFailure,
		Code:       "upstream_unavailable",
		Message:    "上游服务不可用",
		HTTPStatus: http.StatusServiceUnavailable,
		Upstream: &gatewayerrors.UpstreamInfo{
			Provider: "openai",
			Status:   http.StatusServiceUnavailable,
			RawBody:  raw,
		},
	}

	respondError(c, ue)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, string(raw), w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

func TestRespondError_PassthroughUsesUpstreamStatusNotMappedStatus(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Error-Format", "passthrough")

	raw := []byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`)
	ue := &model.UnifiedError{
		Type:       model.ErrorTypeTempFailure,
		Code:       "provider_rate_limited",
		Message:    "上游服务触发限流",
		HTTPStatus: http.StatusBadGateway, // normalized response status
		Upstream: &gatewayerrors.UpstreamInfo{
			Provider: "openai",
			Status:   http.StatusTooManyRequests, // provider response status
			RawBody:  raw,
		},
	}

	respondError(c, ue)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, string(raw), w.Body.String())
}
