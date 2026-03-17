package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupHealthCheckHandler 创建 HealthCheckHandler 和 Gin Engine，用于测试配置相关接口。
// 注意：providerRepo / modelRepo / rdb 传 nil，因为 GetConfig / UpdateConfig 不依赖它们。
// 测试中 config.Enabled 保持 false，避免 Start() 触发后台 goroutine 使用 nil 依赖。
func setupHealthCheckHandler(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "open sqlite")

	// SQLite :memory: is per-connection; limit to 1 so all goroutines share the same DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	// 使用 raw SQL 建表，避免 PG 专属的 gen_random_uuid() 和 uuid 类型
	err = db.Exec(`
		CREATE TABLE health_check_configs (
			id TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 0,
			interval_seconds INTEGER NOT NULL DEFAULT 300,
			failure_threshold INTEGER NOT NULL DEFAULT 3,
			failure_action TEXT NOT NULL DEFAULT 'auto_disable',
			check_providers INTEGER NOT NULL DEFAULT 1,
			check_models INTEGER NOT NULL DEFAULT 1,
			check_mode TEXT NOT NULL DEFAULT 'connectivity',
			timeout_seconds INTEGER NOT NULL DEFAULT 15,
			model_check_enabled INTEGER NOT NULL DEFAULT 1,
			model_check_interval_seconds INTEGER NOT NULL DEFAULT 300,
			model_check_failure_threshold INTEGER NOT NULL DEFAULT 3,
			group_check_enabled INTEGER NOT NULL DEFAULT 1,
			group_check_interval_seconds INTEGER NOT NULL DEFAULT 300,
			model_recovery_enabled INTEGER NOT NULL DEFAULT 1,
			model_recovery_success_threshold INTEGER NOT NULL DEFAULT 2,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);
		CREATE TABLE health_check_logs (
			id TEXT PRIMARY KEY,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			target_name TEXT NOT NULL DEFAULT '',
			provider_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
			provider_name TEXT NOT NULL DEFAULT '',
			success INTEGER NOT NULL,
			response_time_ms INTEGER NOT NULL DEFAULT 0,
			error_message TEXT DEFAULT '',
			checked_at DATETIME NOT NULL
		)
	`).Error
	require.NoError(t, err, "create health_check_configs table")

	healthRepo := repository.NewHealthCheckRepo(db)
	svc := service.NewHealthCheckService(healthRepo, nil, nil, nil)
	h := handler.NewHealthCheckHandler(svc)

	r := gin.New()
	r.GET("/api/v1/health-check/config", h.GetConfig)
	r.PUT("/api/v1/health-check/config", h.UpdateConfig)
	r.GET("/api/v1/health-check/logs", h.GetLogs)
	r.GET("/api/v1/health-check/status", h.GetStatus)
	r.POST("/api/v1/health-check/run", h.RunNow)

	return r, db
}

// seedDefaultConfig 在 DB 中插入一条默认配置
func seedDefaultConfig(t *testing.T, db *gorm.DB) *model.HealthCheckConfig {
	t.Helper()
	cfg := &model.HealthCheckConfig{
		ID:                            uuid.New(), // SQLite 不会自动生成 UUID，需显式设置
		Enabled:                       false,      // 测试中不启动后台 goroutine
		IntervalSeconds:               300,
		FailureThreshold:              3,
		FailureAction:                 "auto_disable",
		CheckProviders:                true,
		CheckModels:                   true,
		CheckMode:                     "connectivity",
		TimeoutSeconds:                15,
		ModelCheckEnabled:             true,
		ModelCheckIntervalSeconds:     300,
		ModelCheckFailureThreshold:    3,
		GroupCheckEnabled:             true,
		GroupCheckIntervalSeconds:     300,
		ModelRecoveryEnabled:          true,
		ModelRecoverySuccessThreshold: 2,
	}
	err := db.Create(cfg).Error
	require.NoError(t, err, "seed default config")
	return cfg
}

// doHealthRequest 发送 HTTP 请求并返回 recorder
func doHealthRequest(r *gin.Engine, method, path string, body ...string) *httptest.ResponseRecorder {
	var reqBody string
	if len(body) > 0 {
		reqBody = body[0]
	}
	req := httptest.NewRequest(method, path, strings.NewReader(reqBody))
	if reqBody != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// assertHealthStatus 断言 HTTP 状态码
func assertHealthStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Errorf("status = %d, want %d, body = %s", w.Code, want, w.Body.String())
	}
}

// ===================== GetConfig 测试 =====================

func TestGetConfig_Success(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	w := doHealthRequest(r, "GET", "/api/v1/health-check/config")
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			Enabled                    bool   `json:"enabled"`
			IntervalSeconds            int    `json:"interval_seconds"`
			IntervalMinutes            int    `json:"interval_minutes"`
			FailureThreshold           int    `json:"max_failures"`
			CheckMode                  string `json:"check_mode"`
			ModelCheckEnabled          bool   `json:"model_check_enabled"`
			ModelCheckIntervalSeconds  int    `json:"model_check_interval_seconds"`
			ModelCheckIntervalMinutes  int    `json:"model_check_interval_minutes"`
			ModelCheckFailureThreshold int    `json:"model_check_failure_threshold"`
			ModelRecoveryEnabled       bool   `json:"model_recovery_enabled"`
			ModelRecoverySuccessThreshold int `json:"model_recovery_success_threshold"`
			GroupCheckEnabled          bool   `json:"group_check_enabled"`
			GroupCheckIntervalSeconds  int    `json:"group_check_interval_seconds"`
			GroupCheckIntervalMinutes  int    `json:"group_check_interval_minutes"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, false, resp.Data.Enabled)
	assert.Equal(t, 300, resp.Data.IntervalSeconds)
	assert.Equal(t, 5, resp.Data.IntervalMinutes)
	assert.Equal(t, 3, resp.Data.FailureThreshold)
	assert.Equal(t, "connectivity", resp.Data.CheckMode)
	assert.Equal(t, true, resp.Data.ModelCheckEnabled)
	assert.Equal(t, 300, resp.Data.ModelCheckIntervalSeconds)
	assert.Equal(t, 5, resp.Data.ModelCheckIntervalMinutes)
	assert.Equal(t, 3, resp.Data.ModelCheckFailureThreshold)
	assert.Equal(t, true, resp.Data.ModelRecoveryEnabled)
	assert.Equal(t, 2, resp.Data.ModelRecoverySuccessThreshold)
	assert.Equal(t, true, resp.Data.GroupCheckEnabled)
	assert.Equal(t, 300, resp.Data.GroupCheckIntervalSeconds)
	assert.Equal(t, 5, resp.Data.GroupCheckIntervalMinutes)
}

func TestGetConfig_DefaultValuesForZeroFields(t *testing.T) {
	// 模拟存量数据：新字段为 0（数据库默认值），验证 API 兜底返回 300
	r, db := setupHealthCheckHandler(t)
	cfg := &model.HealthCheckConfig{
		Enabled:                       false,
		IntervalSeconds:               60,
		FailureThreshold:              2,
		FailureAction:                 "alert_only",
		CheckMode:                     "model_forward",
		TimeoutSeconds:                10,
		ModelCheckEnabled:             true,
		ModelCheckIntervalSeconds:     0, // zero → 应兜底 300
		ModelCheckFailureThreshold:    0, // zero → 应兜底 3
		GroupCheckEnabled:             true,
		GroupCheckIntervalSeconds:     0, // zero → 应兜底 300
		ModelRecoveryEnabled:          false,
		ModelRecoverySuccessThreshold: 0,
	}
	err := db.Create(cfg).Error
	require.NoError(t, err)

	w := doHealthRequest(r, "GET", "/api/v1/health-check/config")
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			ModelCheckIntervalSeconds  int `json:"model_check_interval_seconds"`
			ModelCheckIntervalMinutes  int `json:"model_check_interval_minutes"`
			ModelCheckFailureThreshold int `json:"model_check_failure_threshold"`
			GroupCheckIntervalSeconds  int `json:"group_check_interval_seconds"`
			GroupCheckIntervalMinutes  int `json:"group_check_interval_minutes"`
		} `json:"data"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	// 零值兜底
	assert.Equal(t, 300, resp.Data.ModelCheckIntervalSeconds)
	assert.Equal(t, 5, resp.Data.ModelCheckIntervalMinutes)
	assert.Equal(t, 3, resp.Data.ModelCheckFailureThreshold)
	assert.Equal(t, 300, resp.Data.GroupCheckIntervalSeconds)
	assert.Equal(t, 5, resp.Data.GroupCheckIntervalMinutes)
}

func TestGetConfig_NoConfigRow(t *testing.T) {
	// 没有 config 记录时，GetConfig 返回 DB 错误 → 500
	r, _ := setupHealthCheckHandler(t)
	// 不 seed 任何数据

	w := doHealthRequest(r, "GET", "/api/v1/health-check/config")
	assertHealthStatus(t, w, http.StatusInternalServerError)
}

// ===================== UpdateConfig 测试 =====================

func TestUpdateConfig_AllFields(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{
		"enabled": false,
		"interval_seconds": 120,
		"max_failures": 5,
		"failure_action": "alert_only",
		"check_mode": "both",
		"timeout_seconds": 30,
		"model_check_enabled": false,
		"model_check_interval_seconds": 180,
		"model_check_failure_threshold": 5,
		"model_recovery_enabled": false,
		"model_recovery_success_threshold": 3,
		"group_check_enabled": false,
		"group_check_interval_seconds": 180
	}`

	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			Enabled                     bool   `json:"enabled"`
			IntervalSeconds             int    `json:"interval_seconds"`
			IntervalMinutes             int    `json:"interval_minutes"`
			FailureThreshold            int    `json:"max_failures"`
			FailureAction               string `json:"failure_action"`
			CheckMode                   string `json:"check_mode"`
			TimeoutSeconds              int    `json:"timeout_seconds"`
			ModelCheckEnabled           bool   `json:"model_check_enabled"`
			ModelCheckIntervalSeconds   int    `json:"model_check_interval_seconds"`
			ModelCheckIntervalMinutes   int    `json:"model_check_interval_minutes"`
			ModelCheckFailureThreshold  int    `json:"model_check_failure_threshold"`
			ModelRecoveryEnabled        bool   `json:"model_recovery_enabled"`
			ModelRecoverySuccessThreshold int  `json:"model_recovery_success_threshold"`
			GroupCheckEnabled           bool   `json:"group_check_enabled"`
			GroupCheckIntervalSeconds   int    `json:"group_check_interval_seconds"`
			GroupCheckIntervalMinutes   int    `json:"group_check_interval_minutes"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, false, resp.Data.Enabled)
	assert.Equal(t, 120, resp.Data.IntervalSeconds)
	assert.Equal(t, 2, resp.Data.IntervalMinutes)
	assert.Equal(t, 5, resp.Data.FailureThreshold)
	assert.Equal(t, "alert_only", resp.Data.FailureAction)
	assert.Equal(t, "both", resp.Data.CheckMode)
	assert.Equal(t, 30, resp.Data.TimeoutSeconds)
	assert.Equal(t, false, resp.Data.ModelCheckEnabled)
	assert.Equal(t, 180, resp.Data.ModelCheckIntervalSeconds)
	assert.Equal(t, 3, resp.Data.ModelCheckIntervalMinutes)
	assert.Equal(t, 5, resp.Data.ModelCheckFailureThreshold)
	assert.Equal(t, false, resp.Data.ModelRecoveryEnabled)
	assert.Equal(t, 3, resp.Data.ModelRecoverySuccessThreshold)
	assert.Equal(t, false, resp.Data.GroupCheckEnabled)
	assert.Equal(t, 180, resp.Data.GroupCheckIntervalSeconds)
	assert.Equal(t, 3, resp.Data.GroupCheckIntervalMinutes)
}

func TestUpdateConfig_IntervalMinutes(t *testing.T) {
	// interval_minutes 应被转换为 seconds
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{"interval_minutes": 3}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			IntervalSeconds int `json:"interval_seconds"`
			IntervalMinutes int `json:"interval_minutes"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 180, resp.Data.IntervalSeconds)
	assert.Equal(t, 3, resp.Data.IntervalMinutes)
}

func TestUpdateConfig_ModelCheckIntervalMinutes(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{"model_check_interval_minutes": 3}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			ModelCheckIntervalSeconds int `json:"model_check_interval_seconds"`
			ModelCheckIntervalMinutes int `json:"model_check_interval_minutes"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 180, resp.Data.ModelCheckIntervalSeconds)
	assert.Equal(t, 3, resp.Data.ModelCheckIntervalMinutes)
}

func TestUpdateConfig_GroupCheckIntervalMinutes(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{"group_check_interval_minutes": 3}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			GroupCheckIntervalSeconds int `json:"group_check_interval_seconds"`
			GroupCheckIntervalMinutes int `json:"group_check_interval_minutes"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 180, resp.Data.GroupCheckIntervalSeconds)
	assert.Equal(t, 3, resp.Data.GroupCheckIntervalMinutes)
}

func TestUpdateConfig_MaxFailuresAlias(t *testing.T) {
	// max_failures 是 failure_threshold 的别名，应正确映射
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{"max_failures": 10}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			FailureThreshold int `json:"max_failures"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 10, resp.Data.FailureThreshold)
}

// ===================== UpdateConfig 最小间隔校验 =====================

func TestUpdateConfig_ClampIntervalSecondsBelowMin(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	// 设置 10 秒 → 应被 clamp 到 60 秒
	body := `{"interval_seconds": 10}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			IntervalSeconds int `json:"interval_seconds"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 60, resp.Data.IntervalSeconds, "interval_seconds < 60 should be clamped to 60")
}

func TestUpdateConfig_ClampModelCheckIntervalBelowMin(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{"model_check_interval_seconds": 30}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			ModelCheckIntervalSeconds int `json:"model_check_interval_seconds"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 60, resp.Data.ModelCheckIntervalSeconds, "model_check_interval_seconds < 60 should be clamped to 60")
}

func TestUpdateConfig_ClampGroupCheckIntervalBelowMin(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{"group_check_interval_seconds": 10}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			GroupCheckIntervalSeconds int `json:"group_check_interval_seconds"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 60, resp.Data.GroupCheckIntervalSeconds, "group_check_interval_seconds < 60 should be clamped to 60")
}

func TestUpdateConfig_ClampIntervalMinutesBelowMin(t *testing.T) {
	// interval_minutes=1 → 60 秒 → 不应被 clamp（60 是合法值）
	// interval_minutes=0 → 0 秒 → 会被 clamp 到 60
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{"interval_minutes": 0}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			IntervalSeconds int `json:"interval_seconds"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 60, resp.Data.IntervalSeconds, "interval_minutes=0 → 0s → clamped to 60")
}

func TestUpdateConfig_IntervalAtExactly60(t *testing.T) {
	// 60 秒是合法的最小值，不应被 clamp
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{"interval_seconds": 60, "model_check_interval_seconds": 60, "group_check_interval_seconds": 60}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			IntervalSeconds           int `json:"interval_seconds"`
			ModelCheckIntervalSeconds int `json:"model_check_interval_seconds"`
			GroupCheckIntervalSeconds int `json:"group_check_interval_seconds"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 60, resp.Data.IntervalSeconds)
	assert.Equal(t, 60, resp.Data.ModelCheckIntervalSeconds)
	assert.Equal(t, 60, resp.Data.GroupCheckIntervalSeconds)
}

// ===================== UpdateConfig 边界/错误测试 =====================

func TestUpdateConfig_InvalidJSON(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", `{invalid json}`)
	assertHealthStatus(t, w, http.StatusBadRequest)
}

func TestUpdateConfig_NoConfigRow(t *testing.T) {
	// 没有 config 记录时，GetConfig 失败 → 500
	r, _ := setupHealthCheckHandler(t)
	// 不 seed 数据

	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", `{"interval_seconds": 120}`)
	assertHealthStatus(t, w, http.StatusInternalServerError)
}

func TestUpdateConfig_GroupCheckEnabledOff(t *testing.T) {
	// 关闭模型组检测
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{"group_check_enabled": false}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			GroupCheckEnabled bool `json:"group_check_enabled"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, false, resp.Data.GroupCheckEnabled)
}

func TestUpdateConfig_GroupCheckEnabledOn(t *testing.T) {
	// 开启模型组检测，使用自定义间隔
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	body := `{"group_check_enabled": true, "group_check_interval_minutes": 10}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", body)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			GroupCheckEnabled         bool `json:"group_check_enabled"`
			GroupCheckIntervalSeconds int  `json:"group_check_interval_seconds"`
			GroupCheckIntervalMinutes int  `json:"group_check_interval_minutes"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, true, resp.Data.GroupCheckEnabled)
	assert.Equal(t, 600, resp.Data.GroupCheckIntervalSeconds)
	assert.Equal(t, 10, resp.Data.GroupCheckIntervalMinutes)
}

// ===================== GetStatus 测试 =====================

func TestGetStatus_Disabled(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	w := doHealthRequest(r, "GET", "/api/v1/health-check/status")
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			Enabled bool `json:"enabled"`
			Running bool `json:"running"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, false, resp.Data.Enabled)
	assert.Equal(t, false, resp.Data.Running, "running should be false when enabled=false")
}

// ===================== GetLogs 测试 =====================

func TestGetLogs_DefaultLimit(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	w := doHealthRequest(r, "GET", "/api/v1/health-check/logs")
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data []model.HealthCheckLog `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	// 无日志时返回空数组
	assert.NotNil(t, resp.Data)
}

func TestGetLogs_WithCustomLimit(t *testing.T) {
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	w := doHealthRequest(r, "GET", "/api/v1/health-check/logs?limit=10")
	assertHealthStatus(t, w, http.StatusOK)
}

// ===================== RunNow 测试 =====================
// RunNow spawns goroutines that call providerRepo.List / modelRepo.List, which
// are nil in this test setup.  Skip this test — RunNow 的端到端行为在集成测试中覆盖。

// ===================== 模型组配置持久化验证 =====================

func TestUpdateConfig_GroupFieldsPersisted(t *testing.T) {
	// 验证 group check 字段确实写入 DB 并可通过 GetConfig 读回
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	// Step 1: 更新模型组配置
	updateBody := `{
		"group_check_enabled": true,
		"group_check_interval_minutes": 3
	}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", updateBody)
	assertHealthStatus(t, w, http.StatusOK)

	// Step 2: 读取配置，验证持久化
	w = doHealthRequest(r, "GET", "/api/v1/health-check/config")
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			GroupCheckEnabled         bool `json:"group_check_enabled"`
			GroupCheckIntervalSeconds int  `json:"group_check_interval_seconds"`
			GroupCheckIntervalMinutes int  `json:"group_check_interval_minutes"`
			// 其他字段不应被修改
			ModelCheckEnabled         bool `json:"model_check_enabled"`
			ModelCheckIntervalSeconds int  `json:"model_check_interval_seconds"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, true, resp.Data.GroupCheckEnabled)
	assert.Equal(t, 180, resp.Data.GroupCheckIntervalSeconds)
	assert.Equal(t, 3, resp.Data.GroupCheckIntervalMinutes)
	// 其他字段保持默认值
	assert.Equal(t, true, resp.Data.ModelCheckEnabled)
	assert.Equal(t, 300, resp.Data.ModelCheckIntervalSeconds)
}

// ===================== 模型组独立开关验证 =====================

func TestUpdateConfig_GroupCheckIndependentFromModelCheck(t *testing.T) {
	// 验证模型组检测开关独立于模型检测开关
	r, db := setupHealthCheckHandler(t)
	seedDefaultConfig(t, db)

	// 关闭模型检测，但保持模型组检测开启
	updateBody := `{
		"model_check_enabled": false,
		"group_check_enabled": true,
		"group_check_interval_seconds": 120
	}`
	w := doHealthRequest(r, "PUT", "/api/v1/health-check/config", updateBody)
	assertHealthStatus(t, w, http.StatusOK)

	var resp struct {
		Data struct {
			ModelCheckEnabled         bool `json:"model_check_enabled"`
			GroupCheckEnabled         bool `json:"group_check_enabled"`
			GroupCheckIntervalSeconds int  `json:"group_check_interval_seconds"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, false, resp.Data.ModelCheckEnabled, "model check should be disabled")
	assert.Equal(t, true, resp.Data.GroupCheckEnabled, "group check should remain enabled")
	assert.Equal(t, 120, resp.Data.GroupCheckIntervalSeconds)
}