package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain 在 handler 包测试中初始化加密 KeyManager（DB-less 测试桩）。
// 新版 crypto 包不再读取 ENCRYPTION_KEY 环境变量。
func TestMain(m *testing.M) {
	crypto.InitForTesting()
	os.Exit(m.Run())
}

type fakeTestProviderRepo struct {
	getByIDFn    func(id uuid.UUID) (*model.Provider, error)
	updateRTFn   func(id uuid.UUID, ms int) error
	rtCalledWith int
}

func (f *fakeTestProviderRepo) GetByID(id uuid.UUID) (*model.Provider, error) {
	if f.getByIDFn != nil {
		return f.getByIDFn(id)
	}
	return nil, nil
}

func (f *fakeTestProviderRepo) UpdateResponseTime(id uuid.UUID, ms int) error {
	f.rtCalledWith = ms
	if f.updateRTFn != nil {
		return f.updateRTFn(id, ms)
	}
	return nil
}

type fakeTestModelRepo struct{}

func (f *fakeTestModelRepo) ListAvailable() ([]model.Model, error) {
	return nil, nil
}

var (
	_ TestProviderRepo = (*fakeTestProviderRepo)(nil)
	_ TestModelRepo    = (*fakeTestModelRepo)(nil)
)

func newTestProviderRouter(repo TestProviderRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewTestHandler(repo, &fakeTestModelRepo{})
	r.POST("/api/v1/providers/:id/test", h.TestProvider)
	return r
}

// 构造一个加密了 api_key 的 provider 指针
func mkProvider(t *testing.T, endpoint, apiKey, testModel string) *model.Provider {
	t.Helper()
	creds, _ := json.Marshal(map[string]string{"api_key": apiKey})
	enc, err := crypto.Encrypt(string(creds))
	require.NoError(t, err)
	return &model.Provider{
		ID:                   uuid.New(),
		Endpoint:             endpoint,
		EncryptedCredentials: enc,
		TestModel:            testModel,
	}
}

func TestTestProvider_InvalidID(t *testing.T) {
	repo := &fakeTestProviderRepo{}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/not-a-uuid/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_request")
}

func TestTestProvider_NotFound(t *testing.T) {
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return nil, assertErr("not found")
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+uuid.New().String()+"/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

type assertErr string

func (a assertErr) Error() string { return string(a) }

func TestTestProvider_DecryptError(t *testing.T) {
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return &model.Provider{ID: id, EncryptedCredentials: "!!!"}, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+uuid.New().String()+"/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal_error")
}

func TestTestProvider_ConnectFailure(t *testing.T) {
	provider := mkProvider(t, "http://127.0.0.1:1/nonexistent", "sk-test", "")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "连接失败")
}

func TestTestProvider_UpstreamNon2xx(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "HTTP 401")
}

func TestTestProvider_Connectivity_OK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, strings.HasSuffix(r.URL.Path, "/models"))
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`))
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"model_count":2`)
}

func TestTestProvider_WithChatTest_Success(t *testing.T) {
	var capturedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"only-model"}]}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		_, _ = w.Write([]byte(`{"id":"resp-1","model":"only-model","choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "only-model")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"chat_success":true`)
	assert.Contains(t, w.Body.String(), `"chat_model":"only-model"`)
	assert.Contains(t, capturedBody, `"max_tokens":1`)
}

func TestTestProvider_WithChatTest_SkipsEmbeddingModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"text-embedding-v3"},{"id":"qwen-chat"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"resp-2","model":"qwen-chat","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"chat_success":true`)
	assert.Contains(t, w.Body.String(), `"chat_model":"qwen-chat"`)
}

func TestTestProvider_WithChatTest_PrefersConfiguredTestModel(t *testing.T) {
	var usedChatModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"auto-model"}]}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		usedChatModel = string(body)
		_, _ = w.Write([]byte(`{"id":"resp-pref","model":"configured-model","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "configured-model")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"chat_model":"configured-model"`)
	assert.Contains(t, usedChatModel, `"model":"configured-model"`)
}

func TestTestProvider_WithChatTest_ModelsResponseMalformedButConfiguredModelStillWorks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"unexpected":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"resp-malformed","model":"configured-model","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "configured-model")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"chat_success":true`)
	assert.Contains(t, w.Body.String(), `"chat_model":"configured-model"`)
}

func TestTestProvider_WithChatTest_NoModelAvailable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "无可用模型")
}

func TestTestProvider_WithChatTest_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"only-model"}]}`))
			return
		}
		http.Error(w, `{"error":{"message":"quota exceeded"}}`, http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "only-model")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"chat_success":false`)
	assert.Contains(t, w.Body.String(), "quota exceeded")
}

func TestTestProvider_WithChatTest_BadResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"only-model"}]}`))
			return
		}
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "only-model")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "响应格式异常")
}

func TestTestProvider_WithChatTest_MissingID(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"m1","choices":[]}`))
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "m1")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "响应缺少 id")
}

func TestTestProvider_WithChatTest_MissingChoicesMessageStillReturnsSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"resp-ok","model":"m1","choices":[{}]}`))
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "m1")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"chat_success":true`)
	assert.Contains(t, w.Body.String(), `"chat_model":"m1"`)
}

func TestTestProvider_WithoutTestChat_DoesNotCallChatEndpoint(t *testing.T) {
	chatCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
			return
		}
		chatCalled = true
		_, _ = w.Write([]byte(`{"id":"resp","model":"m1","choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "m1")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":false}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, chatCalled)
	assert.NotContains(t, w.Body.String(), `"chat_success"`)
}

func TestTestProvider_WithChatTest_UpstreamPlainMessageFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
			return
		}
		http.Error(w, `{"message":"temporary unavailable"}`, http.StatusBadGateway)
	}))
	defer upstream.Close()

	provider := mkProvider(t, upstream.URL, "sk-test", "m1")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"chat_success":false`)
	assert.Contains(t, w.Body.String(), `temporary unavailable`)
}

func TestTestProvider_WithChatTest_NetworkError(t *testing.T) {
	// 模拟一个连通性测试通过、但 chat 调用网络失败的场景：先启动 models 服务，然后让 chat URL 指向不可达端口。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))

	provider := mkProvider(t, upstream.URL, "sk-test", "m1")
	repo := &fakeTestProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return provider, nil
	}}
	// 关闭 upstream，让后续 POST /chat/completions 失败
	r := newTestProviderRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/"+provider.ID.String()+"/test", strings.NewReader(`{"test_chat":true}`))
	req.Header.Set("Content-Type", "application/json")
	upstream.Close()
	r.ServeHTTP(w, req)
	// 连通性测试与 chat 都失败时仍返回 200，body 中含连接错误
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "连接失败")
}

// 直接覆盖 extractErrorMessage / truncate / chat 失败时的 errMsg 默认分支
func TestExtractErrorMessage_AndTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 10))
	assert.Equal(t, "abcde...", truncate("abcdefghij", 5))

	assert.Equal(t, "main error", extractErrorMessage([]byte(`{"error":{"message":"main error"}}`)))
	assert.Equal(t, "fallback", extractErrorMessage([]byte(`{"message":"fallback"}`)))
	assert.Equal(t, "no json", extractErrorMessage([]byte(`no json`)))
}
