package handler_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// TestGetAlertEvent_Success 获取单条事件应返回 200
func TestGetAlertEvent_Success(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	rule := model.AlertRule{
		Name: "R", Description: "d", MetricType: "error_rate", Operator: ">",
		Threshold: 0.1, WindowMinutes: 1, SustainedMinutes: 2, Severity: "P1",
		CooldownMinutes: 10, Enabled: true, Filters: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&rule).Error)

	event := model.AlertEvent{
		RuleID: rule.ID, Severity: "P1", Status: "firing", Title: "详情事件",
		MetricValue: 0.2, ThresholdValue: 0.1, FiredAt: time.Now(),
		Description: "详细描述", Dimensions: datatypes.JSON(`{"platform":"openai"}`),
	}
	require.NoError(t, db.Create(&event).Error)

	w := doRequest(r, "GET", "/ops/alert-events/"+uid(event.ID))
	assertStatus(t, w, 200)

	var resp model.AlertEvent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "详情事件", resp.Title)
	assert.Equal(t, "详细描述", resp.Description)
}

// TestGetAlertEvent_NotFound 不存在的事件应返回 404
func TestGetAlertEvent_NotFound(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/alert-events/99999")
	assertStatus(t, w, 404)
}

// TestGetAlertEvent_InvalidID 无效 ID 应返回 400
func TestGetAlertEvent_InvalidID(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/alert-events/abc")
	assertStatus(t, w, 400)
}

// TestUpdateAlertEventStatus_Resolve 将状态更新为 resolved 应设置 resolved_at
func TestUpdateAlertEventStatus_Resolve(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	rule := model.AlertRule{
		Name: "R", Description: "d", MetricType: "error_rate", Operator: ">",
		Threshold: 0.1, WindowMinutes: 1, SustainedMinutes: 2, Severity: "P1",
		CooldownMinutes: 10, Enabled: true, Filters: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&rule).Error)

	event := model.AlertEvent{
		RuleID: rule.ID, Severity: "P1", Status: "firing", Title: "待恢复",
		MetricValue: 0.2, ThresholdValue: 0.1, FiredAt: time.Now(),
		Dimensions: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&event).Error)

	w := doRequest(r, "PUT", "/ops/alert-events/"+uid(event.ID)+"/status", `{"status":"resolved"}`)
	assertStatus(t, w, 200)

	var resp model.AlertEvent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "resolved", resp.Status)
	assert.NotNil(t, resp.ResolvedAt)
}

// TestUpdateAlertEventStatus_ManualResolve 手动恢复状态也应记录 resolved_at
func TestUpdateAlertEventStatus_ManualResolve(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	rule := model.AlertRule{
		Name: "R", Description: "d", MetricType: "error_rate", Operator: ">",
		Threshold: 0.1, WindowMinutes: 1, SustainedMinutes: 2, Severity: "P1",
		CooldownMinutes: 10, Enabled: true, Filters: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&rule).Error)

	event := model.AlertEvent{
		RuleID: rule.ID, Severity: "P1", Status: "firing", Title: "手动恢复",
		MetricValue: 0.2, ThresholdValue: 0.1, FiredAt: time.Now(),
		Dimensions: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&event).Error)

	w := doRequest(r, "PUT", "/ops/alert-events/"+uid(event.ID)+"/status", `{"status":"manual_resolved"}`)
	assertStatus(t, w, 200)

	var resp model.AlertEvent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "manual_resolved", resp.Status)
}

// TestUpdateAlertEventStatus_NotFound 不存在时返回 404
func TestUpdateAlertEventStatus_NotFound(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "PUT", "/ops/alert-events/99999/status", `{"status":"resolved"}`)
	assertStatus(t, w, 404)
}

// TestUpdateAlertEventStatus_InvalidID 无效 ID 返回 400
func TestUpdateAlertEventStatus_InvalidID(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "PUT", "/ops/alert-events/abc/status", `{"status":"resolved"}`)
	assertStatus(t, w, 400)
}

// TestUpdateAlertEventStatus_MissingStatus 缺少 status 字段返回 400
func TestUpdateAlertEventStatus_MissingStatus(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	rule := model.AlertRule{
		Name: "R", Description: "d", MetricType: "error_rate", Operator: ">",
		Threshold: 0.1, WindowMinutes: 1, SustainedMinutes: 2, Severity: "P1",
		CooldownMinutes: 10, Enabled: true, Filters: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&rule).Error)
	event := model.AlertEvent{
		RuleID: rule.ID, Severity: "P1", Status: "firing", Title: "t",
		MetricValue: 1, ThresholdValue: 0.1, FiredAt: time.Now(), Dimensions: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&event).Error)

	w := doRequest(r, "PUT", "/ops/alert-events/"+uid(event.ID)+"/status", `{}`)
	assertStatus(t, w, 400)
}

// TestCreateAlertSilence_Success 创建静默应成功
func TestCreateAlertSilence_Success(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	rule := model.AlertRule{
		Name: "R", Description: "d", MetricType: "error_rate", Operator: ">",
		Threshold: 0.1, WindowMinutes: 1, SustainedMinutes: 2, Severity: "P1",
		CooldownMinutes: 10, Enabled: true, Filters: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&rule).Error)

	until := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	body := `{"rule_id":` + uid(rule.ID) + `,"platform":"openai","until":"` + until + `","reason":"维护窗口"}`
	w := doRequest(r, "POST", "/ops/alert-silences", body)
	assertStatus(t, w, 200)

	var resp model.AlertSilence
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, rule.ID, resp.RuleID)
	assert.Equal(t, "openai", resp.Platform)
	assert.Equal(t, "维护窗口", resp.Reason)
}

// TestCreateAlertSilence_InvalidJSON 无效 JSON 返回 400
func TestCreateAlertSilence_InvalidJSON(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "POST", "/ops/alert-silences", `{bad`)
	assertStatus(t, w, 400)
}

// TestCreateAlertSilence_WithGroupID 带 group_id 的静默
func TestCreateAlertSilence_WithGroupID(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	rule := model.AlertRule{
		Name: "R", Description: "d", MetricType: "group_available_accounts", Operator: "<",
		Threshold: 1, WindowMinutes: 1, SustainedMinutes: 2, Severity: "P1",
		CooldownMinutes: 10, Enabled: true, Filters: datatypes.JSON("{}"),
	}
	require.NoError(t, db.Create(&rule).Error)

	gid := 42
	until := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	silence := model.AlertSilence{
		RuleID: rule.ID, Platform: "anthropic", GroupID: &gid, Until: time.Now().Add(2 * time.Hour),
		Reason: "分组维护",
	}
	_ = silence
	body := `{"rule_id":` + uid(rule.ID) + `,"platform":"anthropic","group_id":42,"until":"` + until + `","reason":"分组维护"}`
	w := doRequest(r, "POST", "/ops/alert-silences", body)
	assertStatus(t, w, 200)

	var resp model.AlertSilence
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp.GroupID)
	assert.Equal(t, 42, *resp.GroupID)
}
