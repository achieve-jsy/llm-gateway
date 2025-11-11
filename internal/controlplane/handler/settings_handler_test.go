package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSettingsSvc struct {
	getSystemSettingsFn func(ctx context.Context) (*model.SystemSettings, error)
	getPublicSettingsFn func(ctx context.Context) (*model.PublicSettings, error)
	updateSettingsFn    func(ctx context.Context, settings *model.SystemSettings) error
	exportAllFn         func(ctx context.Context) (map[string]string, error)
	importSettingsFn    func(ctx context.Context, settings map[string]string) error
}

func (f *fakeSettingsSvc) GetSystemSettings(ctx context.Context) (*model.SystemSettings, error) {
	if f.getSystemSettingsFn != nil {
		return f.getSystemSettingsFn(ctx)
	}
	return nil, nil
}

func (f *fakeSettingsSvc) GetPublicSettings(ctx context.Context) (*model.PublicSettings, error) {
	if f.getPublicSettingsFn != nil {
		return f.getPublicSettingsFn(ctx)
	}
	return nil, nil
}

func (f *fakeSettingsSvc) UpdateSettings(ctx context.Context, settings *model.SystemSettings) error {
	if f.updateSettingsFn != nil {
		return f.updateSettingsFn(ctx, settings)
	}
	return nil
}

func (f *fakeSettingsSvc) ExportAll(ctx context.Context) (map[string]string, error) {
	if f.exportAllFn != nil {
		return f.exportAllFn(ctx)
	}
	return nil, nil
}

func (f *fakeSettingsSvc) ImportSettings(ctx context.Context, settings map[string]string) error {
	if f.importSettingsFn != nil {
		return f.importSettingsFn(ctx, settings)
	}
	return nil
}

var _ SettingsServiceInterface = (*fakeSettingsSvc)(nil)

func newSettingsRouter(svc SettingsServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewSettingsHandler(svc)
	r.GET("/api/v1/settings/public", h.GetPublicSettings)
	r.GET("/api/v1/settings", h.GetSystemSettings)
	r.PUT("/api/v1/settings", h.UpdateSettings)
	r.POST("/api/v1/settings/test-webhook", h.TestWebhook)
	r.POST("/api/v1/settings/export", h.ExportSettings)
	r.POST("/api/v1/settings/import", h.ImportSettings)
	return r
}

func TestGetSystemSettings_IncludesFailoverDefaults(t *testing.T) {
	svc := &fakeSettingsSvc{
		getSystemSettingsFn: func(ctx context.Context) (*model.SystemSettings, error) {
			return &model.SystemSettings{
				SiteName:                       "TestGW",
				DefaultMaxSwitches:             5,
				DefaultSameModelRetries:        2,
				DefaultFailoverCooldownSeconds: 180,
			}, nil
		},
	}
	r := newSettingsRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(5), data["default_max_switches"])
	assert.Equal(t, float64(2), data["default_same_model_retries"])
	assert.Equal(t, float64(180), data["default_failover_cooldown_seconds"])
}

func TestGetSystemSettings_ServiceError(t *testing.T) {
	svc := &fakeSettingsSvc{
		getSystemSettingsFn: func(ctx context.Context) (*model.SystemSettings, error) {
			return nil, errors.New("db error")
		},
	}
	r := newSettingsRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUpdateSettings_WithFailoverDefaults(t *testing.T) {
	var captured *model.SystemSettings
	svc := &fakeSettingsSvc{
		updateSettingsFn: func(ctx context.Context, settings *model.SystemSettings) error {
			captured = settings
			return nil
		},
	}
	r := newSettingsRouter(svc)

	body, _ := json.Marshal(map[string]interface{}{
		"site_name":                          "TestGW",
		"default_max_switches":               4,
		"default_same_model_retries":         2,
		"default_failover_cooldown_seconds":  240,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, captured)
	assert.Equal(t, 4, captured.DefaultMaxSwitches)
	assert.Equal(t, 2, captured.DefaultSameModelRetries)
	assert.Equal(t, 240, captured.DefaultFailoverCooldownSeconds)
}

func TestUpdateSettings_InvalidJSON(t *testing.T) {
	svc := &fakeSettingsSvc{}
	r := newSettingsRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/settings", bytes.NewReader([]byte("bad json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateSettings_ServiceError(t *testing.T) {
	svc := &fakeSettingsSvc{
		updateSettingsFn: func(ctx context.Context, settings *model.SystemSettings) error {
			return errors.New("db error")
		},
	}
	r := newSettingsRouter(svc)

	body, _ := json.Marshal(map[string]string{"site_name": "Test"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetPublicSettings_Success(t *testing.T) {
	svc := &fakeSettingsSvc{
		getPublicSettingsFn: func(ctx context.Context) (*model.PublicSettings, error) {
			return &model.PublicSettings{
				SiteName:             "PublicGW",
				RateLimitPerMinute:   100,
				MaxConcurrencyPerKey: 20,
			}, nil
		},
	}
	r := newSettingsRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/settings/public", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "PublicGW")
	assert.Contains(t, w.Body.String(), `"rate_limit_per_minute":100`)
}

func TestExportSettings_Success(t *testing.T) {
	svc := &fakeSettingsSvc{
		exportAllFn: func(ctx context.Context) (map[string]string, error) {
			return map[string]string{"site_name": "ExportedGW"}, nil
		},
	}
	r := newSettingsRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/settings/export", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ExportedGW")
}

func TestExportSettings_ServiceError(t *testing.T) {
	svc := &fakeSettingsSvc{
		exportAllFn: func(ctx context.Context) (map[string]string, error) {
			return nil, errors.New("db error")
		},
	}
	r := newSettingsRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/settings/export", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestImportSettings_Success(t *testing.T) {
	var captured map[string]string
	svc := &fakeSettingsSvc{
		importSettingsFn: func(ctx context.Context, settings map[string]string) error {
			captured = settings
			return nil
		},
	}
	r := newSettingsRouter(svc)

	body, _ := json.Marshal(map[string]string{"site_name": "ImportedGW"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/settings/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, captured)
	assert.Equal(t, "ImportedGW", captured["site_name"])
}

func TestImportSettings_InvalidJSON(t *testing.T) {
	svc := &fakeSettingsSvc{}
	r := newSettingsRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/settings/import", bytes.NewReader([]byte("bad json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestImportSettings_ServiceError(t *testing.T) {
	svc := &fakeSettingsSvc{
		importSettingsFn: func(ctx context.Context, settings map[string]string) error {
			return errors.New("db error")
		},
	}
	r := newSettingsRouter(svc)

	body, _ := json.Marshal(map[string]string{"site_name": "Test"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/settings/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTestWebhook_NotEnabled(t *testing.T) {
	svc := &fakeSettingsSvc{
		getSystemSettingsFn: func(ctx context.Context) (*model.SystemSettings, error) {
			return &model.SystemSettings{
				WebhookEnabled: false,
				WebhookURL:     "https://hooks.example.com",
			}, nil
		},
	}
	r := newSettingsRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/settings/test-webhook", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTestWebhook_NoURL(t *testing.T) {
	svc := &fakeSettingsSvc{
		getSystemSettingsFn: func(ctx context.Context) (*model.SystemSettings, error) {
			return &model.SystemSettings{
				WebhookEnabled: true,
				WebhookURL:     "",
			}, nil
		},
	}
	r := newSettingsRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/settings/test-webhook", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}