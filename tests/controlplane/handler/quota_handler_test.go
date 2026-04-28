package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	authmodel "github.com/example/llm-gateway/internal/auth/model"
	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/core/quota"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test Setup
// ──────────────────────────────────────────────────────────────────────────────

// setupQuotaTestDB creates SQLite in-memory DB with raw DDL (avoids PostgreSQL-specific
// `gen_random_uuid()` in GORM tags that SQLite cannot parse).
func setupQuotaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// SQLite :memory: is per-connection; limit to 1 so all goroutines share the same DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	// users table (matches authmodel.User, PG-only defaults replaced)
	require.NoError(t, db.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		sub TEXT NOT NULL,
		dept_id TEXT,
		name TEXT NOT NULL,
		email TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'oidc',
		role TEXT NOT NULL DEFAULT 'user',
		employee_id TEXT DEFAULT '',
		job_family TEXT DEFAULT '',
		job_category TEXT DEFAULT '',
		job_title TEXT DEFAULT '',
		dept_source TEXT NOT NULL DEFAULT 'sso',
		dept_updated_by TEXT DEFAULT '',
		dept_updated_at DATETIME,
		login_status TEXT NOT NULL DEFAULT 'active',
		quota_rpm INTEGER,
		quota_max_concurrency INTEGER,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`).Error)

	// api_keys table (matches cpmodel.ApiKey)
	require.NoError(t, db.Exec(`CREATE TABLE api_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		name TEXT NOT NULL,
		key_hash TEXT NOT NULL,
		encrypted_key TEXT NOT NULL DEFAULT '',
		key_prefix TEXT NOT NULL DEFAULT 'sk-',
		key_hint TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		log_enabled INTEGER NOT NULL DEFAULT 1,
		access_all_groups INTEGER NOT NULL DEFAULT 0,
		bypass_org_check INTEGER NOT NULL DEFAULT 0,
		user_name TEXT DEFAULT '',
		dept_id TEXT DEFAULT '',
		dept_name TEXT DEFAULT '',
		studio_id TEXT DEFAULT '',
		studio_name TEXT DEFAULT '',
		key_type TEXT NOT NULL DEFAULT 'personal',
		purpose TEXT NOT NULL DEFAULT '',
		created_by TEXT,
		ip_allowlist TEXT NOT NULL DEFAULT '',
		quota_rpm INTEGER,
		quota_max_concurrency INTEGER,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		last_used_at DATETIME,
		expires_at DATETIME
	)`).Error)

	// model_groups table (matches cpmodel.Group)
	require.NoError(t, db.Exec(`CREATE TABLE model_groups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME,
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		model_type TEXT NOT NULL DEFAULT 'chat',
		strategy TEXT NOT NULL DEFAULT 'round_robin',
		enabled INTEGER NOT NULL DEFAULT 1,
		max_switches INTEGER NOT NULL DEFAULT 2,
		same_model_retries INTEGER NOT NULL DEFAULT 1,
		failover_cooldown_seconds INTEGER NOT NULL DEFAULT 60,
		timeout_seconds INTEGER NOT NULL DEFAULT 0,
		open_mode TEXT NOT NULL DEFAULT 'public',
		is_capability_override INTEGER NOT NULL DEFAULT 0,
		override_supports_vision INTEGER NOT NULL DEFAULT 0,
		override_supports_function_calling INTEGER NOT NULL DEFAULT 0,
		override_supports_tool_choice INTEGER NOT NULL DEFAULT 0,
		quota_rpm INTEGER,
		quota_max_concurrency INTEGER,
		budget_tier_id TEXT
	)`).Error)

	// providers table (matches cpmodel.Provider)
	require.NoError(t, db.Exec(`CREATE TABLE providers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		definition_id TEXT DEFAULT '',
		type TEXT NOT NULL DEFAULT '',
		endpoint TEXT NOT NULL DEFAULT '',
		encrypted_credentials TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		weight INTEGER NOT NULL DEFAULT 1,
		priority INTEGER NOT NULL DEFAULT 0,
		status INTEGER NOT NULL DEFAULT 1,
		test_model TEXT DEFAULT '',
		response_time INTEGER DEFAULT 0,
		balance REAL DEFAULT 0,
		remark TEXT DEFAULT '',
		max_retries INTEGER NOT NULL DEFAULT 3,
		timeout INTEGER NOT NULL DEFAULT 30,
		tag TEXT DEFAULT '',
		consecutive_failures INTEGER NOT NULL DEFAULT 0,
		last_check_error TEXT DEFAULT '',
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`).Error)

	// models table (matches cpmodel.Model)
	require.NoError(t, db.Exec(`CREATE TABLE models (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		provider_id TEXT NOT NULL,
		model_type TEXT NOT NULL DEFAULT 'chat',
		enabled INTEGER NOT NULL DEFAULT 1,
		status INTEGER NOT NULL DEFAULT 1,
		upstream_model_id TEXT DEFAULT '',
		description TEXT DEFAULT '',
		context_length INTEGER DEFAULT 0,
		max_output_tokens INTEGER DEFAULT 0,
		input_price REAL DEFAULT 0,
		output_price REAL DEFAULT 0,
		price_mode TEXT NOT NULL DEFAULT 'catalog',
		weight INTEGER NOT NULL DEFAULT 1,
		catalog_id TEXT,
		ctx_overridden INTEGER NOT NULL DEFAULT 0,
		max_output_overridden INTEGER NOT NULL DEFAULT 0,
		video_supported_resolutions TEXT NOT NULL DEFAULT '[]',
		video_supported_aspect_ratios TEXT NOT NULL DEFAULT '[]',
		video_min_duration_seconds INTEGER NOT NULL DEFAULT 0,
		video_max_duration_seconds INTEGER NOT NULL DEFAULT 0,
		video_supported_actions TEXT NOT NULL DEFAULT '[]',
		quota_rpm INTEGER,
		quota_max_concurrency INTEGER,
		consecutive_failures INTEGER NOT NULL DEFAULT 0,
		consecutive_recovery_successes INTEGER NOT NULL DEFAULT 0,
		last_check_error TEXT DEFAULT '',
		response_time INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`).Error)

	// system_configs table (matches cpmodel.SystemConfig)
	require.NoError(t, db.Exec(`CREATE TABLE system_configs (
		"key" TEXT PRIMARY KEY,
		value TEXT DEFAULT '',
		encrypted INTEGER NOT NULL DEFAULT 0,
		description TEXT DEFAULT '',
		created_at TEXT DEFAULT '',
		updated_at TEXT DEFAULT ''
	)`).Error)

	// quota_exemptions table (matches quota.QuotaExemption)
	require.NoError(t, db.Exec(`CREATE TABLE quota_exemptions (
		id TEXT PRIMARY KEY,
		subject_type TEXT NOT NULL,
		subject_id TEXT NOT NULL,
		reason TEXT DEFAULT '',
		expires_at DATETIME NOT NULL,
		created_by TEXT NOT NULL,
		created_at DATETIME NOT NULL
	)`).Error)

	// Seed default quota settings
	now := time.Now().UTC().Format(time.RFC3339)
	defaults := []cpmodel.SystemConfig{
		{Key: "default_user_rpm", Value: "60", CreatedAt: now, UpdatedAt: now},
		{Key: "default_user_max_concurrency", Value: "10", CreatedAt: now, UpdatedAt: now},
		{Key: "default_service_key_rpm", Value: "120", CreatedAt: now, UpdatedAt: now},
		{Key: "default_service_key_max_concurrency", Value: "20", CreatedAt: now, UpdatedAt: now},
	}
	require.NoError(t, db.Create(&defaults).Error)

	return db
}

func setupQuotaMiniredis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, func() {
		rdb.Close()
		mr.Close()
	}
}

func setupQuotaHandler(t *testing.T) (*gin.Engine, *gorm.DB, *redis.Client) {
	t.Helper()
	db := setupQuotaTestDB(t)
	rdb, cleanup := setupQuotaMiniredis(t)
	t.Cleanup(cleanup)

	quotaSvc := service.NewQuotaService(db, rdb)
	exemptionSvc := service.NewQuotaExemptionService(db, rdb)

	quotaH := handler.NewQuotaHandler(quotaSvc)
	quotaH.SetAuditRecorder(audit.Nop{})

	exemptionH := handler.NewQuotaExemptionHandler(exemptionSvc)
	exemptionH.SetAuditRecorder(audit.Nop{})

	r := gin.New()

	// User quota routes
	r.PATCH("/api/v1/users/:id/quota", quotaH.SetUserQuota)
	r.GET("/api/v1/users/:id/quota", quotaH.GetUserQuota)
	r.GET("/api/v1/me/quota", func(c *gin.Context) {
		// 模拟 auth middleware 设置 user_id
		c.Set("user_id", c.GetHeader("X-Test-User-ID"))
		quotaH.GetMyQuota(c)
	})

	// Group quota routes
	r.PATCH("/api/v1/groups/:id/quota", quotaH.SetGroupQuota)
	r.GET("/api/v1/groups/:id/quota", quotaH.GetGroupQuota)

	// Model quota routes
	r.PATCH("/api/v1/models/:id/quota", quotaH.SetModelQuota)
	r.GET("/api/v1/models/:id/quota", quotaH.GetModelQuota)

	// Service key quota routes
	r.PATCH("/api/v1/keys/:id/quota", quotaH.SetServiceKeyQuota)
	r.GET("/api/v1/keys/:id/quota", quotaH.GetServiceKeyQuota)

	// Exemption routes
	r.POST("/api/v1/quota/exemptions", func(c *gin.Context) {
		c.Set("user_id", "test-admin-user")
		exemptionH.Create(c)
	})
	r.GET("/api/v1/quota/exemptions", exemptionH.List)
	r.DELETE("/api/v1/quota/exemptions/:id", func(c *gin.Context) {
		c.Set("user_id", "test-admin-user")
		exemptionH.Revoke(c)
	})

	return r, db, rdb
}

func doQuotaRequest(r *gin.Engine, method, path string, body ...string) *httptest.ResponseRecorder {
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

// ──────────────────────────────────────────────────────────────────────────────
// QuotaHandler Tests
// ──────────────────────────────────────────────────────────────────────────────

func TestSetUserQuota_Success(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建测试用户
	userID := uuid.New()
	user := authmodel.User{
		ID:    userID,
		Sub:   "test-user",
		Name:  "Test User",
		Email: "test@example.com",
	}
	require.NoError(t, db.Create(&user).Error)

	// 设置配额
	body := `{"rpm": 100, "max_concurrency": 20}`
	w := doQuotaRequest(r, http.MethodPatch, fmt.Sprintf("/api/v1/users/%s/quota", userID), body)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["code"])
	assert.Equal(t, "用户配额已更新", resp["message"])

	// 验证 DB 中的配额
	var updatedUser authmodel.User
	require.NoError(t, db.First(&updatedUser, "id = ?", userID).Error)
	require.NotNil(t, updatedUser.QuotaRPM)
	assert.Equal(t, 100, *updatedUser.QuotaRPM)
	require.NotNil(t, updatedUser.QuotaMaxConcurrency)
	assert.Equal(t, 20, *updatedUser.QuotaMaxConcurrency)
}

func TestSetUserQuota_ClearOverride(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建用户并设置配额
	userID := uuid.New()
	rpm := 100
	maxConc := 20
	user := authmodel.User{
		ID:                  userID,
		Sub:                 "test-user",
		Name:                "Test User",
		Email:               "test@example.com",
		QuotaRPM:            &rpm,
		QuotaMaxConcurrency: &maxConc,
	}
	require.NoError(t, db.Create(&user).Error)

	// 清除配额（设为 null）
	body := `{"rpm": null, "max_concurrency": null}`
	w := doQuotaRequest(r, http.MethodPatch, fmt.Sprintf("/api/v1/users/%s/quota", userID), body)

	assert.Equal(t, http.StatusOK, w.Code)

	// 验证 DB 中配额为 NULL
	var updatedUser authmodel.User
	require.NoError(t, db.First(&updatedUser, "id = ?", userID).Error)
	assert.Nil(t, updatedUser.QuotaRPM)
	assert.Nil(t, updatedUser.QuotaMaxConcurrency)
}

func TestGetUserQuota_WithOverride(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建用户并设置自定义配额
	userID := uuid.New()
	rpm := 100
	maxConc := 20
	user := authmodel.User{
		ID:                  userID,
		Sub:                 "test-user",
		Name:                "Test User",
		Email:               "test@example.com",
		QuotaRPM:            &rpm,
		QuotaMaxConcurrency: &maxConc,
	}
	require.NoError(t, db.Create(&user).Error)

	w := doQuotaRequest(r, http.MethodGet, fmt.Sprintf("/api/v1/users/%s/quota", userID))

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["code"])

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(100), data["rpm"])
	assert.Equal(t, float64(20), data["max_concurrency"])
	assert.Equal(t, "override", data["source"])
	assert.Equal(t, "user", data["subject_type"])
}

func TestGetUserQuota_Default(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建用户（无自定义配额）
	userID := uuid.New()
	user := authmodel.User{
		ID:    userID,
		Sub:   "test-user",
		Name:  "Test User",
		Email: "test@example.com",
	}
	require.NoError(t, db.Create(&user).Error)

	w := doQuotaRequest(r, http.MethodGet, fmt.Sprintf("/api/v1/users/%s/quota", userID))

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(60), data["rpm"])             // 默认值
	assert.Equal(t, float64(10), data["max_concurrency"]) // 默认值
	assert.Equal(t, "default_user", data["source"])
}

func TestSetGroupQuota_Success(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建测试组
	group := cpmodel.Group{
		Name:     "test-group",
		Strategy: "round_robin",
		Enabled:  true,
	}
	require.NoError(t, db.Create(&group).Error)

	body := `{"rpm": 50, "max_concurrency": 10}`
	w := doQuotaRequest(r, http.MethodPatch, fmt.Sprintf("/api/v1/groups/%d/quota", group.ID), body)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "模型组配额已更新", resp["message"])

	// 验证 DB
	var updatedGroup cpmodel.Group
	require.NoError(t, db.First(&updatedGroup, "id = ?", group.ID).Error)
	require.NotNil(t, updatedGroup.QuotaRPM)
	assert.Equal(t, 50, *updatedGroup.QuotaRPM)
	require.NotNil(t, updatedGroup.QuotaMaxConcurrency)
	assert.Equal(t, 10, *updatedGroup.QuotaMaxConcurrency)
}

func TestSetModelQuota_Success(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建 provider 和 model
	provider := cpmodel.Provider{
		ID:     uuid.New(),
		Name:   "test-provider",
		Status: 1, // enabled
	}
	require.NoError(t, db.Create(&provider).Error)

	modelID := uuid.New()
	enabled := true
	model := cpmodel.Model{
		ID:         modelID,
		Name:       "test-model",
		ProviderID: provider.ID,
		Enabled:    &enabled,
	}
	require.NoError(t, db.Create(&model).Error)

	body := `{"rpm": 30, "max_concurrency": 5}`
	w := doQuotaRequest(r, http.MethodPatch, fmt.Sprintf("/api/v1/models/%s/quota", modelID), body)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "模型配额已更新", resp["message"])

	// 验证 DB
	var updatedModel cpmodel.Model
	require.NoError(t, db.First(&updatedModel, "id = ?", modelID).Error)
	require.NotNil(t, updatedModel.QuotaRPM)
	assert.Equal(t, 30, *updatedModel.QuotaRPM)
}

func TestSetServiceKeyQuota_Success(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建 service key
	keyID := uuid.New()
	userID := uuid.New()
	key := cpmodel.ApiKey{
		ID:           keyID,
		UserID:       userID,
		Name:         "test-service-key",
		KeyType:      cpmodel.KeyTypeService,
		KeyHash:      "hash123",
		EncryptedKey: "enc123",
		KeyHint:      "test",
	}
	require.NoError(t, db.Create(&key).Error)

	body := `{"rpm": 200, "max_concurrency": 40}`
	w := doQuotaRequest(r, http.MethodPatch, fmt.Sprintf("/api/v1/keys/%s/quota", keyID), body)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Service Key 配额已更新", resp["message"])

	// 验证 DB
	var updatedKey cpmodel.ApiKey
	require.NoError(t, db.First(&updatedKey, "id = ?", keyID).Error)
	require.NotNil(t, updatedKey.QuotaRPM)
	assert.Equal(t, 200, *updatedKey.QuotaRPM)
}

func TestGetMyQuota(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建用户
	userID := uuid.New()
	rpm := 150
	user := authmodel.User{
		ID:       userID,
		Sub:      "test-user",
		Name:     "Test User",
		Email:    "test@example.com",
		QuotaRPM: &rpm,
	}
	require.NoError(t, db.Create(&user).Error)

	// 通过 header 模拟 user_id
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/quota", nil)
	req.Header.Set("X-Test-User-ID", userID.String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	eff := data["effective"].(map[string]interface{})
	assert.Equal(t, float64(150), eff["rpm"])
	assert.Equal(t, "override", eff["source"])
}

func TestSetQuota_InvalidValue(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	userID := uuid.New()
	user := authmodel.User{
		ID:    userID,
		Sub:   "test-user",
		Name:  "Test User",
		Email: "test@example.com",
	}
	require.NoError(t, db.Create(&user).Error)

	// 负数应返回 500（service 返回 error）
	body := `{"rpm": -10, "max_concurrency": 20}`
	w := doQuotaRequest(r, http.MethodPatch, fmt.Sprintf("/api/v1/users/%s/quota", userID), body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Contains(t, errObj["message"], "quota_rpm must be >= 0")
}

func TestSetQuota_NotFound(t *testing.T) {
	r, _, _ := setupQuotaHandler(t)

	// 不存在的用户 ID
	fakeID := uuid.New()
	body := `{"rpm": 100, "max_concurrency": 20}`
	w := doQuotaRequest(r, http.MethodPatch, fmt.Sprintf("/api/v1/users/%s/quota", fakeID), body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Contains(t, errObj["message"], "user not found")
}

// ──────────────────────────────────────────────────────────────────────────────
// QuotaExemptionHandler Tests
// ──────────────────────────────────────────────────────────────────────────────

func TestCreateExemption_Success(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	userID := uuid.New()
	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{
		"subject_type": "user",
		"subject_id": "%s",
		"reason": "紧急业务需求",
		"expires_at": "%s"
	}`, userID, expiresAt)

	w := doQuotaRequest(r, http.MethodPost, "/api/v1/quota/exemptions", body)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["code"])
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "user", data["subject_type"])
	assert.Equal(t, userID.String(), data["subject_id"])
	assert.Equal(t, "紧急业务需求", data["reason"])

	// 验证 DB 中有记录
	var count int64
	db.Model(&quota.QuotaExemption{}).Where("subject_id = ?", userID).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestCreateExemption_InvalidSubjectType(t *testing.T) {
	r, _, _ := setupQuotaHandler(t)

	body := `{
		"subject_type": "invalid_type",
		"subject_id": "some-id",
		"reason": "test",
		"expires_at": "2099-12-31T23:59:59Z"
	}`

	w := doQuotaRequest(r, http.MethodPost, "/api/v1/quota/exemptions", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Contains(t, errObj["message"], "invalid subject_type")
}

func TestCreateExemption_PastExpiry(t *testing.T) {
	r, _, _ := setupQuotaHandler(t)

	pastTime := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{
		"subject_type": "user",
		"subject_id": "%s",
		"reason": "test",
		"expires_at": "%s"
	}`, uuid.New(), pastTime)

	w := doQuotaRequest(r, http.MethodPost, "/api/v1/quota/exemptions", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Contains(t, errObj["message"], "expires_at must be in the future")
}

func TestListExemptions_ActiveOnly(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建 2 个豁免：1 个未过期，1 个已过期
	activeExemption := quota.QuotaExemption{
		ID:          uuid.New(),
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "active",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	}
	expiredExemption := quota.QuotaExemption{
		ID:          uuid.New(),
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "expired",
		ExpiresAt:   time.Now().Add(-24 * time.Hour),
		CreatedBy:   "admin",
	}
	require.NoError(t, db.Create(&activeExemption).Error)
	require.NoError(t, db.Create(&expiredExemption).Error)

	w := doQuotaRequest(r, http.MethodGet, "/api/v1/quota/exemptions")

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]interface{})
	assert.Equal(t, 1, len(data), "should only return active exemptions")

	exemption := data[0].(map[string]interface{})
	assert.Equal(t, "active", exemption["reason"])
}

func TestRevokeExemption_Success(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建豁免
	exemptionID := uuid.New()
	exemption := quota.QuotaExemption{
		ID:          exemptionID,
		SubjectType: "user",
		SubjectID:   uuid.New().String(),
		Reason:      "test",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		CreatedBy:   "admin",
	}
	require.NoError(t, db.Create(&exemption).Error)

	w := doQuotaRequest(r, http.MethodDelete, fmt.Sprintf("/api/v1/quota/exemptions/%s", exemptionID))

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "豁免已撤销", resp["message"])

	// 验证 DB 中记录被删除
	var count int64
	db.Model(&quota.QuotaExemption{}).Where("id = ?", exemptionID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestRevokeExemption_NotFound(t *testing.T) {
	r, _, _ := setupQuotaHandler(t)

	fakeID := uuid.New()
	w := doQuotaRequest(r, http.MethodDelete, fmt.Sprintf("/api/v1/quota/exemptions/%s", fakeID))

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Contains(t, errObj["message"], "exemption not found")
}
