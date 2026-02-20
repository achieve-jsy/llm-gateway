package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetRuntimeLogConfig_DefaultHasFullFields 默认运行时日志配置应包含前端期望的
// retention_days / enable_caller / enable_stacktrace 等字段（B06/WP2）。
func TestGetRuntimeLogConfig_DefaultHasFullFields(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/runtime/logging")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "info", resp["level"])
	for _, key := range []string{
		"retention_days", "enable_caller", "enable_stacktrace",
		"stacktrace_level", "sampling_initial", "sampling_thereafter",
		"slow_request_ms", "max_body_bytes",
	} {
		assert.Contains(t, resp, key, "missing field: %s", key)
	}
	assert.Equal(t, true, resp["enable_caller"])
	assert.Equal(t, false, resp["enable_stacktrace"])
}

// TestCleanupSystemLogs_BodyRetentionDays 前端发 body JSON {retention_days} 应被接住（B06）
func TestCleanupSystemLogs_BodyRetentionDays(t *testing.T) {
	r, db, _ := setupOpsHandler(t)
	// seed 一条 system_log（ts 设为很久以前，确保被 7 天阈值覆盖）
	require.NoError(t, db.Exec(`INSERT INTO system_logs (ts, level, component, message) VALUES ('2020-01-01T00:00:00Z','INFO','gateway','old')`).Error)

	w := doRequest(r, "POST", "/ops/system-logs/cleanup", `{"retention_days":7}`)
	assertStatus(t, w, 200)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "deleted")
	assert.Equal(t, float64(7), resp["older_than_days"])
}

// TestCleanupSystemLogs_QueryDaysFallback 旧客户端发 ?days= 仍可工作（兼容回退）
func TestCleanupSystemLogs_QueryDaysFallback(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	// 无 body，走 query 回退
	w := doRequest(r, "POST", "/ops/system-logs/cleanup?days=5", "")
	assertStatus(t, w, 200)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(5), resp["older_than_days"])
}
