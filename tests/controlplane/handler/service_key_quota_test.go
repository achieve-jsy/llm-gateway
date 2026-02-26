package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetServiceKeyQuota_Success tests the GET endpoint for service key quota
func TestGetServiceKeyQuota_Success(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建 service key
	keyID := uuid.New()
	userID := uuid.New()
	rpm := 200
	maxConc := 40
	key := cpmodel.ApiKey{
		ID:                  keyID,
		UserID:              userID,
		Name:                "test-service-key",
		KeyType:             cpmodel.KeyTypeService,
		KeyHash:             "hash-get-test",
		EncryptedKey:        "enc123",
		KeyHint:             "test",
		QuotaRPM:            &rpm,
		QuotaMaxConcurrency: &maxConc,
	}
	require.NoError(t, db.Create(&key).Error)

	// Verify the key exists in DB
	var count int64
	db.Model(&cpmodel.ApiKey{}).Where("id = ? AND key_type = ?", keyID, cpmodel.KeyTypeService).Count(&count)
	require.Equal(t, int64(1), count, "Service key should exist in DB")

	// Make GET request
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/keys/%s/quota", keyID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)
	t.Logf("Response body: %s", w.Body.String())

	assert.Equal(t, http.StatusOK, w.Code, "Should return 200 OK")

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["code"])

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(200), data["rpm"])
	assert.Equal(t, float64(40), data["max_concurrency"])
	assert.Equal(t, "service_key", data["subject_type"])
	assert.Equal(t, "override", data["source"])
}

// TestGetServiceKeyQuota_NotFound tests GET for non-existent key
func TestGetServiceKeyQuota_NotFound(t *testing.T) {
	r, _, _ := setupQuotaHandler(t)

	fakeID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/keys/%s/quota", fakeID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, "key_not_found", errObj["code"])
}

// TestGetServiceKeyQuota_PersonalKey tests GET for personal key (should fail)
func TestGetServiceKeyQuota_PersonalKey(t *testing.T) {
	r, db, _ := setupQuotaHandler(t)

	// 创建 personal key
	keyID := uuid.New()
	userID := uuid.New()
	key := cpmodel.ApiKey{
		ID:           keyID,
		UserID:       userID,
		Name:         "test-personal-key",
		KeyType:      cpmodel.KeyTypePersonal, // NOT service
		KeyHash:      "hash-personal",
		EncryptedKey: "enc123",
		KeyHint:      "test",
	}
	require.NoError(t, db.Create(&key).Error)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/keys/%s/quota", keyID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, "Personal key should not be accessible via service key quota endpoint")
}
