package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetDashboardSettings_DefaultHasCardFlags 默认看板设置应包含所有卡片可见性开关
// （B02/WP2），否则前端 OpsDashboardPage 的 v-if 会导致 4 张卡片在 settings 加载后消失。
func TestGetDashboardSettings_DefaultHasCardFlags(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/settings/dashboard")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// 既有字段不丢
	assert.Contains(t, resp, "default_time_range")
	assert.Contains(t, resp, "refresh_interval")
	// B02 新增卡片可见性开关（默认 true）
	for _, key := range []string{
		"show_concurrency_card", "show_switch_rate_chart",
		"show_token_stats", "show_alert_events", "show_system_metrics",
	} {
		v, ok := resp[key]
		require.True(t, ok, "missing field: %s", key)
		assert.Equal(t, true, v, "%s should default to true", key)
	}
	// 自动刷新间隔 + 查询模式
	assert.Contains(t, resp, "auto_refresh_interval")
	assert.Contains(t, resp, "default_query_mode")
}

// TestUpdateDashboardSettings_PreserveCardFlags 更新部分字段后，缺失的卡片开关应由默认值补全
func TestUpdateDashboardSettings_PreserveCardFlags(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	// 仅更新 refresh_interval，卡片开关应保留默认 true
	body := `{"refresh_interval":15}`
	w := doRequest(r, "PUT", "/ops/settings/dashboard", body)
	assertStatus(t, w, 200)

	w2 := doRequest(r, "GET", "/ops/settings/dashboard")
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["show_concurrency_card"])
	assert.Equal(t, true, resp["show_token_stats"])
	assert.InDelta(t, 15.0, resp["refresh_interval"], 0.1)
}
