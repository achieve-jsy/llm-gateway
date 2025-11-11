package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Test Harness ──────────────────────────────────────────────────────────

// keyForwardMockOrch is a minimal ChatOrchestratorInterface for key-forward tests.
type keyForwardMockOrch struct {
	chatFn   func(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error)
	streamFn func(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error)
}

func (m *keyForwardMockOrch) Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	if m.chatFn != nil {
		return m.chatFn(ctx, req)
	}
	return &core.ChatResponse{
		ID:     "mock-id",
		Object: "chat.completion",
		Model:  req.Model,
		Choices: []core.Choice{{
			Index: 0,
			Message: core.ResponseMessage{
				Role:    "assistant",
				Content: "mock response",
			},
			FinishReason: "stop",
		}},
	}, nil
}

func (m *keyForwardMockOrch) ChatStream(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, req)
	}
	// Default: return minimal SSE with [DONE]
	sseBody := `data: {"id":"mock-chunk","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"mock"},"finish_reason":null}]}` + "\n\n" +
		"data: [DONE]\n\n"
	return io.NopCloser(strings.NewReader(sseBody)), nil
}

// keyForwardSetupOpts controls the behaviour of the test harness.
type keyForwardSetupOpts struct {
	authStatus     string
	authStatusCode int
	userID         string
	keyID          string
	chatFn         func(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error)
	streamFn       func(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error)
}

type keyForwardHarness struct {
	Engine     *gin.Engine
	AuthServer *httptest.Server
	Cleanup    func()
}

func newKeyForwardHarness(opts keyForwardSetupOpts) *keyForwardHarness {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/auth/verify-key" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if opts.authStatusCode != 0 {
			w.WriteHeader(opts.authStatusCode)
			json.NewEncoder(w).Encode(gin.H{"error": "auth error"})
			return
		}
		json.NewEncoder(w).Encode(gin.H{
			"code":    0,
			"message": "success",
			"data": gin.H{
				"status": opts.authStatus,
				"key_id": opts.keyID,
				"user":   gin.H{"user_id": opts.userID},
			},
		})
	}))

	authClient := infra.NewAuthClient(authServer.URL, "")
	orch := &keyForwardMockOrch{chatFn: opts.chatFn, streamFn: opts.streamFn}
	chatHandler := NewChatHandler(orch)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.RequestID())
	engine.Use(middleware.ErrorHandler())

	v1 := engine.Group("/v1")
	v1.Use(middleware.AuthMiddleware(authClient))
	v1.POST("/chat/completions", chatHandler.Handle)

	return &keyForwardHarness{
		Engine:     engine,
		AuthServer: authServer,
		Cleanup:    func() { authServer.Close() },
	}
}

func defaultChatRequestBody() []byte {
	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "Hello"}},
		"stream":   false,
	})
	return body
}

func doChatRequest(t *testing.T, engine *gin.Engine, apiKey string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		r.Header.Set("Authorization", "Bearer "+apiKey)
	}
	engine.ServeHTTP(w, r)
	return w
}

// ─── Auth Happy Path ────────────────────────────────────────────────────────

func TestKeyForward_NonStream_Success(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequestBody())

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "mock-id", resp["id"])
}

func TestKeyForward_Stream_Success(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "Hi"}},
		"stream":   true,
	})

	w := doChatRequest(t, h.Engine, "sk-stream-test", body)

	assert.Equal(t, http.StatusOK, w.Code)
	bodyStr := w.Body.String()
	assert.Contains(t, bodyStr, "data:")
	assert.Contains(t, bodyStr, "[DONE]")
}

// ─── Auth Failures ──────────────────────────────────────────────────────────

func TestKeyForward_MissingAuthHeader(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{authStatus: "active"})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "", defaultChatRequestBody())
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "authentication_error")
}

func TestKeyForward_EmptyKey(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{authStatus: "active"})
	defer h.Cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(defaultChatRequestBody()))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer ")
	h.Engine.ServeHTTP(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestKeyForward_MissingBearerPrefix(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{authStatus: "active"})
	defer h.Cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(defaultChatRequestBody()))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "sk-test123") // missing "Bearer " prefix
	h.Engine.ServeHTTP(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestKeyForward_DisabledKey(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{authStatus: "disabled"})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-disabled", defaultChatRequestBody())
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestKeyForward_AuthServiceUnavailable(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus:     "active",
		authStatusCode: http.StatusInternalServerError,
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequestBody())
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "service_unavailable")
}

// ─── LLM routing after auth ─────────────────────────────────────────────────

func TestKeyForward_ModelNotFound(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		chatFn: func(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
			return nil, model.ErrModelNotFound
		},
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequestBody())
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "model_not_found")
}

func TestKeyForward_ProviderError(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		chatFn: func(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
			return nil, model.ErrProviderError
		},
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequestBody())
	assert.Equal(t, http.StatusBadGateway, w.Code)
	var errResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&errResp)
	errObj := errResp["error"].(map[string]interface{})
	assert.Equal(t, "temporary_failure", errObj["type"])
}

func TestKeyForward_MultipleRequests(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	for i := 0; i < 5; i++ {
		w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequestBody())
		assert.Equal(t, http.StatusOK, w.Code, "request %d should succeed", i)
	}
}

func TestKeyForward_InvalidRequestBody(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{authStatus: "active"})
	defer h.Cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{not valid json"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer sk-test123")
	h.Engine.ServeHTTP(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_request")
}

func TestKeyForward_ResponseFormat(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{authStatus: "active"})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequestBody())
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Contains(t, resp, "id")
	assert.Contains(t, resp, "object")
	assert.Contains(t, resp, "model")
	assert.Contains(t, resp, "choices")
}
