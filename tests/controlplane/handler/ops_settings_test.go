package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetMetricThresholds_Default 无配置时返回默认阈值
func TestGetMetricThresholds_Default(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/settings/metric-thresholds")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "error_rate_warn")
	assert.Contains(t, resp, "error_rate_critical")
	assert.Contains(t, resp, "p95_ms_warn")
}

// TestUpdateMetricThresholds_Success 更新阈值应成功
func TestUpdateMetricThresholds_Success(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	body := `{"error_rate_warn":0.08,"error_rate_critical":0.15}`
	w := doRequest(r, "PUT", "/ops/settings/metric-thresholds", body)
	assertStatus(t, w, 200)

	// 重新读取验证
	w2 := doRequest(r, "GET", "/ops/settings/metric-thresholds")
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.InDelta(t, 0.08, resp["error_rate_warn"], 0.001)
	assert.InDelta(t, 0.15, resp["error_rate_critical"], 0.001)
	// 未更新的字段应保留默认值
	assert.Contains(t, resp, "p95_ms_warn")
}

// TestUpdateMetricThresholds_InvalidJSON 无效 JSON 返回 400
func TestUpdateMetricThresholds_InvalidJSON(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "PUT", "/ops/settings/metric-thresholds", `{bad`)
	assertStatus(t, w, 400)
}

// TestGetDashboardSettings_Default 无配置时返回默认看板设置
func TestGetDashboardSettings_Default(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/settings/dashboard")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "default_time_range")
	assert.Contains(t, resp, "refresh_interval")
}

// TestUpdateDashboardSettings_Success 更新看板设置应成功
func TestUpdateDashboardSettings_Success(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	body := `{"default_time_range":"6h","refresh_interval":60,"show_system_metrics":false}`
	w := doRequest(r, "PUT", "/ops/settings/dashboard", body)
	assertStatus(t, w, 200)

	// 重新读取验证
	w2 := doRequest(r, "GET", "/ops/settings/dashboard")
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Equal(t, "6h", resp["default_time_range"])
	assert.InDelta(t, 60.0, resp["refresh_interval"], 0.1)
	// 未更新的字段补默认值
	assert.Contains(t, resp, "compact_mode")
}

// TestUpdateDashboardSettings_InvalidJSON 无效 JSON 返回 400
func TestUpdateDashboardSettings_InvalidJSON(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "PUT", "/ops/settings/dashboard", `{bad`)
	assertStatus(t, w, 400)
}

// TestGetRuntimeLogConfig_Default 无配置时返回默认日志配置
func TestGetRuntimeLogConfig_Default(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/runtime/logging")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "info", resp["level"])
	assert.Contains(t, resp, "slow_request_ms")
}

// TestUpdateRuntimeLogConfig_Success 更新日志配置应成功
func TestUpdateRuntimeLogConfig_Success(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	body := `{"level":"debug","slow_request_ms":3000}`
	w := doRequest(r, "PUT", "/ops/runtime/logging", body)
	assertStatus(t, w, 200)

	w2 := doRequest(r, "GET", "/ops/runtime/logging")
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Equal(t, "debug", resp["level"])
	assert.InDelta(t, 3000.0, resp["slow_request_ms"], 0.1)
}

// TestUpdateRuntimeLogConfig_InvalidJSON 无效 JSON 返回 400
func TestUpdateRuntimeLogConfig_InvalidJSON(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "PUT", "/ops/runtime/logging", `{bad`)
	assertStatus(t, w, 400)
}

// TestResetRuntimeLogConfig_Success 重置日志配置应恢复默认值
func TestResetRuntimeLogConfig_Success(t *testing.T) {
	r, _, _ := setupOpsHandler(t)

	// 先更新
	doRequest(r, "PUT", "/ops/runtime/logging", `{"level":"debug","slow_request_ms":100}`)

	// 重置
	w := doRequest(r, "POST", "/ops/runtime/logging/reset")
	assertStatus(t, w, 200)

	// 验证恢复默认
	w2 := doRequest(r, "GET", "/ops/runtime/logging")
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Equal(t, "info", resp["level"])
}

// TestSettingsCRUD_FullCycle 阈值+看板设置完整 CRUD 生命周期
func TestSettingsCRUD_FullCycle(t *testing.T) {
	r, _, _ := setupOpsHandler(t)

	// 1. 读取默认阈值
	w := doRequest(r, "GET", "/ops/settings/metric-thresholds")
	assertStatus(t, w, 200)

	// 2. 更新阈值
	w = doRequest(r, "PUT", "/ops/settings/metric-thresholds", `{"error_rate_warn":0.03}`)
	assertStatus(t, w, 200)

	// 3. 读取默认看板设置
	w = doRequest(r, "GET", "/ops/settings/dashboard")
	assertStatus(t, w, 200)

	// 4. 更新看板设置
	w = doRequest(r, "PUT", "/ops/settings/dashboard", `{"refresh_interval":15}`)
	assertStatus(t, w, 200)

	// 5. 读取日志配置
	w = doRequest(r, "GET", "/ops/runtime/logging")
	assertStatus(t, w, 200)

	// 6. 更新日志配置
	w = doRequest(r, "PUT", "/ops/runtime/logging", `{"level":"warn"}`)
	assertStatus(t, w, 200)

	// 7. 重置日志配置
	w = doRequest(r, "POST", "/ops/runtime/logging/reset")
	assertStatus(t, w, 200)
}
