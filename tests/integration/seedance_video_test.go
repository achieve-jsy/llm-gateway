//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core/video"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockVideoProvider implements video.VideoProvider for integration tests.
type mockVideoProvider struct {
	createTaskID string
	createErr    error
	getTaskResp  json.RawMessage
	getTaskErr   error
}

func (m *mockVideoProvider) CreateTask(ctx context.Context, req json.RawMessage) (string, error) {
	return m.createTaskID, m.createErr
}

func (m *mockVideoProvider) GetTask(ctx context.Context, taskID string) (json.RawMessage, error) {
	return m.getTaskResp, m.getTaskErr
}

// Compile-time interface checks
var _ video.VideoProvider = (*mockVideoProvider)(nil)

// mockAssetProvider implements video.AssetProvider for integration tests.
type mockAssetProvider struct {
	doActionResp json.RawMessage
	doActionErr  error
}

func (m *mockAssetProvider) DoAction(ctx context.Context, action string, req json.RawMessage) (json.RawMessage, error) {
	return m.doActionResp, m.doActionErr
}

var _ video.AssetProvider = (*mockAssetProvider)(nil)

func TestSeedanceVideo_CreateTaskIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("full lifecycle: create -> get -> succeeded", func(t *testing.T) {
		mock := &mockVideoProvider{
			createTaskID: "cgt-test-001",
			getTaskResp:  json.RawMessage(`{"id":"cgt-test-001","status":"succeeded","content":{"video_url":"https://example.com/video.mp4"}}`),
		}

		h := handler.NewVideoHandler(mock, mock)
		router := gin.New()
		router.POST("/contents/generations/tasks", h.CreateTask)
		router.GET("/contents/generations/tasks/:task_id", h.GetTask)

		// Step 1: Create task
		createBody := `{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"test"}],"resolution":"720p","duration":5}`
		req := httptest.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(createBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var createResp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&createResp)
		assert.Equal(t, "cgt-test-001", createResp["id"])

		// Step 2: Query task
		req = httptest.NewRequest("GET", "/contents/generations/tasks/cgt-test-001", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var getResp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&getResp)
		assert.Equal(t, "succeeded", getResp["status"])
		assert.NotNil(t, getResp["content"])
	})

	t.Run("all video scenarios", func(t *testing.T) {
		mock := &mockVideoProvider{createTaskID: "cgt-test-002"}
		h := handler.NewVideoHandler(mock, mock)
		router := gin.New()
		router.POST("/contents/generations/tasks", h.CreateTask)

		scenarios := []struct {
			name string
			body string
		}{
			{"text-to-video", `{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"test"}],"resolution":"720p"}`},
			{"image-to-video", `{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"test"},{"type":"image_url","image_url":{"url":"asset://Asset-xxx"},"role":"first_frame"}]}`},
			{"multimodal", `{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"test"},{"type":"image_url","image_url":{"url":"https://example.com/img.jpg"},"role":"reference_image"},{"type":"video_url","video_url":{"url":"https://example.com/video.mp4"},"role":"reference_video"}]}`},
			{"web-search", `{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"test"}],"tools":[{"type":"web_search"}]}`},
		}

		for _, sc := range scenarios {
			t.Run(sc.name, func(t *testing.T) {
				req := httptest.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(sc.body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				assert.Equal(t, http.StatusOK, w.Code)
			})
		}
	})

	t.Run("all asset actions", func(t *testing.T) {
		mock := &mockAssetProvider{
			doActionResp: json.RawMessage(`{"Result":{"Id":"test-123"}}`),
		}
		h := handler.NewAssetHandler(mock, mock)
		router := gin.New()
		router.POST("/assets/:action", h.Handle)

		actions := []string{
			"CreateAssetGroup", "CreateAsset", "GetAsset", "ListAssets",
			"UpdateAsset", "GetAssetGroup", "UpdateAssetGroup", "DeleteAsset",
			"CreateVisualValidateSession", "GetVisualValidateResult",
		}

		for _, action := range actions {
			t.Run(action, func(t *testing.T) {
				req := httptest.NewRequest("POST", "/assets/"+action, bytes.NewBufferString(`{}`))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				assert.Equal(t, http.StatusOK, w.Code)
			})
		}
	})

	t.Run("route registration", func(t *testing.T) {
		mock := &mockVideoProvider{createTaskID: "cgt-test-003"}
		h := handler.NewVideoHandler(mock, mock)

		router := gin.New()
		router.POST("/contents/generations/tasks", h.CreateTask)
		router.GET("/contents/generations/tasks/:task_id", h.GetTask)

		// Verify routes are registered
		req := httptest.NewRequest("GET", "/contents/generations/tasks/test-id", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		// Should not return 404 for the route
		assert.NotEqual(t, http.StatusNotFound, w.Code)
	})
}

func TestSeedanceVideo_ErrorHandling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("upstream error returns 502", func(t *testing.T) {
		mock := &mockVideoProvider{
			createErr: errors.New("upstream connection refused"),
		}
		h := handler.NewVideoHandler(mock, mock)
		router := gin.New()
		router.POST("/contents/generations/tasks", h.CreateTask)

		req := httptest.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(`{"model":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadGateway, w.Code)
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		mock := &mockVideoProvider{}
		h := handler.NewVideoHandler(mock, mock)
		router := gin.New()
		router.POST("/contents/generations/tasks", h.CreateTask)

		req := httptest.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(`not json`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
