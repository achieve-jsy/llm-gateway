package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	authmodel "github.com/example/llm-gateway/internal/auth/model"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	corequota "github.com/example/llm-gateway/internal/core/quota"
	gwquota "github.com/example/llm-gateway/internal/gateway/quota"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupQuotaServiceTestDB creates SQLite in-memory DB for quota service tests
func setupQuotaServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	// users table
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

	// api_keys table
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

	// model_groups table
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
		budget_tier_id TEXT,
		override_mode TEXT DEFAULT '',
		override_context_length INTEGER DEFAULT 0,
		override_max_output_tokens INTEGER DEFAULT 0,
		sticky_enabled INTEGER DEFAULT 0,
		sticky_ttl_seconds INTEGER DEFAULT 0
	)`).Error)

	// providers table
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

	// models table
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
		cache_read_price REAL DEFAULT 0,
		cache_write_price REAL DEFAULT 0,
		price_mode TEXT NOT NULL DEFAULT 'catalog',
		weight INTEGER NOT NULL DEFAULT 1,
		catalog_id TEXT,
		ctx_overridden INTEGER NOT NULL DEFAULT 0,
		max_output_overridden INTEGER NOT NULL DEFAULT 0,
		override_supports_video_input INTEGER,
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

	// system_configs table
	require.NoError(t, db.Exec(`CREATE TABLE system_configs (
		"key" TEXT PRIMARY KEY,
		value TEXT DEFAULT '',
		encrypted INTEGER NOT NULL DEFAULT 0,
		description TEXT DEFAULT '',
		created_at TEXT DEFAULT '',
		updated_at TEXT DEFAULT ''
	)`).Error)

	// quota_exemptions table
	require.NoError(t, db.Exec(`CREATE TABLE quota_exemptions (
		id TEXT PRIMARY KEY,
		subject_type TEXT NOT NULL,
		subject_id TEXT NOT NULL,
		reason TEXT,
		expires_at DATETIME NOT NULL,
		created_by TEXT NOT NULL,
		created_at DATETIME NOT NULL
	)`).Error)

	// Seed default quota settings
	now := time.Now().UTC().Format(time.RFC3339)
	defaults := []cpmodel.SystemConfig{
		{Key: "quota_default_user_rpm", Value: "60", CreatedAt: now, UpdatedAt: now},
		{Key: "quota_default_user_max_concurrency", Value: "10", CreatedAt: now, UpdatedAt: now},
		{Key: "quota_default_service_key_rpm", Value: "120", CreatedAt: now, UpdatedAt: now},
		{Key: "quota_default_service_key_max_concurrency", Value: "20", CreatedAt: now, UpdatedAt: now},
	}
	require.NoError(t, db.Create(&defaults).Error)

	return db
}

func setupQuotaServiceMiniredis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, func() {
		rdb.Close()
		mr.Close()
	}
}

func TestRefreshSnapshot_RebuildsCompleteSnapshot(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	quota := 1000
	user := authmodel.User{
		ID:                  uuid.New(),
		Sub:                 "snapshot-user",
		Name:                "Snapshot User",
		Email:               "snapshot@example.com",
		QuotaRPM:            &quota,
		QuotaMaxConcurrency: &quota,
	}
	require.NoError(t, db.Create(&user).Error)

	staleUserID := uuid.New().String()
	staleQuota := 1
	staleSnapshot, err := json.Marshal(service.QuotaSnapshotData{
		Users: map[string]service.QuotaEntry{
			staleUserID: {
				RPM:            &staleQuota,
				MaxConcurrency: &staleQuota,
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, rdb.Set(context.Background(), service.QuotaSnapshotKey, staleSnapshot, 0).Err())

	svc := service.NewQuotaService(db, rdb)
	require.NoError(t, svc.RefreshSnapshot(context.Background()))

	rawSnapshot, err := rdb.Get(context.Background(), service.QuotaSnapshotKey).Bytes()
	require.NoError(t, err)

	var snapshot service.QuotaSnapshotData
	require.NoError(t, json.Unmarshal(rawSnapshot, &snapshot))
	assert.Equal(t, 60, snapshot.DefaultUserRPM)
	assert.Equal(t, 10, snapshot.DefaultUserMaxConcurrency)
	assert.Equal(t, 120, snapshot.DefaultServiceKeyRPM)
	assert.Equal(t, 20, snapshot.DefaultServiceKeyMaxConcurrency)
	assert.NotContains(t, snapshot.Users, staleUserID)
	entry, ok := snapshot.Users[user.ID.String()]
	require.True(t, ok)
	require.NotNil(t, entry.RPM)
	assert.Equal(t, 1000, *entry.RPM)
	require.NotNil(t, entry.MaxConcurrency)
	assert.Equal(t, 1000, *entry.MaxConcurrency)
	assert.False(t, snapshot.UpdatedAt.IsZero())
}

func TestRefreshSnapshot_UserPartialOverridePreservesInheritedFieldForGateway(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	rpm := 321
	user := authmodel.User{
		ID:       uuid.New(),
		Sub:      "partial-user",
		Name:     "Partial User",
		Email:    "partial-user@example.com",
		QuotaRPM: &rpm,
	}
	require.NoError(t, db.Create(&user).Error)

	svc := service.NewQuotaService(db, rdb)
	require.NoError(t, svc.RefreshSnapshot(context.Background()))

	rawSnapshot, err := rdb.Get(context.Background(), service.QuotaSnapshotKey).Bytes()
	require.NoError(t, err)
	var gatewaySnapshot corequota.QuotaSnapshot
	require.NoError(t, json.Unmarshal(rawSnapshot, &gatewaySnapshot))

	effective := gwquota.ResolveSubjectLimits(&gatewaySnapshot, "user", user.ID.String())
	assert.Equal(t, 321, effective.RPM)
	assert.Equal(t, 10, effective.MaxConcurrency)
	assert.Equal(t, "override", effective.Source)
}

func TestRefreshSnapshot_ServiceKeyPartialOverridePreservesInheritedFieldForGateway(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	maxConcurrency := 7
	key := cpmodel.ApiKey{
		ID:                  uuid.New(),
		UserID:              uuid.New(),
		Name:                "partial-service-key",
		KeyHash:             "partial-service-key-hash",
		EncryptedKey:        "partial-service-key-encrypted",
		KeyType:             cpmodel.KeyTypeService,
		QuotaMaxConcurrency: &maxConcurrency,
	}
	require.NoError(t, db.Create(&key).Error)

	svc := service.NewQuotaService(db, rdb)
	require.NoError(t, svc.RefreshSnapshot(context.Background()))

	rawSnapshot, err := rdb.Get(context.Background(), service.QuotaSnapshotKey).Bytes()
	require.NoError(t, err)
	var gatewaySnapshot corequota.QuotaSnapshot
	require.NoError(t, json.Unmarshal(rawSnapshot, &gatewaySnapshot))

	effective := gwquota.ResolveSubjectLimits(&gatewaySnapshot, "service_key", key.ID.String())
	assert.Equal(t, 120, effective.RPM)
	assert.Equal(t, 7, effective.MaxConcurrency)
	assert.Equal(t, "override", effective.Source)
}

func TestRefreshSnapshot_NilRedisIsDisabled(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	svc := service.NewQuotaService(db, nil)

	require.NoError(t, svc.RefreshSnapshot(context.Background()))
}

func TestRefreshSnapshot_RedisFailureIsReturned(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	cleanup()

	svc := service.NewQuotaService(db, rdb)
	err := svc.RefreshSnapshot(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "set quota snapshot")
}

func TestStartSnapshotReconciler_RefreshesImmediatelyAndPeriodically(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := service.NewQuotaService(db, rdb)
	done := svc.StartSnapshotReconciler(ctx, 10*time.Millisecond)

	require.NoError(t, rdb.Get(context.Background(), service.QuotaSnapshotKey).Err())

	require.NoError(t, rdb.Del(context.Background(), service.QuotaSnapshotKey).Err())
	require.Eventually(t, func() bool {
		return rdb.Exists(context.Background(), service.QuotaSnapshotKey).Val() == 1
	}, time.Second, 10*time.Millisecond)

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

// TestSetUserQuota_Success verifies setting user quota updates DB correctly
func TestSetUserQuota_Success(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	svc := service.NewQuotaService(db, rdb)

	// Create test user
	userID := uuid.New()
	user := authmodel.User{
		ID:    userID,
		Sub:   "test-user",
		Name:  "Test User",
		Email: "test@example.com",
	}
	require.NoError(t, db.Create(&user).Error)

	// Set quota
	rpm := 100
	maxConc := 20
	err := svc.SetUserQuota(context.Background(), userID.String(), &rpm, &maxConc)
	require.NoError(t, err)

	// Wait for async snapshot refresh
	time.Sleep(50 * time.Millisecond)

	// Verify DB
	var updatedUser authmodel.User
	require.NoError(t, db.First(&updatedUser, "id = ?", userID).Error)
	require.NotNil(t, updatedUser.QuotaRPM)
	assert.Equal(t, 100, *updatedUser.QuotaRPM)
	require.NotNil(t, updatedUser.QuotaMaxConcurrency)
	assert.Equal(t, 20, *updatedUser.QuotaMaxConcurrency)
}

// TestSetUserQuota_NilValues verifies nil values clear the override (restore inheritance)
func TestSetUserQuota_NilValues(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	svc := service.NewQuotaService(db, rdb)

	// Create user with quota
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

	// Clear quota (set to nil)
	err := svc.SetUserQuota(context.Background(), userID.String(), nil, nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Verify DB is NULL
	var updatedUser authmodel.User
	require.NoError(t, db.First(&updatedUser, "id = ?", userID).Error)
	assert.Nil(t, updatedUser.QuotaRPM)
	assert.Nil(t, updatedUser.QuotaMaxConcurrency)
}

// TestSetUserQuota_ZeroValues verifies 0 means unlimited (explicitly set, not NULL)
func TestSetUserQuota_ZeroValues(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	svc := service.NewQuotaService(db, rdb)

	userID := uuid.New()
	user := authmodel.User{
		ID:    userID,
		Sub:   "test-user",
		Name:  "Test User",
		Email: "test@example.com",
	}
	require.NoError(t, db.Create(&user).Error)

	// Set to 0 (unlimited)
	rpm := 0
	maxConc := 0
	err := svc.SetUserQuota(context.Background(), userID.String(), &rpm, &maxConc)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Verify DB has 0 (not NULL)
	var updatedUser authmodel.User
	require.NoError(t, db.First(&updatedUser, "id = ?", userID).Error)
	require.NotNil(t, updatedUser.QuotaRPM)
	assert.Equal(t, 0, *updatedUser.QuotaRPM)
	require.NotNil(t, updatedUser.QuotaMaxConcurrency)
	assert.Equal(t, 0, *updatedUser.QuotaMaxConcurrency)
}

// TestSetUserQuota_NegativeRejected verifies negative values are rejected
func TestSetUserQuota_NegativeRejected(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	svc := service.NewQuotaService(db, rdb)

	userID := uuid.New()
	user := authmodel.User{
		ID:    userID,
		Sub:   "test-user",
		Name:  "Test User",
		Email: "test@example.com",
	}
	require.NoError(t, db.Create(&user).Error)

	// Try negative RPM
	rpm := -10
	err := svc.SetUserQuota(context.Background(), userID.String(), &rpm, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota_rpm must be >= 0")

	// Try negative concurrency
	maxConc := -5
	err = svc.SetUserQuota(context.Background(), userID.String(), nil, &maxConc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota_max_concurrency must be >= 0")
}

// TestGetUserEffectiveQuota_Override verifies override source is returned
func TestGetUserEffectiveQuota_Override(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	svc := service.NewQuotaService(db, rdb)

	userID := uuid.New()
	rpm := 150
	maxConc := 30
	user := authmodel.User{
		ID:                  userID,
		Sub:                 "test-user",
		Name:                "Test User",
		Email:               "test@example.com",
		QuotaRPM:            &rpm,
		QuotaMaxConcurrency: &maxConc,
	}
	require.NoError(t, db.Create(&user).Error)

	effective, err := svc.GetUserEffectiveQuota(context.Background(), userID.String())
	require.NoError(t, err)
	require.NotNil(t, effective)

	assert.Equal(t, 150, effective.RPM)
	assert.Equal(t, 30, effective.MaxConcurrency)
	assert.Equal(t, "override", effective.Source)
	assert.Equal(t, "user", effective.SubjectType)
	assert.Equal(t, userID, effective.SubjectID)
	// Defaults should still be reported
	assert.Equal(t, 60, effective.DefaultRPM)
	assert.Equal(t, 10, effective.DefaultMaxConc)
}

// TestGetUserEffectiveQuota_Default verifies default_user source when no override
func TestGetUserEffectiveQuota_Default(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	svc := service.NewQuotaService(db, rdb)

	userID := uuid.New()
	user := authmodel.User{
		ID:    userID,
		Sub:   "test-user",
		Name:  "Test User",
		Email: "test@example.com",
	}
	require.NoError(t, db.Create(&user).Error)

	effective, err := svc.GetUserEffectiveQuota(context.Background(), userID.String())
	require.NoError(t, err)
	require.NotNil(t, effective)

	assert.Equal(t, 60, effective.RPM)
	assert.Equal(t, 10, effective.MaxConcurrency)
	assert.Equal(t, "default_user", effective.Source)
}

func TestGetUserEffectiveQuota_PartialOverrideReportsRawValuesAndFieldSources(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	svc := service.NewQuotaService(db, nil)

	rpm := 150
	user := authmodel.User{
		ID:       uuid.New(),
		Sub:      "partial-effective-user",
		Name:     "Partial Effective User",
		Email:    "partial-effective-user@example.com",
		QuotaRPM: &rpm,
	}
	require.NoError(t, db.Create(&user).Error)

	effective, err := svc.GetUserEffectiveQuota(context.Background(), user.ID.String())
	require.NoError(t, err)
	payload := marshalQuotaEffective(t, effective)

	assert.Equal(t, float64(150), payload["configured_rpm"])
	assert.Contains(t, payload, "configured_max_concurrency")
	assert.Nil(t, payload["configured_max_concurrency"])
	assert.Equal(t, "override", payload["rpm_source"])
	assert.Equal(t, "default_user", payload["max_concurrency_source"])
}

func TestGetServiceKeyEffectiveQuota_PartialOverrideReportsRawValuesAndFieldSources(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	svc := service.NewQuotaService(db, nil)

	maxConcurrency := 7
	key := cpmodel.ApiKey{
		ID:                  uuid.New(),
		UserID:              uuid.New(),
		Name:                "partial-effective-service-key",
		KeyHash:             "partial-effective-service-key-hash",
		EncryptedKey:        "partial-effective-service-key-encrypted",
		KeyType:             cpmodel.KeyTypeService,
		QuotaMaxConcurrency: &maxConcurrency,
	}
	require.NoError(t, db.Create(&key).Error)

	effective, err := svc.GetServiceKeyEffectiveQuota(context.Background(), key.ID.String())
	require.NoError(t, err)
	payload := marshalQuotaEffective(t, effective)

	assert.Contains(t, payload, "configured_rpm")
	assert.Nil(t, payload["configured_rpm"])
	assert.Equal(t, float64(7), payload["configured_max_concurrency"])
	assert.Equal(t, "default_service_key", payload["rpm_source"])
	assert.Equal(t, "override", payload["max_concurrency_source"])
}

func TestGetGroupEffectiveQuota_PartialOverrideKeepsUnsetFieldUnlimited(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	svc := service.NewQuotaService(db, nil)

	maxConcurrency := 5
	group := cpmodel.Group{
		Name:                "partial-effective-group",
		Strategy:            "round_robin",
		Enabled:             true,
		QuotaMaxConcurrency: &maxConcurrency,
	}
	require.NoError(t, db.Create(&group).Error)

	effective, err := svc.GetGroupEffectiveQuota(context.Background(), group.ID)
	require.NoError(t, err)
	payload := marshalQuotaEffective(t, effective)

	assert.Equal(t, 0, effective.RPM)
	assert.Contains(t, payload, "configured_rpm")
	assert.Nil(t, payload["configured_rpm"])
	assert.Equal(t, "unset", payload["rpm_source"])
	assert.Equal(t, "override", payload["max_concurrency_source"])
}

func marshalQuotaEffective(t *testing.T, effective *service.QuotaEffective) map[string]any {
	t.Helper()
	data, err := json.Marshal(effective)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	return payload
}

// TestGetUserEffectiveQuota_Fallback verifies fallback when system settings missing
func TestGetUserEffectiveQuota_Fallback(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	// Delete system settings to force fallback
	db.Exec("DELETE FROM system_configs")

	svc := service.NewQuotaService(db, rdb)

	userID := uuid.New()
	user := authmodel.User{
		ID:    userID,
		Sub:   "test-user",
		Name:  "Test User",
		Email: "test@example.com",
	}
	require.NoError(t, db.Create(&user).Error)

	effective, err := svc.GetUserEffectiveQuota(context.Background(), userID.String())
	require.NoError(t, err)
	require.NotNil(t, effective)

	// Should use hardcoded defaults from SettingsService
	assert.Equal(t, 60, effective.RPM)
	assert.Equal(t, 10, effective.MaxConcurrency)
	assert.Equal(t, "default_user", effective.Source)
}

// TestGetUserEffectiveQuota_UserNotFound verifies admin/non-sso users not in users table
// get default quota instead of error (regression test for admin 500 bug).
func TestGetUserEffectiveQuota_UserNotFound(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	svc := service.NewQuotaService(db, rdb)

	// User never inserted into users table — simulations admin / platform user
	userID := uuid.New()

	effective, err := svc.GetUserEffectiveQuota(context.Background(), userID.String())
	require.NoError(t, err) // must NOT return error
	require.NotNil(t, effective)

	assert.Equal(t, 60, effective.RPM)
	assert.Equal(t, 10, effective.MaxConcurrency)
	assert.Equal(t, "default_user", effective.Source)
	assert.Equal(t, "user", effective.SubjectType)
	assert.Equal(t, userID.String(), effective.SubjectID)
}

// TestSetServiceKeyQuota_ValidateKeyType verifies only service keys can have quota set
func TestSetServiceKeyQuota_ValidateKeyType(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	svc := service.NewQuotaService(db, rdb)

	// Create personal key
	personalKeyID := uuid.New()
	userID := uuid.New()
	personalKey := cpmodel.ApiKey{
		ID:           personalKeyID,
		UserID:       userID,
		Name:         "personal-key",
		KeyType:      cpmodel.KeyTypePersonal,
		KeyHash:      "hash1",
		EncryptedKey: "enc1",
		KeyHint:      "test",
	}
	require.NoError(t, db.Create(&personalKey).Error)

	// Try to set quota on personal key - should fail (RowsAffected = 0)
	rpm := 100
	err := svc.SetServiceKeyQuota(context.Background(), personalKeyID.String(), &rpm, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service key not found")

	// Create service key
	serviceKeyID := uuid.New()
	serviceKey := cpmodel.ApiKey{
		ID:           serviceKeyID,
		UserID:       userID,
		Name:         "service-key",
		KeyType:      cpmodel.KeyTypeService,
		KeyHash:      "hash2",
		EncryptedKey: "enc2",
		KeyHint:      "test",
	}
	require.NoError(t, db.Create(&serviceKey).Error)

	// Set quota on service key - should succeed
	err = svc.SetServiceKeyQuota(context.Background(), serviceKeyID.String(), &rpm, nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	var updatedKey cpmodel.ApiKey
	require.NoError(t, db.First(&updatedKey, "id = ?", serviceKeyID).Error)
	require.NotNil(t, updatedKey.QuotaRPM)
	assert.Equal(t, 100, *updatedKey.QuotaRPM)
}

// TestSetGroupQuota_Success verifies setting group quota
func TestSetGroupQuota_Success(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	svc := service.NewQuotaService(db, rdb)

	group := cpmodel.Group{
		Name:     "test-group",
		Strategy: "round_robin",
		Enabled:  true,
	}
	require.NoError(t, db.Create(&group).Error)

	rpm := 50
	maxConc := 10
	err := svc.SetGroupQuota(context.Background(), group.ID, &rpm, &maxConc)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	var updatedGroup cpmodel.Group
	require.NoError(t, db.First(&updatedGroup, "id = ?", group.ID).Error)
	require.NotNil(t, updatedGroup.QuotaRPM)
	assert.Equal(t, 50, *updatedGroup.QuotaRPM)
	require.NotNil(t, updatedGroup.QuotaMaxConcurrency)
	assert.Equal(t, 10, *updatedGroup.QuotaMaxConcurrency)
}

// TestSetModelQuota_Success verifies setting model quota
func TestSetModelQuota_Success(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()

	svc := service.NewQuotaService(db, rdb)

	provider := cpmodel.Provider{
		ID:     uuid.New(),
		Name:   "test-provider",
		Status: 1,
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

	rpm := 30
	maxConc := 5
	err := svc.SetModelQuota(context.Background(), modelID.String(), &rpm, &maxConc)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	var updatedModel cpmodel.Model
	require.NoError(t, db.First(&updatedModel, "id = ?", modelID).Error)
	require.NotNil(t, updatedModel.QuotaRPM)
	assert.Equal(t, 30, *updatedModel.QuotaRPM)
	require.NotNil(t, updatedModel.QuotaMaxConcurrency)
	assert.Equal(t, 5, *updatedModel.QuotaMaxConcurrency)
}

// ──────────────────────────────────────────────────────────────────────────────
// GetUserCurrentUsage：并发读路径必须与网关租约键格式一致，且只读、不虚报
// ──────────────────────────────────────────────────────────────────────────────

// TestGetUserCurrentUsage_KeyAbsent_ReturnsZero 键不存在属正常（用户无在途请求），
// 返回 0 而不是错误——旧实现按字符串 GET，恒 redis.Nil，被吞掉后恒返回 0 并伴随告警。
func TestGetUserCurrentUsage_KeyAbsent_ReturnsZero(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()
	svc := service.NewQuotaService(db, rdb)

	usage, err := svc.GetUserCurrentUsage(context.Background(), uuid.NewString())
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 0, usage.MaxConcurrency)
	assert.Equal(t, 0, usage.RPM)
}

// TestGetUserCurrentUsage_ReadsLeaseKey_GatewayFormat 读路径必须命中网关实际写入的键：
// 用 gateway quota 包的 ConcurrencyKey 写入（生产写入方），断言控制面能读到。
// 该用例是「键格式漂移」的回归闸门：若两侧前缀/格式再次不一致即失败。
func TestGetUserCurrentUsage_ReadsLeaseKey_GatewayFormat(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()
	svc := service.NewQuotaService(db, rdb)
	ctx := context.Background()
	userID := uuid.NewString()

	// 关键：键由 share 常量构造，网关与控制面必须一致
	require.Equal(t, corequota.ConcurrencyKey("user", userID, "", ""),
		gwquota.ConcurrencyKey("user", userID, "", ""),
		"网关与控制面必须使用完全相同的并发键")

	// 模拟网关写入 3 条在途租约（score = 未来到期时间戳）
	key := gwquota.ConcurrencyKey("user", userID, "", "")
	expireAt := float64(time.Now().Add(6 * time.Minute).UnixMilli())
	for i := 0; i < 3; i++ {
		require.NoError(t, rdb.ZAdd(ctx, key, redis.Z{Score: expireAt, Member: "lease-" + uuid.NewString()}).Err())
	}

	usage, err := svc.GetUserCurrentUsage(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 3, usage.MaxConcurrency, "应读到网关写入的 3 条在途租约")
}

// TestGetUserCurrentUsage_IgnoresExpiredMembers 只有已过期成员时必须返回 0：
// 不能用 ZCARD（会把已过期但未被清理的成员算进来，向上虚报占用）。
func TestGetUserCurrentUsage_IgnoresExpiredMembers(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()
	svc := service.NewQuotaService(db, rdb)
	ctx := context.Background()
	userID := uuid.NewString()
	key := gwquota.ConcurrencyKey("user", userID, "", "")

	// 3 条已过期（score 在过去）、2 条仍在飞（score 在未来）
	past := float64(time.Now().Add(-time.Minute).UnixMilli())
	future := float64(time.Now().Add(5 * time.Minute).UnixMilli())
	for i := 0; i < 3; i++ {
		require.NoError(t, rdb.ZAdd(ctx, key, redis.Z{Score: past, Member: "expired-" + uuid.NewString()}).Err())
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, rdb.ZAdd(ctx, key, redis.Z{Score: future, Member: "in-flight-" + uuid.NewString()}).Err())
	}

	usage, err := svc.GetUserCurrentUsage(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 2, usage.MaxConcurrency, "只应统计未过期（score > now）的成员")
	// 且不得顺手清理：只读语义，过期成员必须仍在
	assert.Equal(t, int64(5), rdb.ZCard(ctx, key).Val(), "读路径不得删除任何成员（非破坏性）")
}

// TestGetUserCurrentUsage_ReadOnly_NoWriteSideEffects 读路径不得改动任何键：
// 断言调用前后 key 的成员集合、score 与 TTL 完全不变。
func TestGetUserCurrentUsage_ReadOnly_NoWriteSideEffects(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()
	svc := service.NewQuotaService(db, rdb)
	ctx := context.Background()
	userID := uuid.NewString()
	concKey := gwquota.ConcurrencyKey("user", userID, "", "")
	rpmKey := corequota.RPMKey("user", userID, "", "")

	require.NoError(t, rdb.ZAdd(ctx, concKey, redis.Z{
		Score: float64(time.Now().Add(time.Minute).UnixMilli()), Member: "lease-a"}).Err())
	require.NoError(t, rdb.ZAdd(ctx, concKey, redis.Z{
		Score: float64(time.Now().Add(-time.Minute).UnixMilli()), Member: "lease-expired"}).Err())
	require.NoError(t, rdb.ZAdd(ctx, rpmKey, redis.Z{
		Score: float64(time.Now().UnixMilli()), Member: "req-1"}).Err())
	t.Cleanup(func() {
		rdb.Del(context.Background(), concKey, rpmKey)
	})

	beforeConc, err := rdb.ZRangeWithScores(ctx, concKey, 0, -1).Result()
	require.NoError(t, err)
	beforeRPM, err := rdb.ZRangeWithScores(ctx, rpmKey, 0, -1).Result()
	require.NoError(t, err)

	usage, err := svc.GetUserCurrentUsage(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, 1, usage.MaxConcurrency)
	require.Equal(t, 1, usage.RPM)

	afterConc, err := rdb.ZRangeWithScores(ctx, concKey, 0, -1).Result()
	require.NoError(t, err)
	afterRPM, err := rdb.ZRangeWithScores(ctx, rpmKey, 0, -1).Result()
	require.NoError(t, err)
	assert.Equal(t, beforeConc, afterConc, "读路径不得改动并发键")
	assert.Equal(t, beforeRPM, afterRPM, "读路径不得改动 RPM 键")
}

// TestGetUserCurrentUsage_AllExpired_MustUseZCountNotZCard 选型对照断言（t5 追加要求）：
// 构造「全部成员都已过期但尚未被清理」的 ZSET —— 这是租约式设计引入的新常态
// （进程被强杀 / 优雅退出强断长流式请求导致归还丢失，条目到期后仍留在集合里，
// 直到下一次 acquire 的 ZREMRANGEBYSCORE 顺手清理）。
//
// 本用例同时固定两件事：
//
//	(a) 这种残留状态在真实运行中确实存在（ZCARD > 0），不是理论假设；
//	(b) 用 ZCARD 读会把它算成占用而虚报，因此读路径必须用带 now 下界的 ZCOUNT。
//
// 判别力：若将来有人把只读路径「优化」回 ZCARD，本用例立即失败。
func TestGetUserCurrentUsage_AllExpired_MustUseZCountNotZCard(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()
	svc := service.NewQuotaService(db, rdb)
	ctx := context.Background()
	userID := uuid.NewString()
	key := gwquota.ConcurrencyKey("user", userID, "", "")

	// 3 条已过期（score 在过去）、0 条在飞
	past := float64(time.Now().Add(-time.Minute).UnixMilli())
	for i := 0; i < 3; i++ {
		require.NoError(t, rdb.ZAdd(ctx, key,
			redis.Z{Score: past, Member: "lost-lease-" + uuid.NewString()}).Err())
	}

	// 对照组的两个原始读数（故意用 ZCARD 展示会虚报）
	zcard := rdb.ZCard(ctx, key).Val()
	zcount, err := rdb.ZCount(ctx, key, fmt.Sprintf("(%d", time.Now().UnixMilli()), "+inf").Result()
	require.NoError(t, err)

	if zcard <= 0 {
		t.Fatalf("前置条件不成立：应存在已过期未清理的成员（ZCARD=%d）", zcard)
	}
	if zcount != 0 {
		t.Fatalf("ZCARD=%d 但真正在飞数应为 0，实际 ZCOUNT=%d", zcard, zcount)
	}
	if zcard == zcount {
		t.Fatalf("本对照要求 ZCARD(%d) 与 ZCOUNT(%d) 必须不同，否则选型差异未被覆盖", zcard, zcount)
	}

	// 读路径必须返回 0（等价于 ZCOUNT 语义），不得被 ZCARD 虚报
	usage, err := svc.GetUserCurrentUsage(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 0, usage.MaxConcurrency,
		"全部成员已过期时读路径必须返回 0（ZCARD=%d 属虚报）", zcard)

	// 只读语义：读完之后那些残留成员必须原封不动（不得 DEL / 不得 ZREMRANGEBYSCORE）
	assert.Equal(t, int64(3), rdb.ZCard(ctx, key).Val(),
		"只读路径不得清理已过期成员")
	assert.True(t, rdb.Exists(ctx, key).Val() > 0, "只读路径不得删除键")
}

// TestGetUserCurrentUsage_RPM_WindowOnly 选型一致性：RPM 读数同样只统计窗口内请求。
// 写入侧判定脚本用 ZREMRANGEBYSCORE 丢弃窗口外条目，因此读侧也必须带窗口下界，
// 否则窗口外但尚未清理的残留成员会被 ZCARD 算进来而虚报。
func TestGetUserCurrentUsage_RPM_WindowOnly(t *testing.T) {
	db := setupQuotaServiceTestDB(t)
	rdb, cleanup := setupQuotaServiceMiniredis(t)
	defer cleanup()
	svc := service.NewQuotaService(db, rdb)
	ctx := context.Background()
	userID := uuid.NewString()
	rpmKey := corequota.RPMKey("user", userID, "", "")

	nowMS := float64(time.Now().UnixMilli())
	window := float64(corequota.RPMWindowMS)
	// 2 条窗口内 + 2 条窗口外（更早，但键尚未过期/尚未被写入侧清理）
	for i := 0; i < 2; i++ {
		require.NoError(t, rdb.ZAdd(ctx, rpmKey, redis.Z{Score: nowMS - 1000, Member: "in-" + uuid.NewString()}).Err())
	}
	for i := 0; i < 2; i++ {
		stale := nowMS - window - float64(i+1)*1000
		require.NoError(t, rdb.ZAdd(ctx, rpmKey, redis.Z{Score: stale, Member: "stale-" + uuid.NewString()}).Err())
	}

	if zcard := rdb.ZCard(ctx, rpmKey).Val(); zcard != 4 {
		t.Fatalf("前置条件：集合应有 4 条成员（含窗口外残留），实际 %d", zcard)
	}

	usage, err := svc.GetUserCurrentUsage(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 2, usage.RPM, "RPM 只应统计窗口内请求（窗口外残留不得计入）")
	// 只读：窗口外残留必须原封不动
	assert.Equal(t, int64(4), rdb.ZCard(ctx, rpmKey).Val(), "只读路径不得清理窗口外成员")
}
