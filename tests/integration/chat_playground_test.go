package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatPlayground_BackendFlow_ProviderTestChat(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")
	crypto.InitForTesting()

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"text-embedding-v3"},{"id":"qwen-chat"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"resp-int","model":"qwen-chat","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	providerResp, err := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "chat-playground-int",
		DefinitionID: "dashscope",
		Endpoint:     upstream.URL,
		Credentials:  `{"api_key":"sk-int-test"}`,
	})
	require.NoError(t, err)

	realProviderRepo := repository.NewProviderRepo(db)
	realModelRepo := repository.NewModelRepo(db)
	h := handler.NewTestHandler(realProviderRepo, realModelRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/providers/:id/test", h.TestProvider)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+providerResp.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"success":true`)
	assert.Contains(t, w.Body.String(), `"chat_success":true`)
	assert.Contains(t, w.Body.String(), `"chat_model":"qwen-chat"`)
	assert.NotContains(t, w.Body.String(), `text-embedding-v3`)
}

func TestChatPlayground_BackendFlow_PrefersConfiguredTestModel(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")
	crypto.InitForTesting()

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	var chatBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"auto-model"}]}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		chatBody = string(body)
		_, _ = w.Write([]byte(`{"id":"resp-pref","model":"configured-model","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	providerResp, err := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "chat-playground-prefers-test-model",
		DefinitionID: "dashscope",
		Endpoint:     upstream.URL,
		Credentials:  `{"api_key":"sk-int-test"}`,
		TestModel:    "configured-model",
	})
	require.NoError(t, err)

	h := handler.NewTestHandler(repository.NewProviderRepo(db), repository.NewModelRepo(db))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/providers/:id/test", h.TestProvider)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+providerResp.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"chat_model":"configured-model"`)
	assert.Contains(t, chatBody, `"model":"configured-model"`)
}

func TestChatPlayground_BackendFlow_ChatErrorPropagates(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")
	crypto.InitForTesting()

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen-chat"}]}`))
			return
		}
		http.Error(w, `{"error":{"message":"quota exceeded"}}`, http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	providerResp, err := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "chat-playground-error-flow",
		DefinitionID: "dashscope",
		Endpoint:     upstream.URL,
		Credentials:  `{"api_key":"sk-int-test"}`,
	})
	require.NoError(t, err)

	h := handler.NewTestHandler(repository.NewProviderRepo(db), repository.NewModelRepo(db))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/providers/:id/test", h.TestProvider)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+providerResp.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"chat_success":false`)
	assert.Contains(t, w.Body.String(), `quota exceeded`)
}

func TestChatPlayground_BackendFlow_ResponseContractFieldsPresent(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")
	crypto.InitForTesting()

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen-chat"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"resp-contract","model":"qwen-chat","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	providerResp, err := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "chat-playground-contract-fields",
		DefinitionID: "dashscope",
		Endpoint:     upstream.URL,
		Credentials:  `{"api_key":"sk-int-test"}`,
	})
	require.NoError(t, err)

	h := handler.NewTestHandler(repository.NewProviderRepo(db), repository.NewModelRepo(db))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/providers/:id/test", h.TestProvider)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+providerResp.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"success":true`)
	assert.Contains(t, w.Body.String(), `"status_code":200`)
	assert.Contains(t, w.Body.String(), `"model_count":1`)
	assert.Contains(t, w.Body.String(), `"chat_success":true`)
	assert.Contains(t, w.Body.String(), `"chat_model":"qwen-chat"`)
	assert.Contains(t, w.Body.String(), `"chat_response_time":`)
}

func TestChatPlayground_BackendFlow_ConnectivityOnly_NoChatFields(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")
	crypto.InitForTesting()

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	chatCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen-chat"}]}`))
			return
		}
		chatCalled = true
		_, _ = w.Write([]byte(`{"id":"resp-ignored","model":"qwen-chat","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	providerResp, err := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "chat-playground-connectivity-only",
		DefinitionID: "dashscope",
		Endpoint:     upstream.URL,
		Credentials:  `{"api_key":"sk-int-test"}`,
	})
	require.NoError(t, err)

	h := handler.NewTestHandler(repository.NewProviderRepo(db), repository.NewModelRepo(db))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/providers/:id/test", h.TestProvider)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+providerResp.ID.String()+"/test", strings.NewReader(`{"test_chat":false}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, chatCalled)
	assert.NotContains(t, w.Body.String(), `"chat_success"`)
	assert.NotContains(t, w.Body.String(), `"chat_model"`)
}

var (
	_ handler.TestProviderRepo = (*repository.ProviderRepo)(nil)
	_ handler.TestModelRepo    = (*repository.ModelRepo)(nil)
)
