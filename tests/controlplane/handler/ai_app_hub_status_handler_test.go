package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fakeStatusService struct {
	allowed   bool
	authErr   error
	status    service.HubSyncStatus
	statusErr error
}

func (f *fakeStatusService) Sync(context.Context, service.SyncScope) (service.HubSyncResult, error) {
	return service.HubSyncResult{}, nil
}

func (f *fakeStatusService) Authorize(context.Context, string, string, service.SyncScope) (bool, error) {
	return f.allowed, f.authErr
}

func (f *fakeStatusService) Status(context.Context) (service.HubSyncStatus, error) {
	return f.status, f.statusErr
}

func doStatus(t *testing.T, svc handler.HubSyncService) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handler.NewAIAppHubSyncHandler(svc)
	r.GET("/sync-status", func(c *gin.Context) {
		c.Set("user_id", "u-admin")
		h.Status(c)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sync-status", nil))
	return w
}

func TestSyncStatusHandlerReturnsState(t *testing.T) {
	w := doStatus(t, &fakeStatusService{
		allowed: true,
		status: service.HubSyncStatus{
			LastSyncAt:  "2026-11-03T13:50:16+08:00",
			Status:      "success",
			Departments: 100,
			Employees:   547,
			Running:     false,
		},
	})
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "success", body["status"])
	require.Equal(t, float64(547), body["employees"])
	require.Equal(t, false, body["running"])
}

func TestSyncStatusHandlerForbidden(t *testing.T) {
	w := doStatus(t, &fakeStatusService{allowed: false})
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestSyncStatusHandlerRunningFlag(t *testing.T) {
	w := doStatus(t, &fakeStatusService{allowed: true, status: service.HubSyncStatus{Running: true}})
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, true, body["running"], "刷新页面后按钮靠它保持'同步中'")
}
