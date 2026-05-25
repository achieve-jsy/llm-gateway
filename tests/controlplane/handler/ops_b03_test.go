package handler_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetConcurrencyStats_EmptyRedisFieldShape 空数据时响应应含前端契约全部聚合字段（B03）
func TestGetConcurrencyStats_EmptyRedisFieldShape(t *testing.T) {
	r, _, _ := setupOpsHandlerAll(t)
	w := doRequest(r, "GET", "/ops/concurrency")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	for _, key := range []string{
		"providers", "total_providers", "available_providers",
		"rate_limited_providers", "error_providers",
		"total_active_requests", "total_waiting_queue",
	} {
		assert.Contains(t, resp, key, "missing aggregate field: %s", key)
	}
	providers, ok := resp["providers"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, providers)
}

// TestGetConcurrencyStats_WithRedisProvider Redis 有 provider 并发数据时，每项应含前端契约字段（B03）
func TestGetConcurrencyStats_WithRedisProvider(t *testing.T) {
	r, _, rdb := setupOpsHandlerAll(t)
	ctx := context.Background()
	rdb.Set(ctx, "concurrency:provider:openai", "5", 0)
	rdb.Set(ctx, "ratelimit:provider:openai", `{"max_capacity":10}`, 0)

	w := doRequest(r, "GET", "/ops/concurrency")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	providers := resp["providers"].([]interface{})
	require.Len(t, providers, 1)
	p := providers[0].(map[string]interface{})
	assert.Equal(t, "openai", p["provider_id"])
	assert.Equal(t, "openai", p["provider_name"])
	assert.Equal(t, float64(10), p["max_capacity"])
	assert.Equal(t, float64(5), p["active_requests"])
	assert.InDelta(t, 50.0, p["current_load"], 0.1)
	assert.Equal(t, "available", p["status"])
	assert.Equal(t, float64(1), resp["total_providers"])
	assert.Equal(t, float64(5), resp["total_active_requests"])
	// 无 call_logs 时质量字段应为 null（不造假 0%）
	assert.Contains(t, p, "success_rate_5min")
	assert.Contains(t, p, "duration_p95_ms")
	assert.Nil(t, p["success_rate_5min"])
	assert.Nil(t, p["duration_p95_ms"])
}
