package handler_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// GetRequestErrorDetail
// =============================================================================

// TestGetRequestErrorDetail_Success 获取存在的客户端错误应返回 200
func TestGetRequestErrorDetail_Success(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	require.NoError(t, db.Exec(`
		INSERT INTO call_logs (request_id, ts, key_id, user_id, model, group_name, provider,
			status, duration_ms, first_token_ms, total_tokens, prompt_tokens, completion_tokens,
			upstream_status, ip_address, failover_used, routed_via_group)
		VALUES ('req-4xx-001', '2025-06-01T10:00:00Z', 'key-1', 'user-1', 'gpt-4', 'default',
			'openai', 429, 120, 80, 500, 300, 200, 429, '10.0.0.1', 0, 1)
	`).Error)

	w := doRequest(r, "GET", "/ops/request-errors/req-4xx-001")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "req-4xx-001", resp["request_id"])
	assert.Equal(t, "openai", resp["provider"])

	lb, ok := resp["latency_breakdown"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(120), lb["upstream_ms"])
	assert.Equal(t, float64(80), lb["ttft_ms"])

	uc, ok := resp["upstream_context"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(429), uc["upstream_status_code"])
}

// TestGetRequestErrorDetail_NotFound 不存在的请求应返回 404
func TestGetRequestErrorDetail_NotFound(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/request-errors/nonexistent-id")
	assertStatus(t, w, 404)
}

// TestGetRequestErrorDetail_ServerError 5xx 错误不会出现在 4xx 端点
func TestGetRequestErrorDetail_ServerError(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	require.NoError(t, db.Exec(`
		INSERT INTO call_logs (request_id, ts, key_id, user_id, model, group_name, provider,
			status, duration_ms, first_token_ms, total_tokens, prompt_tokens, completion_tokens,
			upstream_status, ip_address, failover_used, routed_via_group)
		VALUES ('req-5xx-001', '2025-06-01T10:00:00Z', 'key-1', 'user-1', 'gpt-4', 'default',
			'openai', 500, 500, 200, 100, 50, 50, 500, '10.0.0.1', 0, 0)
	`).Error)

	w := doRequest(r, "GET", "/ops/request-errors/req-5xx-001")
	assertStatus(t, w, 404)
}

// =============================================================================
// GetUpstreamErrorDetail
// =============================================================================

// TestGetUpstreamErrorDetail_Success 获取存在的上游错误应返回 200
func TestGetUpstreamErrorDetail_Success(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	require.NoError(t, db.Exec(`
		INSERT INTO call_logs (request_id, ts, key_id, user_id, model, group_name, provider,
			status, duration_ms, first_token_ms, total_tokens, prompt_tokens, completion_tokens,
			upstream_status, ip_address, failover_used, routed_via_group)
		VALUES ('req-500-001', '2025-06-01T10:00:00Z', 'key-1', 'user-1', 'gpt-4', 'default',
			'openai', 502, 2000, 1500, 200, 100, 100, 502, '10.0.0.2', 1, 0)
	`).Error)

	w := doRequest(r, "GET", "/ops/upstream-errors/req-500-001")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "req-500-001", resp["request_id"])
	assert.Equal(t, float64(502), resp["status"])

	uc, ok := resp["upstream_context"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(502), uc["upstream_status_code"])
}

// TestGetUpstreamErrorDetail_NotFound 不存在时应返回 404
func TestGetUpstreamErrorDetail_NotFound(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/upstream-errors/nonexistent-id")
	assertStatus(t, w, 404)
}

// TestGetUpstreamErrorDetail_ClientError 4xx 不会出现在 5xx 端点
func TestGetUpstreamErrorDetail_ClientError(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	require.NoError(t, db.Exec(`
		INSERT INTO call_logs (request_id, ts, key_id, user_id, model, group_name, provider,
			status, duration_ms, first_token_ms, total_tokens, prompt_tokens, completion_tokens,
			upstream_status, ip_address, failover_used, routed_via_group)
		VALUES ('req-429-001', '2025-06-01T10:00:00Z', 'key-1', 'user-1', 'gpt-4', 'default',
			'openai', 429, 100, 50, 100, 50, 50, 429, '10.0.0.1', 0, 0)
	`).Error)

	w := doRequest(r, "GET", "/ops/upstream-errors/req-429-001")
	assertStatus(t, w, 404)
}

// =============================================================================
// GetAccountAvailability
// =============================================================================

// TestGetAccountAvailability_Empty 无数据时返回空列表
func TestGetAccountAvailability_Empty(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/account-availability")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["total"])
}

// TestGetAccountAvailability_WithData Redis 中有数据时返回正确解析
func TestGetAccountAvailability_WithData(t *testing.T) {
	r, _, rdb := setupOpsHandler(t)

	rdb.Set(context.Background(), "ratelimit:provider:openai", `{"is_rate_limited":true,"rate_limit_remaining":0,"rate_limit_reset_seconds":30,"error_count":5}`, 0)
	rdb.Set(context.Background(), "ratelimit:provider:anthropic", `{"is_rate_limited":false,"rate_limit_remaining":100,"rate_limit_reset_seconds":0,"error_count":0}`, 0)

	w := doRequest(r, "GET", "/ops/account-availability")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(2), resp["total"])

	items, ok := resp["items"].([]interface{})
	require.True(t, ok)

	found := map[string]bool{"openai": false, "anthropic": false}
	for _, item := range items {
		m := item.(map[string]interface{})
		pid := m["provider_id"].(string)
		found[pid] = true
	}

	for pid, f := range found {
		assert.True(t, f, "missing provider_id: "+pid)
	}
}

// TestGetAccountAvailability_FallbackCount Redis 值无法 JSON 解析时回退到简单计数
func TestGetAccountAvailability_FallbackCount(t *testing.T) {
	r, _, rdb := setupOpsHandler(t)

	rdb.Set(context.Background(), "ratelimit:provider:bedrock", "42", 0)

	w := doRequest(r, "GET", "/ops/account-availability")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(1), resp["total"])

	items := resp["items"].([]interface{})
	m := items[0].(map[string]interface{})
	assert.Equal(t, "bedrock", m["provider_id"])
	assert.Equal(t, float64(42), m["error_count"])
}

// =============================================================================
// GetAdvancedSettings / UpdateAdvancedSettings
// =============================================================================

// TestGetAdvancedSettings_Default 未设置时返回默认值
func TestGetAdvancedSettings_Default(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/settings/advanced")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(30), resp["data_retention_days"])
	assert.Equal(t, float64(90), resp["error_log_retention_days"])
	assert.Equal(t, false, resp["ignore_context_canceled"])
	assert.Equal(t, false, resp["ignore_429_errors"])
	assert.Equal(t, true, resp["auto_refresh_enabled"])
}

// TestUpdateAdvancedSettings_Success 更新高级设置应成功
func TestUpdateAdvancedSettings_Success(t *testing.T) {
	r, _, _ := setupOpsHandler(t)

	body := `{"data_retention_days":60,"error_log_retention_days":180,"ignore_context_canceled":true,"ignore_429_errors":true}`
	w := doRequest(r, "PUT", "/ops/settings/advanced", body)
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(60), resp["data_retention_days"])
	assert.Equal(t, float64(180), resp["error_log_retention_days"])
	assert.Equal(t, true, resp["ignore_context_canceled"])
	assert.Equal(t, true, resp["ignore_429_errors"])
}

// TestUpdateAdvancedSettings_InvalidJSON 无效 JSON 返回 400
func TestUpdateAdvancedSettings_InvalidJSON(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "PUT", "/ops/settings/advanced", `{bad`)
	assertStatus(t, w, 400)
}

// TestUpdateAdvancedSettings_Persistence 更新后重新获取应返回更新的值
func TestUpdateAdvancedSettings_Persistence(t *testing.T) {
	r, _, _ := setupOpsHandler(t)

	body := `{"data_retention_days":15,"error_log_retention_days":30}`
	w := doRequest(r, "PUT", "/ops/settings/advanced", body)
	assertStatus(t, w, 200)

	w2 := doRequest(r, "GET", "/ops/settings/advanced")
	assertStatus(t, w2, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Equal(t, float64(15), resp["data_retention_days"])
	assert.Equal(t, float64(30), resp["error_log_retention_days"])
}

// TestGetAdvancedSettings_DefaultMerge 部分字段为 0 时与默认值合并
func TestGetAdvancedSettings_DefaultMerge(t *testing.T) {
	r, _, _ := setupOpsHandler(t)

	body := `{"data_retention_days":7}`
	doRequest(r, "PUT", "/ops/settings/advanced", body)

	w := doRequest(r, "GET", "/ops/settings/advanced")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(7), resp["data_retention_days"])
	assert.Equal(t, float64(90), resp["error_log_retention_days"])
}