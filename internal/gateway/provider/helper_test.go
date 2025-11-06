package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestURL_Patterns(t *testing.T) {
	cases := []struct {
		name string
		cfg  VendorConfig
		base string
		path string
		mdl  string
		want string
	}{
		{
			name: "standard",
			cfg:  VendorConfig{URLPattern: "standard"},
			base: "https://api.openai.com/v1/",
			path: "/chat/completions",
			want: "https://api.openai.com/v1/chat/completions",
		},
		{
			name: "azure default version",
			cfg:  VendorConfig{URLPattern: "azure"},
			base: "https://x.azure.com",
			path: "/chat/completions",
			mdl:  "gpt-4.0",
			want: "https://x.azure.com/openai/deployments/gpt-40/chat/completions?api-version=2024-02-01",
		},
		{
			name: "azure explicit version",
			cfg:  VendorConfig{URLPattern: "azure", APIVersion: "2024-09-01"},
			base: "https://x.azure.com",
			path: "/chat/completions",
			mdl:  "gpt-4o",
			want: "https://x.azure.com/openai/deployments/gpt-4o/chat/completions?api-version=2024-09-01",
		},
		{
			name: "volcengine",
			cfg:  VendorConfig{URLPattern: "volcengine"},
			base: "https://ark.volces.com/api/v3/bot/chat",
			path: "/completions",
			want: "https://ark.volces.com/api/v3/bot/chat/completions",
		},
		{
			name: "custom",
			cfg:  VendorConfig{URLPattern: "custom"},
			base: "https://custom.example.com",
			path: "/v1/chat",
			want: "https://custom.example.com/v1/chat",
		},
		{
			name: "default fallback",
			cfg:  VendorConfig{},
			base: "https://default.example.com",
			path: "/x",
			want: "https://default.example.com/x",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, BuildRequestURL(c.base, c.path, c.cfg, c.mdl))
		})
	}
}

func TestSetAuthHeader(t *testing.T) {
	tests := []struct {
		name   string
		cfg    VendorConfig
		header string
		value  string
	}{
		{"bearer default", VendorConfig{}, "Authorization", "Bearer key"},
		{"api-key", VendorConfig{AuthHeader: "api-key"}, "api-key", "key"},
		{"x-api-key", VendorConfig{AuthHeader: "x-api-key"}, "x-api-key", "key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			SetAuthHeader(req, "key", tt.cfg)
			assert.Equal(t, tt.value, req.Header.Get(tt.header))
		})
	}
}

func TestSetAuthHeader_WithExtraHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	cfg := VendorConfig{ExtraHeaders: map[string]string{"X-Trace": "abc", "X-Plain": "1"}}
	SetAuthHeader(req, "k", cfg)
	assert.Equal(t, "abc", req.Header.Get("X-Trace"))
	assert.Equal(t, "1", req.Header.Get("X-Plain"))
}

func TestOpenAICompatChat_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		resp := model.ChatCompletionResponse{ID: "ok", Object: "chat.completion", Model: "m", Created: time.Now().Unix()}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := server.Client()
	req := &model.ChatCompletionRequest{Model: "m", Messages: []model.Message{{Role: "user", Content: "hi"}}, Stream: true}
	got, err := OpenAICompatChat(client, server.URL, req, "test-key", DefaultVendorConfig, func(s int, b []byte) error {
		return errors.New("should not be called")
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", got.ID)
	// Stream 应被强制改为 false（仅复制结构体）— 原 req 仍是 true
	assert.True(t, req.Stream)
}

func TestOpenAICompatChat_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer server.Close()

	req := &model.ChatCompletionRequest{Model: "m", Messages: []model.Message{{Role: "user", Content: "hi"}}}
	_, err := OpenAICompatChat(server.Client(), server.URL, req, "k", DefaultVendorConfig, func(s int, b []byte) error {
		return errors.New("status 500")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestOpenAICompatChat_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	req := &model.ChatCompletionRequest{Model: "m", Messages: []model.Message{{Role: "user", Content: "hi"}}}
	_, err := OpenAICompatChat(server.Client(), server.URL, req, "k", DefaultVendorConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestOpenAICompatChat_NetworkError(t *testing.T) {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	req := &model.ChatCompletionRequest{Model: "m", Messages: []model.Message{{Role: "user", Content: "hi"}}}
	_, err := OpenAICompatChat(client, "http://127.0.0.1:1", req, "k", DefaultVendorConfig, nil)
	require.Error(t, err)
}

func TestOpenAICompatStream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// 一条注释 + 空行 + 一条 data + 空 data + DONE
		_, _ = w.Write([]byte(": comment line\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"chunk-1","choices":[{"index":0,"delta":{"content":"hello"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: \n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	req := &model.ChatCompletionRequest{Model: "m", Messages: []model.Message{{Role: "user", Content: "hi"}}}
	ch, err := OpenAICompatStream(server.Client(), server.URL, req, "k", DefaultVendorConfig, nil)
	require.NoError(t, err)

	var got []model.StreamChunk
	for c := range ch {
		got = append(got, c)
	}
	require.NotEmpty(t, got)
	// 最后必为 Done
	assert.True(t, got[len(got)-1].Done)
}

func TestOpenAICompatStream_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusUnauthorized)
	}))
	defer server.Close()

	req := &model.ChatCompletionRequest{Model: "m", Messages: []model.Message{{Role: "user", Content: "hi"}}}
	_, err := OpenAICompatStream(server.Client(), server.URL, req, "k", DefaultVendorConfig, func(s int, b []byte) error {
		return errors.New("unauthorized")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized")
}

func TestOpenAICompatStream_NetworkError(t *testing.T) {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	req := &model.ChatCompletionRequest{Model: "m", Messages: []model.Message{{Role: "user", Content: "hi"}}}
	_, err := OpenAICompatStream(client, "http://127.0.0.1:1", req, "k", DefaultVendorConfig, nil)
	require.Error(t, err)
}

func TestOpenAICompatStream_SkipEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// 空 choices 的 chunk 应被跳过
		_, _ = w.Write([]byte(`data: {"id":"chunk-keep-alive","choices":[]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"chunk-real","choices":[{"index":0,"delta":{"content":"x"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	req := &model.ChatCompletionRequest{Model: "m", Messages: []model.Message{{Role: "user", Content: "hi"}}}
	cfg := DefaultVendorConfig
	cfg.EmptyChoicesSkip = true
	ch, err := OpenAICompatStream(server.Client(), server.URL, req, "k", cfg, nil)
	require.NoError(t, err)

	var dataChunks int
	for c := range ch {
		if c.Data != nil {
			dataChunks++
		}
	}
	assert.Equal(t, 1, dataChunks)
}

// 行使 MockProvider 的全部方法/setter，使其覆盖率 >0
func TestMockProvider_Methods(t *testing.T) {
	p := NewMockProvider("mp")
	assert.Equal(t, "mp", p.Name())

	resp, err := p.Chat(context.Background(), &model.ChatCompletionRequest{Model: "m"})
	require.NoError(t, err)
	assert.NotNil(t, resp)

	ch, err := p.ChatStream(context.Background(), &model.ChatCompletionRequest{Model: "m"})
	require.NoError(t, err)
	for range ch {
	}

	models, err := p.ListModels(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, models)

	require.NoError(t, p.HealthCheck(context.Background()))

	// 自定义 setter
	p.SetChatFunc(func(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
		return nil, errors.New("custom")
	})
	p.SetChatStreamFunc(func(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
		return nil, errors.New("custom")
	})
	p.SetListModelsFunc(func(ctx context.Context) ([]model.ModelInfo, error) { return nil, errors.New("custom") })
	p.SetHealthCheckFunc(func(ctx context.Context) error { return errors.New("custom") })

	_, err = p.Chat(context.Background(), &model.ChatCompletionRequest{})
	assert.Error(t, err)
	_, err = p.ChatStream(context.Background(), &model.ChatCompletionRequest{})
	assert.Error(t, err)
	_, err = p.ListModels(context.Background())
	assert.Error(t, err)
	assert.Error(t, p.HealthCheck(context.Background()))
}
