package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fakeSyncService struct {
	syncErr  error
	authErr  error
	allowed  bool
	gotScope service.SyncScope
	syncCall int
}

func (f *fakeSyncService) Sync(_ context.Context, scope service.SyncScope) (service.HubSyncResult, error) {
	f.gotScope = scope
	f.syncCall++
	if f.syncErr != nil {
		return service.HubSyncResult{}, f.syncErr
	}
	return service.HubSyncResult{Scope: string(scope.Kind), Departments: 3, Studios: 2}, nil
}

func (f *fakeSyncService) Authorize(_ context.Context, _, _ string, _ service.SyncScope) (bool, error) {
	if f.authErr != nil {
		return false, f.authErr
	}
	return f.allowed, nil
}

func newSyncRouter(svc handler.HubSyncService, passport string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handler.NewAIAppHubSyncHandler(svc)
	r.POST("/sync", func(c *gin.Context) {
		c.Set("user_id", passport)
		h.Sync(c)
	})
	return r
}

func doSync(t *testing.T, svc handler.HubSyncService, passport, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	newSyncRouter(svc, passport).ServeHTTP(w, req)
	return w
}

func TestSyncHandlerMapsErrors(t *testing.T) {
	cases := []struct {
		name   string
		svcErr error
		want   int
	}{
		{"in progress", service.ErrSyncInProgress, http.StatusConflict},
		{"scope missing", service.ErrScopeNotFound, http.StatusNotFound},
		{"upstream", ehrclient.ErrUpstream, http.StatusBadGateway},
		{"unauthorized upstream", ehrclient.ErrUnauthorized, http.StatusBadGateway},
		{"empty", ehrclient.ErrEmptyResult, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeSyncService{allowed: true, syncErr: tc.svcErr}
			w := doSync(t, svc, "hezhiguo", "{\"scope\":\"all\"}")
			require.Equal(t, tc.want, w.Code, w.Body.String())
		})
	}
}

func TestSyncHandlerForbiddenForNonLeader(t *testing.T) {
	svc := &fakeSyncService{allowed: false}
	w := doSync(t, svc, "zhangsan", "{\"scope\":\"studio\",\"id\":\"3f2b0000-0000-0000-0000-000000000000\"}")
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Zero(t, svc.syncCall, "无权限时不得真正触发同步")
}

func TestSyncHandlerRejectsUnknownScope(t *testing.T) {
	svc := &fakeSyncService{allowed: true}
	w := doSync(t, svc, "hezhiguo", "{\"scope\":\"whatever\"}")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Zero(t, svc.syncCall)
}

func TestSyncHandlerRejectsMissingID(t *testing.T) {
	svc := &fakeSyncService{allowed: true}
	w := doSync(t, svc, "hezhiguo", "{\"scope\":\"studio\"}")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSyncHandlerSuccessReturnsResult(t *testing.T) {
	svc := &fakeSyncService{allowed: true}
	w := doSync(t, svc, "hezhiguo", "{\"scope\":\"all\"}")
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "all", body["scope"])
	require.Equal(t, float64(3), body["departments"])
}

func TestSyncHandlerUnknownScopeReturns404(t *testing.T) {
	svc := &fakeSyncService{allowed: true, authErr: service.ErrScopeNotFound}
	w := doSync(t, svc, "hezhiguo", "{\"scope\":\"department\",\"id\":\"not-synced\"}")
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Zero(t, svc.syncCall)
}
