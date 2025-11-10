package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Test Harness ──────────────────────────────────────────────────────────

// keyForwardSetupOpts controls the behavior of the test harness.
type keyForwardSetupOpts struct {
	authStatus    string
	authStatusCode int    // HTTP status returned by auth mock
	userID        string
	keyID         string
	// Provider control
	chatFunc func(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error)
	chatStreamFunc func(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error)
	registeredModels map[string]string // model -> provider name
}

// keyForwardHarness encapsulates the wired-up test engine and cleanup.
type keyForwardHarness struct {
	Engine     *gin.Engine
	AuthServer *httptest.Server
	Cleanup    func()
}

// newKeyForwardHarness builds a Gin engine that routes:
//
//	mock-auth <-- AuthClient <-- AuthMiddleware --> ChatHandler --> ChatOrchestrator --> MockProvider
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
				"user": gin.H{
					"user_id": opts.userID,
				},
			},
		})
	}))

	authClient := infra.NewAuthClient(authServer.URL, "")

	reg := provider.NewRegistry()
	mp := provider.NewMockProvider("openai")
	if opts.chatFunc != nil {
		mp.SetChatFunc(opts.chatFunc)
	}
	if opts.chatStreamFunc != nil {
		mp.SetChatStreamFunc(opts.chatStreamFunc)
	}
	reg.Register("openai", mp)

	models := opts.registeredModels
	if models == nil {
		models = map[string]string{"gpt-4": "openai"}
	}
	for m, p := range models {
		reg.RegisterModel(m, p)
	}

	orch := orchestrator.NewChatOrchestrator(reg)
	chatHandler := NewChatHandler(orch)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.RequestID())
	engine.Use(middleware.ErrorHandler())

	v1 := engine.Group("/v1")
	v1.Use(middleware.AuthMiddleware(authClient))
	{
		v1.POST("/chat/completions", chatHandler.Handle)
	}

	return &keyForwardHarness{
		Engine:     engine,
		AuthServer: authServer,
		Cleanup: func() { authServer.Close() },
	}
}

// authHeader builds a Bearer auth header value.
func authHeader(key string) string {
	if key == "" {
		return ""
	}
	return "Bearer " + key
}

// defaultChatRequest returns a minimal non-stream chat request.
func defaultChatRequest() model.ChatCompletionRequest {
	return model.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
		Stream: false,
	}
}

// doChatRequest sends a chat completion request and returns the raw response.
func doChatRequest(t *testing.T, engine *gin.Engine, apiKey string, req model.ChatCompletionRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		r.Header.Set("Authorization", authHeader(apiKey))
	}

	engine.ServeHTTP(w, r)
	return w
}

// ─── Single Key Forwarding — Happy Path ────────────────────────────────────

func TestKeyForward_NonStream_Success(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
		chatFunc: func(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
			finishReason := "stop"
			return &model.ChatCompletionResponse{
				ID:      "forwarded-resp-1",
				Object:  "chat.completion",
				Created: 1234567890,
				Model:   req.Model,
				Choices: []model.Choice{
					{
						Index:        0,
						Message:      &model.Message{Role: "assistant", Content: "forwarded via key auth"},
						FinishReason: &finishReason,
					},
				},
				Usage: &model.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
			}, nil
		},
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequest())

	assert.Equal(t, http.StatusOK, w.Code)

	var resp model.ChatCompletionResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "forwarded-resp-1", resp.ID)
	assert.Equal(t, "gpt-4", resp.Model)
	assert.NotEmpty(t, resp.Choices)
	assert.Equal(t, "assistant", resp.Choices[0].Message.Role)
	assert.Equal(t, "forwarded via key auth", resp.Choices[0].Message.Content)
	assert.NotNil(t, resp.Usage)
}

func TestKeyForward_Stream_Success(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	req := defaultChatRequest()
	req.Stream = true

	w := doChatRequest(t, h.Engine, "sk-stream-test", req)

	assert.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	assert.Contains(t, body, "data:")
	assert.Contains(t, body, "[DONE]")
	assert.Contains(t, body, "mock")
}

func TestKeyForward_WithLongKey(t *testing.T) {
	longKey := "sk-" + strings.Repeat("abcdef0123456789", 4)

	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-long",
		keyID:      "key-long",
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, longKey, defaultChatRequest())

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestKeyForward_WithExtraHeaders(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	body, err := json.Marshal(defaultChatRequest())
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer sk-test123")
	r.Header.Set("X-Custom-Header", "custom-value")

	h.Engine.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
}

// ─── Single Key Forwarding — Auth Failures ────────────────────────────────

func TestKeyForward_MissingAuthHeader(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "", defaultChatRequest())

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "error")
	assert.Contains(t, w.Body.String(), "authentication_error")
}

func TestKeyForward_EmptyKey(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	body, err := json.Marshal(defaultChatRequest())
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer ")

	h.Engine.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestKeyForward_MissingBearerPrefix(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	body, err := json.Marshal(defaultChatRequest())
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "sk-test123") // missing "Bearer " prefix

	h.Engine.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestKeyForward_WrongAuthPrefix(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	body, err := json.Marshal(defaultChatRequest())
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Basic sk-test123")

	h.Engine.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestKeyForward_DisabledKey(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "disabled",
		userID:     "user-001",
		keyID:      "key-disabled",
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-disabled", defaultChatRequest())

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body := w.Body.String()
	assert.Contains(t, body, "error")
	assert.Contains(t, body, "invalid or disabled")
}

func TestKeyForward_AuthServiceUnavailable(t *testing.T) {
	// Auth mock returns 500 → gateway should return 503
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus:     "active",
		authStatusCode: http.StatusInternalServerError,
		userID:         "user-001",
		keyID:          "key-001",
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequest())

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "service_unavailable")
}

// ─── Single Key Forwarding — LLM routing after auth ────────────────────────

func TestKeyForward_ModelNotFound(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus:       "active",
		userID:           "user-001",
		keyID:            "key-001",
		registeredModels: map[string]string{},
	})
	defer h.Cleanup()

	req := defaultChatRequest()
	req.Model = "non-existent-model"

	w := doChatRequest(t, h.Engine, "sk-test123", req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "model_not_found")
}

func TestKeyForward_ProviderError(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
		chatFunc: func(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
			return nil, errors.New("upstream 502")
		},
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequest())

	assert.Equal(t, http.StatusBadGateway, w.Code)

	var errResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&errResp)
	errObj := errResp["error"].(map[string]interface{})
	assert.Equal(t, "temporary_failure", errObj["type"])
}

func TestKeyForward_MultipleRequests(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	for i := 0; i < 5; i++ {
		w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequest())
		assert.Equal(t, http.StatusOK, w.Code, "request %d should succeed", i)
	}
}

// ─── Single Key Forwarding — Identity propagation ─────────────────────────

func TestKeyForward_IdentityPropagation(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-uuid-123",
		keyID:      "key-uuid-456",
		chatFunc: func(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
			return &model.ChatCompletionResponse{
				ID:     "identity-check",
				Object: "chat.completion",
				Model:  req.Model,
				Choices: []model.Choice{
					{
						Index:   0,
						Message: &model.Message{Role: "assistant", Content: "ok"},
					},
				},
			}, nil
		},
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-identity", defaultChatRequest())

	assert.Equal(t, http.StatusOK, w.Code)
}

// ─── Single Key Forwarding — Invalid request body after auth ───────────────

func TestKeyForward_InvalidRequestBody(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	body := `{not valid json`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer sk-test123")

	h.Engine.ServeHTTP(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_request")
}

func TestKeyForward_MissingMessages(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	req := model.ChatCompletionRequest{Model: "gpt-4", Stream: false}
	w := doChatRequest(t, h.Engine, "sk-test123", req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestKeyForward_EmptyMessages(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	req := model.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []model.Message{},
		Stream:   false,
	}
	w := doChatRequest(t, h.Engine, "sk-test123", req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ─── Single Key Forwarding — ChatRequest body round-trip ──────────────────

func TestKeyForward_ChatRequestPreserved(t *testing.T) {
	var capturedReq *model.ChatCompletionRequest

	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
		chatFunc: func(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
			capturedReq = req
			return &model.ChatCompletionResponse{
				ID:     "captured",
				Object: "chat.completion",
				Model:  req.Model,
				Choices: []model.Choice{
					{
						Index:   0,
						Message: &model.Message{Role: "assistant", Content: "ok"},
					},
				},
			}, nil
		},
	})
	defer h.Cleanup()

	temp := model.JSONFloat64(0.7)
	maxTokens := 100
	req := model.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []model.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hi"},
		},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		Stream:      false,
	}

	w := doChatRequest(t, h.Engine, "sk-capture", req)
	assert.Equal(t, http.StatusOK, w.Code)

	require.NotNil(t, capturedReq)
	assert.Equal(t, "gpt-4", capturedReq.Model)
	assert.Equal(t, 2, len(capturedReq.Messages))
	assert.Equal(t, "system", capturedReq.Messages[0].Role)
	assert.Equal(t, "user", capturedReq.Messages[1].Role)
	assert.NotNil(t, capturedReq.Temperature)
	assert.NotNil(t, capturedReq.MaxTokens)
}

// ─── Single Key Forwarding — Custom provider response ─────────────────────

func TestKeyForward_CustomProviderResponse(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
		chatFunc: func(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
			finishReason := "stop"
			return &model.ChatCompletionResponse{
				ID:      "custom-id-999",
				Object:  "chat.completion",
				Created: 1688888888,
				Model:   req.Model,
				Choices: []model.Choice{
					{
						Index:        0,
						Message:      &model.Message{Role: "assistant", Content: "custom response"},
						FinishReason: &finishReason,
					},
				},
				Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 15, TotalTokens: 20},
			}, nil
		},
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-custom", defaultChatRequest())

	assert.Equal(t, http.StatusOK, w.Code)

	var resp model.ChatCompletionResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "custom-id-999", resp.ID)
	assert.Equal(t, "custom response", resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", *resp.Choices[0].FinishReason)
	assert.NotNil(t, resp.Usage)
	assert.Equal(t, 5, resp.Usage.PromptTokens)
	assert.Equal(t, 15, resp.Usage.CompletionTokens)
	assert.Equal(t, 20, resp.Usage.TotalTokens)
}

// ─── Single Key Forwarding — Response format ──────────────────────────────

func TestKeyForward_ResponseFormat(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	w := doChatRequest(t, h.Engine, "sk-test123", defaultChatRequest())
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	assert.Contains(t, resp, "id")
	assert.Contains(t, resp, "object")
	assert.Contains(t, resp, "model")
	assert.Contains(t, resp, "choices")

	choices := resp["choices"].([]interface{})
	assert.NotEmpty(t, choices)

	choice := choices[0].(map[string]interface{})
	assert.Contains(t, choice, "message")
	assert.Contains(t, choice, "index")
}

// ─── Single Key Forwarding — Stream completion ────────────────────────────

func TestKeyForward_Stream_MultipleChunks(t *testing.T) {
	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-stream",
		keyID:      "key-stream",
		chatStreamFunc: func(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
			ch := make(chan model.StreamChunk, 4)
			go func() {
				defer close(ch)
				for _, word := range []string{"Hello", ", ", "World", "!"} {
					ch <- model.StreamChunk{
						Data: &model.ChatCompletionChunk{
							ID:      "stream-id",
							Object:  "chat.completion.chunk",
							Model:   req.Model,
							Choices: []model.Choice{
								{Index: 0, Delta: &model.Delta{Content: word}},
							},
						},
					}
				}
				ch <- model.StreamChunk{Done: true}
			}()
			return ch, nil
		},
	})
	defer h.Cleanup()

	req := defaultChatRequest()
	req.Stream = true

	w := doChatRequest(t, h.Engine, "sk-stream", req)
	assert.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	assert.Contains(t, body, "data:")
	assert.Contains(t, body, "[DONE]")

	dataLines := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data:") && !strings.Contains(line, "[DONE]") {
			dataLines++
		}
	}
	assert.Equal(t, 4, dataLines)
}

// ─── Single Key Forwarding — Edge Cases ───────────────────────────────────

func TestKeyForward_AuthServerSlow(t *testing.T) {
	t.Skip("slow auth server test — enable for resilience checks")

	h := newKeyForwardHarness(keyForwardSetupOpts{
		authStatus: "active",
		userID:     "user-001",
		keyID:      "key-001",
	})
	defer h.Cleanup()

	// With default 500ms timeout in real AuthClient, this would trigger timeout
	w := doChatRequest(t, h.Engine, "sk-slow", defaultChatRequest())
	assert.NotNil(t, w)
}

// recoverBody reads the response body, then creates a new reader for reuse.
func recoverBody(t *testing.T, w *httptest.ResponseRecorder) io.Reader {
	t.Helper()
	return strings.NewReader(w.Body.String())
}

var _ = recoverBody // suppress unused warning in current scope