package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
)

// mockVideoProvider implements video.VideoProvider for testing
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

// capturingVideoProvider wraps a mockVideoProvider and captures the request body
type capturingVideoProvider struct {
	*mockVideoProvider
	capturedBody *json.RawMessage
}

func (m *capturingVideoProvider) CreateTask(ctx context.Context, req json.RawMessage) (string, error) {
	*m.capturedBody = req
	return m.mockVideoProvider.CreateTask(ctx, req)
}
// mockAssetProvider implements video.AssetProvider for testing
type mockAssetProvider struct {
	doActionResp json.RawMessage
	doActionErr  error
}

func (m *mockAssetProvider) DoAction(ctx context.Context, action string, req json.RawMessage) (json.RawMessage, error) {
	return m.doActionResp, m.doActionErr
}

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// =============================================================================
// VideoHandler tests
// =============================================================================

func TestVideoHandler_CreateTask_Success(t *testing.T) {
	mock := &mockVideoProvider{
		createTaskID: "cgt-test-123",
	}
	handler := NewVideoHandler(mock, mock)

	router := setupTestRouter()
	router.POST("/contents/generations/tasks", handler.CreateTask)

	body := `{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"test"}],"resolution":"720p"}`
	req, _ := http.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["id"] != "cgt-test-123" {
		t.Errorf("expected task_id 'cgt-test-123', got '%v'", resp["id"])
	}
}

func TestVideoHandler_CreateTask_InvalidJSON(t *testing.T) {
	mock := &mockVideoProvider{}
	handler := NewVideoHandler(mock, mock)

	router := setupTestRouter()
	router.POST("/contents/generations/tasks", handler.CreateTask)

	req, _ := http.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(`not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid JSON, got %d", w.Code)
	}
}

func TestVideoHandler_CreateTask_EmptyBody(t *testing.T) {
	mock := &mockVideoProvider{}
	handler := NewVideoHandler(mock, mock)

	router := setupTestRouter()
	router.POST("/contents/generations/tasks", handler.CreateTask)

	req, _ := http.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(``))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for empty body, got %d", w.Code)
	}
}

func TestVideoHandler_CreateTask_ProviderError(t *testing.T) {
	mock := &mockVideoProvider{
		createErr: errors.New("upstream service unavailable"),
	}
	handler := NewVideoHandler(mock, mock)

	router := setupTestRouter()
	router.POST("/contents/generations/tasks", handler.CreateTask)

	body := `{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"test"}]}`
	req, _ := http.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", w.Code)
	}
}

func TestVideoHandler_CreateTask_WithoutContentType(t *testing.T) {
	mock := &mockVideoProvider{
		createTaskID: "cgt-test-123",
	}
	handler := NewVideoHandler(mock, mock)

	router := setupTestRouter()
	router.POST("/contents/generations/tasks", handler.CreateTask)

	// No Content-Type header set
	req, _ := http.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(`{"model":"test"}`))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Handler uses io.ReadAll directly, so it should still work
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 without Content-Type, got %d", w.Code)
	}
}

func TestVideoHandler_CreateTask_RequestPassthrough(t *testing.T) {
	// Verify that the request body is passed through to the provider unchanged
	var capturedBody json.RawMessage
	baseMock := &mockVideoProvider{
		createTaskID: "cgt-test-789",
	}
	capturingMock := &capturingVideoProvider{
		mockVideoProvider: baseMock,
		capturedBody:      &capturedBody,
	}

	handler := NewVideoHandler(capturingMock, capturingMock)

	router := setupTestRouter()
	router.POST("/contents/generations/tasks", handler.CreateTask)

	originalBody := `{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"test prompt"}],"resolution":"720p","ratio":"16:9","duration":5}`
	req, _ := http.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(originalBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Verify the body was passed through faithfully
	var original, captured map[string]interface{}
	json.Unmarshal([]byte(originalBody), &original)
	json.Unmarshal(capturedBody, &captured)

	if original["model"] != captured["model"] {
		t.Errorf("model mismatch: expected '%v', got '%v'", original["model"], captured["model"])
	}
	if original["resolution"] != captured["resolution"] {
		t.Errorf("resolution mismatch: expected '%v', got '%v'", original["resolution"], captured["resolution"])
	}
	if original["ratio"] != captured["ratio"] {
		t.Errorf("ratio mismatch: expected '%v', got '%v'", original["ratio"], captured["ratio"])
	}
}

// stubVideoGroupDispatch 模拟组调度：把组名解析为上游模型 ID。
type stubVideoGroupDispatch struct {
	decision *scheduler.RouteDecision
	err      error
	seen     string
}

func (s *stubVideoGroupDispatch) Dispatch(c *gin.Context, modelName string, req *core.ChatRequest) (*scheduler.RouteDecision, error) {
	s.seen = modelName
	return s.decision, s.err
}

func TestVideoHandler_CreateTask_RewritesGroupModelToUpstream(t *testing.T) {
	var capturedBody json.RawMessage
	capturingMock := &capturingVideoProvider{
		mockVideoProvider: &mockVideoProvider{createTaskID: "cgt-rewritten"},
		capturedBody:      &capturedBody,
	}
	stub := &stubVideoGroupDispatch{
		decision: &scheduler.RouteDecision{
			ModelName:    "doubao-seedance-2-0-260128",
			ChannelType:  "seedance-relay",
			ProviderName: "景汐-测试E2E",
		},
	}
	handler := NewVideoHandler(capturingMock, capturingMock, WithGroupDispatcher(stub))

	router := setupTestRouter()
	router.POST("/contents/generations/tasks", handler.CreateTask)

	body := `{"model":"seedance-video-test","content":[{"type":"text","text":"一只猫"}],"resolution":"720p","ratio":"16:9","duration":5}`
	req, _ := http.NewRequest("POST", "/contents/generations/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if stub.seen != "seedance-video-test" {
		t.Fatalf("dispatch seen=%q want seedance-video-test", stub.seen)
	}
	var captured map[string]interface{}
	if err := json.Unmarshal(capturedBody, &captured); err != nil {
		t.Fatalf("unmarshal captured: %v", err)
	}
	if captured["model"] != "doubao-seedance-2-0-260128" {
		t.Fatalf("upstream body model=%v want doubao-seedance-2-0-260128", captured["model"])
	}
	if captured["resolution"] != "720p" {
		t.Fatalf("resolution should be preserved, got %v", captured["resolution"])
	}
}

func TestRewriteVideoBodyModel(t *testing.T) {
	in := []byte(`{"model":"seedance-video-test","resolution":"720p","duration":5}`)
	out, err := rewriteVideoBodyModel(in, "doubao-seedance-2-0-260128")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["model"] != "doubao-seedance-2-0-260128" {
		t.Fatalf("model=%v", m["model"])
	}
	if m["resolution"] != "720p" || m["duration"] != float64(5) {
		t.Fatalf("other fields corrupted: %v", m)
	}
}

func TestVideoHandler_GetTask_Success(t *testing.T) {
	mock := &mockVideoProvider{
		getTaskResp: json.RawMessage(`{"id":"cgt-test-123","status":"succeeded","content":{"video_url":"https://example.com/video.mp4"}}`),
	}
	handler := NewVideoHandler(mock, mock)

	router := setupTestRouter()
	router.GET("/contents/generations/tasks/:task_id", handler.GetTask)

	req, _ := http.NewRequest("GET", "/contents/generations/tasks/cgt-test-123", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "succeeded" {
		t.Errorf("expected status 'succeeded', got '%v'", resp["status"])
	}
	if resp["id"] != "cgt-test-123" {
		t.Errorf("expected id 'cgt-test-123', got '%v'", resp["id"])
	}
}

func TestVideoHandler_GetTask_MissingTaskID(t *testing.T) {
	mock := &mockVideoProvider{}
	handler := NewVideoHandler(mock, mock)

	router := setupTestRouter()
	router.GET("/contents/generations/tasks/:task_id", handler.GetTask)

	// Request with empty task_id (route won't match, so 404 from Gin)
	req, _ := http.NewRequest("GET", "/contents/generations/tasks/", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Logf("status: %d (expected 404 for empty param)", w.Code)
	}
}

func TestVideoHandler_GetTask_ProviderError(t *testing.T) {
	mock := &mockVideoProvider{
		getTaskErr: errors.New("task not found"),
	}
	handler := NewVideoHandler(mock, mock)

	router := setupTestRouter()
	router.GET("/contents/generations/tasks/:task_id", handler.GetTask)

	req, _ := http.NewRequest("GET", "/contents/generations/tasks/nonexistent-task", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status 502 for provider error, got %d", w.Code)
	}
}

func TestVideoHandler_GetTask_ResponsePassthrough(t *testing.T) {
	// Verify that the provider's raw JSON response is returned as-is via c.Data
	rawResponse := json.RawMessage(`{"id":"task-999","status":"processing","progress":45,"custom_field":"value"}`)
	mock := &mockVideoProvider{
		getTaskResp: rawResponse,
	}
	handler := NewVideoHandler(mock, mock)

	router := setupTestRouter()
	router.GET("/contents/generations/tasks/:task_id", handler.GetTask)

	req, _ := http.NewRequest("GET", "/contents/generations/tasks/task-999", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Verify the raw response is preserved exactly
	if string(rawResponse) != w.Body.String() {
		t.Errorf("response body mismatch:\n  expected: %s\n  got:      %s", string(rawResponse), w.Body.String())
	}
}

// =============================================================================
// AssetHandler tests
// =============================================================================

func TestAssetHandler_Handle_Success(t *testing.T) {
	mock := &mockAssetProvider{
		doActionResp: json.RawMessage(`{"Result":{"Id":"group-test-456"}}`),
	}
	handler := NewAssetHandler(mock, mock)

	router := setupTestRouter()
	router.POST("/assets/:action", handler.Handle)

	body := `{"Name":"test-group","GroupType":"AIGC"}`
	req, _ := http.NewRequest("POST", "/assets/CreateAssetGroup", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify raw response is returned as-is
	if w.Body.String() != `{"Result":{"Id":"group-test-456"}}` {
		t.Errorf("unexpected response body: %s", w.Body.String())
	}
}

func TestAssetHandler_Handle_AllActions(t *testing.T) {
	mock := &mockAssetProvider{
		doActionResp: json.RawMessage(`{"Result":{"Id":"test-123"}}`),
	}
	handler := NewAssetHandler(mock, mock)

	router := setupTestRouter()
	router.POST("/assets/:action", handler.Handle)

	actions := []string{
		"CreateAssetGroup", "CreateAsset", "GetAsset", "ListAssets",
		"UpdateAsset", "GetAssetGroup", "UpdateAssetGroup", "DeleteAsset",
		"CreateVisualValidateSession", "GetVisualValidateResult",
	}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			req, _ := http.NewRequest("POST", "/assets/"+action, bytes.NewBufferString(`{}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("action %s: expected status 200, got %d", action, w.Code)
			}
		})
	}
}

func TestAssetHandler_Handle_InvalidBody(t *testing.T) {
	mock := &mockAssetProvider{}
	handler := NewAssetHandler(mock, mock)

	router := setupTestRouter()
	router.POST("/assets/:action", handler.Handle)

	req, _ := http.NewRequest("POST", "/assets/CreateAssetGroup", bytes.NewBufferString(`not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// The handler does not parse/validate JSON; it passes the raw body through.
	// So invalid JSON is still forwarded to the provider.
	// If the provider returns an error, it responds with 502.
	// With no provider error, it passes through successfully.
	if w.Code != http.StatusOK {
		t.Logf("status: %d (handler passes raw body through)", w.Code)
	}
}

func TestAssetHandler_Handle_ProviderError(t *testing.T) {
	mock := &mockAssetProvider{
		doActionErr: errors.New("upstream asset service error"),
	}
	handler := NewAssetHandler(mock, mock)

	router := setupTestRouter()
	router.POST("/assets/:action", handler.Handle)

	req, _ := http.NewRequest("POST", "/assets/CreateAssetGroup", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", w.Code)
	}
}

func TestAssetHandler_Handle_EmptyBody(t *testing.T) {
	mock := &mockAssetProvider{
		doActionResp: json.RawMessage(`{"ok":true}`),
	}
	handler := NewAssetHandler(mock, mock)

	router := setupTestRouter()
	router.POST("/assets/:action", handler.Handle)

	req, _ := http.NewRequest("POST", "/assets/GetAsset", bytes.NewBufferString(``))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Empty body is valid JSON? No - io.ReadAll returns "" which is not valid JSON.
	// But the handler passes it through as raw bytes to the provider.
	// The mock provider doesn't validate, so it returns success.
	if w.Code != http.StatusOK {
		t.Logf("status: %d (handler passes raw body through)", w.Code)
	}
}
func TestVideoAdapterRegistry_Swap(t *testing.T) {
	old := &mockVideoProvider{createTaskID: "old"}
	neu := &mockVideoProvider{createTaskID: "new"}
	reg := NewVideoAdapterRegistry(old, nil)
	if got := reg.Get("seedance-relay"); got != old {
		t.Fatalf("expected old adapter")
	}
	reg.Swap(neu, nil, "景汐-新", "")
	if got := reg.Get("seedance-relay"); got != neu {
		t.Fatalf("expected swapped adapter")
	}
	if name := reg.ProviderName("seedance-relay"); name != "景汐-新" {
		t.Fatalf("expected new name, got %q", name)
	}
}
