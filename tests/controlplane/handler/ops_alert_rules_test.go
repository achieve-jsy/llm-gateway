package handler_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func uid(id uint) string { return fmt.Sprintf("%d", id) }

// TestCreateAlertRule_Success 创建告警规则应返回 200
func TestCreateAlertRule_Success(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	body := `{"name":"高错误率告警","description":"错误率超过10%","enabled":true,"metric_type":"error_rate","operator":">","threshold":0.1,"window_minutes":5,"sustained_minutes":2,"severity":"P1","cooldown_minutes":10,"notify_email":true}`

	w := doRequest(r, "POST", "/ops/alert-rules", body)
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "高错误率告警", resp["name"])
	assert.Equal(t, "error_rate", resp["metric_type"])
	assert.Equal(t, "P1", resp["severity"])
	assert.NotZero(t, resp["id"])
}

// TestCreateAlertRule_InvalidJSON 无效 JSON 应返回 400
func TestCreateAlertRule_InvalidJSON(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "POST", "/ops/alert-rules", `{invalid`)
	assertStatus(t, w, 400)
}

// TestListAlertRules_Empty 空列表应返回空数组
func TestListAlertRules_Empty(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/alert-rules")
	assertStatus(t, w, 200)

	var resp struct {
		Items []interface{} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items)
}

// TestListAlertRules_WithData 有数据时返回列表并按 ID 倒序
func TestListAlertRules_WithData(t *testing.T) {
	r, db, _ := setupOpsHandler(t)
	rules := []model.AlertRule{
		{Name: "规则A", Description: "desc", MetricType: "error_rate", Operator: ">", Threshold: 0.1, WindowMinutes: 1, SustainedMinutes: 2, Severity: "P1", CooldownMinutes: 10, NotifyEmail: true, Enabled: true, Filters: datatypes.JSON("{}")},
		{Name: "规则B", Description: "desc", MetricType: "cpu_usage_percent", Operator: ">", Threshold: 80, WindowMinutes: 5, SustainedMinutes: 3, Severity: "P2", CooldownMinutes: 5, NotifyEmail: false, Enabled: false, Filters: datatypes.JSON("{}")},
	}
	for i := range rules {
		require.NoError(t, db.Create(&rules[i]).Error)
	}

	w := doRequest(r, "GET", "/ops/alert-rules")
	assertStatus(t, w, 200)

	var resp struct {
		Items []model.AlertRule `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 2)
	assert.Equal(t, "规则B", resp.Items[0].Name)
	assert.Equal(t, "规则A", resp.Items[1].Name)
}

// TestUpdateAlertRule_Success 更新规则应成功
func TestUpdateAlertRule_Success(t *testing.T) {
	r, db, _ := setupOpsHandler(t)
	rule := model.AlertRule{
		Name: "旧名称", Description: "desc", MetricType: "error_rate", Operator: ">",
		Threshold: 0.1, WindowMinutes: 1, SustainedMinutes: 2, Severity: "P1",
		CooldownMinutes: 10, NotifyEmail: true, Enabled: true, Filters: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&rule).Error)

	body := `{"name":"新名称","description":"updated","enabled":false,"metric_type":"error_rate","operator":"<","threshold":0.05,"window_minutes":5,"sustained_minutes":3,"severity":"P0","cooldown_minutes":15,"notify_email":false}`
	w := doRequest(r, "PUT", "/ops/alert-rules/"+uid(rule.ID), body)
	assertStatus(t, w, 200)

	var resp model.AlertRule
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "新名称", resp.Name)
	assert.False(t, resp.Enabled)
	assert.Equal(t, "P0", resp.Severity)
	assert.InDelta(t, 0.05, resp.Threshold, 0.001)
}

// TestUpdateAlertRule_NotFound 更新不存在的规则应返回 404
func TestUpdateAlertRule_NotFound(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "PUT", "/ops/alert-rules/99999", `{"name":"x"}`)
	assertStatus(t, w, 404)
}

// TestUpdateAlertRule_InvalidID 无效 ID 应返回 400
func TestUpdateAlertRule_InvalidID(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "PUT", "/ops/alert-rules/abc", `{"name":"x"}`)
	assertStatus(t, w, 400)
}

// TestDeleteAlertRule_Success 删除规则应成功
func TestDeleteAlertRule_Success(t *testing.T) {
	r, db, _ := setupOpsHandler(t)
	rule := model.AlertRule{
		Name: "待删除", Description: "desc", MetricType: "error_rate", Operator: ">",
		Threshold: 0.1, WindowMinutes: 1, SustainedMinutes: 2, Severity: "P1",
		CooldownMinutes: 10, NotifyEmail: true, Enabled: true, Filters: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&rule).Error)

	w := doRequest(r, "DELETE", "/ops/alert-rules/"+uid(rule.ID))
	assertStatus(t, w, 200)

	w2 := doRequest(r, "GET", "/ops/alert-rules")
	var resp struct {
		Items []interface{} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items)
}

// TestDeleteAlertRule_NotFound 删除不存在的规则应返回 404
func TestDeleteAlertRule_NotFound(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "DELETE", "/ops/alert-rules/99999")
	assertStatus(t, w, 404)
}

// TestDeleteAlertRule_InvalidID 无效 ID 应返回 400
func TestDeleteAlertRule_InvalidID(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "DELETE", "/ops/alert-rules/xyz")
	assertStatus(t, w, 400)
}

// TestAlertRuleCRUD_FullCycle 完整 CRUD 生命周期
func TestAlertRuleCRUD_FullCycle(t *testing.T) {
	r, _, _ := setupOpsHandler(t)

	// 1. 创建
	w := doRequest(r, "POST", "/ops/alert-rules", `{"name":"完整测试","description":"CRUD","enabled":true,"metric_type":"success_rate","operator":"<","threshold":99,"window_minutes":1,"sustained_minutes":2,"severity":"P2","cooldown_minutes":5,"notify_email":true}`)
	assertStatus(t, w, 200)
	var created model.AlertRule
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotZero(t, created.ID)

	// 2. 列表包含
	w = doRequest(r, "GET", "/ops/alert-rules")
	assertStatus(t, w, 200)
	var listResp struct {
		Items []model.AlertRule `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	assert.Len(t, listResp.Items, 1)

	// 3. 更新
	w = doRequest(r, "PUT", "/ops/alert-rules/"+uid(created.ID), `{"name":"已更新","description":"updated","enabled":false,"metric_type":"success_rate","operator":"<","threshold":98,"window_minutes":5,"sustained_minutes":5,"severity":"P1","cooldown_minutes":10,"notify_email":false}`)
	assertStatus(t, w, 200)

	// 4. 删除
	w = doRequest(r, "DELETE", "/ops/alert-rules/"+uid(created.ID))
	assertStatus(t, w, 200)

	// 5. 验证空
	w = doRequest(r, "GET", "/ops/alert-rules")
	assertStatus(t, w, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	assert.Empty(t, listResp.Items)
}
