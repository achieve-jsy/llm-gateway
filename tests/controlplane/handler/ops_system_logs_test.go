package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListJobs_Empty 空任务列表应返回空数组
func TestListJobs_Empty(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/jobs")
	assertStatus(t, w, 200)

	var resp struct {
		Items []interface{} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items)
}

// TestListJobs_WithData 有数据时应返回任务列表并按 name 排序
func TestListJobs_WithData(t *testing.T) {
	r, db, _ := setupOpsHandler(t)

	jobs := []model.AlertJobHeartbeat{
		{Name: "alert_evaluator", Status: "running", LastDurationMs: 50, LastResult: "ok"},
		{Name: "config_sync", Status: "running", LastDurationMs: 120, LastResult: "synced 5 providers"},
		{Name: "budget_check", Status: "error", LastDurationMs: 800, LastResult: "", LastError: "timeout"},
	}
	for i := range jobs {
		require.NoError(t, db.Create(&jobs[i]).Error)
	}

	w := doRequest(r, "GET", "/ops/jobs")
	assertStatus(t, w, 200)

	var resp struct {
		Items []model.AlertJobHeartbeat `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 3)
	assert.Equal(t, "alert_evaluator", resp.Items[0].Name)
	assert.Equal(t, "budget_check", resp.Items[1].Name)
	assert.Equal(t, "config_sync", resp.Items[2].Name)
}

// TestGetSystemLogSinkHealth_Success 健康检查应返回 200 且包含关键字段
func TestGetSystemLogSinkHealth_Success(t *testing.T) {
	r, _, _ := setupOpsHandler(t)
	w := doRequest(r, "GET", "/ops/system-logs/health")
	assertStatus(t, w, 200)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "healthy", resp["status"])
	assert.Contains(t, resp, "pending_count")
	assert.Contains(t, resp, "dropped_count")
	assert.Contains(t, resp, "writer_enabled")
}

// TestSystemLogsCRUD_CreateAndQuery 创建日志后直接通过 GORM 查询验证数据完整性
func TestSystemLogsCRUD_CreateAndQuery(t *testing.T) {
	_, db, _ := setupOpsHandler(t)

	logs := []model.SystemLog{
		{Level: "ERROR", Component: "gateway", Message: "upstream timeout"},
		{Level: "WARN", Component: "scheduler", Message: "model unhealthy"},
		{Level: "INFO", Component: "cache", Message: "cache refreshed"},
	}
	for i := range logs {
		require.NoError(t, db.Create(&logs[i]).Error)
	}

	var count int64
	db.Model(&model.SystemLog{}).Count(&count)
	assert.Equal(t, int64(3), count)

	var errLogs []model.SystemLog
	db.Where("level = ?", "ERROR").Find(&errLogs)
	assert.Len(t, errLogs, 1)
	assert.Equal(t, "upstream timeout", errLogs[0].Message)
}

// TestJobsCRUD_CreateMultiple 多条任务记录全部正确创建
func TestJobsCRUD_CreateMultiple(t *testing.T) {
	_, db, _ := setupOpsHandler(t)

	jobs := []model.AlertJobHeartbeat{
		{Name: "job-1", Status: "running"},
		{Name: "job-2", Status: "error", LastError: "db timeout"},
		{Name: "job-3", Status: "running", LastDurationMs: 200},
	}
	for i := range jobs {
		require.NoError(t, db.Create(&jobs[i]).Error)
	}

	var count int64
	db.Model(&model.AlertJobHeartbeat{}).Count(&count)
	assert.Equal(t, int64(3), count)
}

// TestSystemLogsCRUD_WithOptionalFields 可选字段（request_id, provider, model 等）应正确存储
func TestSystemLogsCRUD_WithOptionalFields(t *testing.T) {
	_, db, _ := setupOpsHandler(t)

	rid := "req-abc-123"
	provider := "openai"
	modelName := "gpt-4"
	durationMs := 350
	status := 200

	log := model.SystemLog{
		Level:      "INFO",
		Component:  "handler",
		Message:    "request completed",
		RequestID:  &rid,
		Provider:   &provider,
		Model:      &modelName,
		DurationMs: &durationMs,
		Status:     &status,
	}
	require.NoError(t, db.Create(&log).Error)

	var fetched model.SystemLog
	db.First(&fetched, log.ID)
	assert.NotNil(t, fetched.RequestID)
	assert.Equal(t, "req-abc-123", *fetched.RequestID)
	assert.Equal(t, "openai", *fetched.Provider)
	assert.Equal(t, "gpt-4", *fetched.Model)
	assert.Equal(t, 350, *fetched.DurationMs)
	assert.Equal(t, 200, *fetched.Status)
}
