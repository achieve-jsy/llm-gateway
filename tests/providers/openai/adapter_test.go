package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFakeProvider 创建一个指向 fakeServer 的 OpenAI Provider，用于单元测试
func newFakeProvider(t *testing.T, serverURL string) core.Provider {
	t.Helper()
	p := openai.New(providers.ProviderConfig{
		APIKey:  "test-key",
		BaseURL: serverURL,
	}, providers.ProviderOptions{})
	return p
}

func TestChatCompletion_正常请求(t *testing.T) {
	tests := []struct {
		name        string
		description string
		model       string
		messages    []core.Message
		wantID      string
		wantModel   string
		wantContent string
	}{
		{
			name:        "标准GPT-4请求",
			description: "发送包含一条 user 消息的请求，上游返回正常 ChatResponse，验证响应字段正确映射",
			model:       "gpt-4",
			messages:    []core.Message{{Role: "user", Content: "Hello!"}},
			wantID:      "chatcmpl-123",
			wantModel:   "gpt-4",
			wantContent: "Hello! How can I help you?",
		},
		{
			name:        "多轮消息请求",
			description: "发送包含 system + user 消息的多轮请求，验证请求体正确传递且响应正常",
			model:       "gpt-4o",
			messages: []core.Message{
				{Role: "system", Content: "You are a helpful assistant."},
				{Role: "user", Content: "What is Go?"},
			},
			wantID:      "chatcmpl-456",
			wantModel:   "gpt-4o",
			wantContent: "Go is a programming language.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/chat/completions", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

				// 验证请求体包含正确的 model
				var body map[string]any
				err := json.NewDecoder(r.Body).Decode(&body)
				require.NoError(t, err)
				assert.Equal(t, tt.model, body["model"])

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]any{
					"id":      tt.wantID,
					"object":  "chat.completion",
					"model":   tt.wantModel,
					"created": 1700000000,
					"choices": []map[string]any{
						{
							"index": 0,
							"message": map[string]any{
								"role":    "assistant",
								"content": tt.wantContent,
							},
							"finish_reason": "stop",
						},
					},
					"usage": map[string]any{
						"prompt_tokens":     10,
						"completion_tokens": 5,
						"total_tokens":      15,
					},
				})
			}))
			defer fakeServer.Close()

			p := newFakeProvider(t, fakeServer.URL)
			resp, err := p.ChatCompletion(context.Background(), &core.ChatRequest{
				Model:    tt.model,
				Messages: tt.messages,
			})

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.wantID, resp.ID)
			assert.Equal(t, tt.wantModel, resp.Model)
			require.Len(t, resp.Choices, 1)
			assert.Equal(t, "assistant", resp.Choices[0].Message.Role)
			assert.Equal(t, tt.wantContent, resp.Choices[0].Message.Content)
			assert.Equal(t, "stop", resp.Choices[0].FinishReason)
			assert.Equal(t, 10, resp.Usage.PromptTokens)
			assert.Equal(t, 5, resp.Usage.CompletionTokens)
			assert.Equal(t, 15, resp.Usage.TotalTokens)
		})
	}
}

func TestChatCompletion_NilRequest(t *testing.T) {
	// 测试目的：传入 nil ChatRequest，应返回 invalid_request_error
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("不应该发送请求到上游")
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	resp, err := p.ChatCompletion(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)

	var gatewayErr *core.GatewayError
	require.ErrorAs(t, err, &gatewayErr)
	assert.Equal(t, core.ErrorTypeInvalidRequest, gatewayErr.Type)
}

func TestChatCompletion_上游错误映射(t *testing.T) {
	tests := []struct {
		name           string
		description    string
		statusCode     int
		responseBody   map[string]any
		wantErrorType  core.ErrorType
		wantStatusCode int
	}{
		{
			name:        "401认证失败",
			description: "上游返回 401 Unauthorized，应映射为 authentication_error",
			statusCode:  http.StatusUnauthorized,
			responseBody: map[string]any{
				"error": map[string]any{
					"message": "Incorrect API key provided",
					"type":    "invalid_request_error",
					"code":    "invalid_api_key",
				},
			},
			wantErrorType:  core.ErrorTypeAuthentication,
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name:        "429速率限制",
			description: "上游返回 429 Too Many Requests，应映射为 rate_limit_error",
			statusCode:  http.StatusTooManyRequests,
			responseBody: map[string]any{
				"error": map[string]any{
					"message": "Rate limit reached for requests",
					"type":    "rate_limit_error",
				},
			},
			wantErrorType:  core.ErrorTypeRateLimit,
			wantStatusCode: http.StatusTooManyRequests,
		},
		{
			name:        "500服务端错误",
			description: "上游返回 500 Internal Server Error，应映射为 provider_error",
			statusCode:  http.StatusInternalServerError,
			responseBody: map[string]any{
				"error": map[string]any{
					"message": "The server had an error",
					"type":    "server_error",
				},
			},
			wantErrorType:  core.ErrorTypeProvider,
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name:        "503服务不可用",
			description: "上游返回 503 Service Unavailable，应映射为 provider_error 并保留原始状态码",
			statusCode:  http.StatusServiceUnavailable,
			responseBody: map[string]any{
				"error": map[string]any{
					"message": "Service is temporarily unavailable",
					"type":    "server_error",
				},
			},
			wantErrorType:  core.ErrorTypeProvider,
			wantStatusCode: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				json.NewEncoder(w).Encode(tt.responseBody)
			}))
			defer fakeServer.Close()

			p := newFakeProvider(t, fakeServer.URL)
			resp, err := p.ChatCompletion(context.Background(), &core.ChatRequest{
				Model:    "gpt-4",
				Messages: []core.Message{{Role: "user", Content: "Hello"}},
			})

			require.Error(t, err)
			assert.Nil(t, resp)

			var gatewayErr *core.GatewayError
			require.ErrorAs(t, err, &gatewayErr)
			assert.Equal(t, tt.wantErrorType, gatewayErr.Type)
			assert.Equal(t, tt.wantStatusCode, gatewayErr.StatusCode)
		})
	}
}

func TestStreamChatCompletion_正常流式请求(t *testing.T) {
	// 测试目的：发送 stream=true 请求，验证返回有效的 io.ReadCloser 且上游收到 stream=true
	sseData := `data: {"id":"chatcmpl-stream","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}

data: {"id":"chatcmpl-stream","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":"stop"}]}

data: [DONE]

`
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		// 验证请求体中 stream=true
		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, true, body["stream"])

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseData))
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	stream, err := p.StreamChatCompletion(context.Background(), &core.ChatRequest{
		Model:    "gpt-4",
		Messages: []core.Message{{Role: "user", Content: "Hi"}},
	})

	require.NoError(t, err)
	require.NotNil(t, stream)
	defer stream.Close()

	// 读取流内容，验证非空
	data, err := io.ReadAll(stream)
	require.NoError(t, err)
	assert.Contains(t, string(data), "chatcmpl-stream")
	assert.Contains(t, string(data), "[DONE]")
}

func TestStreamChatCompletion_NilRequest(t *testing.T) {
	// 测试目的：传入 nil ChatRequest 应立即返回错误
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("不应该发送请求到上游")
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	stream, err := p.StreamChatCompletion(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, stream)

	var gatewayErr *core.GatewayError
	require.ErrorAs(t, err, &gatewayErr)
	assert.Equal(t, core.ErrorTypeInvalidRequest, gatewayErr.Type)
}

func TestListModels_正常请求(t *testing.T) {
	// 测试目的：调用 ListModels，上游返回模型列表，验证响应正确解析
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/models", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"id":       "gpt-4",
					"object":   "model",
					"owned_by": "openai",
					"created":  1700000000,
				},
				{
					"id":       "gpt-3.5-turbo",
					"object":   "model",
					"owned_by": "openai",
					"created":  1680000000,
				},
			},
		})
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	resp, err := p.ListModels(context.Background())

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "list", resp.Object)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, "gpt-4", resp.Data[0].ID)
	assert.Equal(t, "model", resp.Data[0].Object)
	assert.Equal(t, "openai", resp.Data[0].OwnedBy)
	assert.Equal(t, "gpt-3.5-turbo", resp.Data[1].ID)
}

func TestListModels_上游返回错误(t *testing.T) {
	// 测试目的：ListModels 时上游返回 500，应映射为 provider_error
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Internal server error",
				"type":    "server_error",
			},
		})
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	resp, err := p.ListModels(context.Background())

	require.Error(t, err)
	assert.Nil(t, resp)

	var gatewayErr *core.GatewayError
	require.ErrorAs(t, err, &gatewayErr)
	assert.Equal(t, core.ErrorTypeProvider, gatewayErr.Type)
}

func TestEmbeddings_正常请求(t *testing.T) {
	// 测试目的：发送 Embeddings 请求，上游返回正常 EmbeddingResponse，验证响应正确映射
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/embeddings", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "text-embedding-ada-002", body["model"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  "text-embedding-ada-002",
			"data": []map[string]any{
				{
					"object":    "embedding",
					"embedding": []float64{0.1, 0.2, 0.3, 0.4},
					"index":     0,
				},
			},
			"usage": map[string]any{
				"prompt_tokens": 4,
				"total_tokens":  4,
			},
		})
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	resp, err := p.Embeddings(context.Background(), &core.EmbeddingRequest{
		Model: "text-embedding-ada-002",
		Input: "hello world",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "list", resp.Object)
	assert.Equal(t, "text-embedding-ada-002", resp.Model)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "embedding", resp.Data[0].Object)
	assert.Equal(t, 0, resp.Data[0].Index)
	assert.Equal(t, 4, resp.Usage.PromptTokens)
	assert.Equal(t, 4, resp.Usage.TotalTokens)
}

func TestEmbeddings_NilRequest(t *testing.T) {
	// 测试目的：传入 nil EmbeddingRequest，应返回 invalid_request_error
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("不应该发送请求到上游")
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	resp, err := p.Embeddings(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)

	var gatewayErr *core.GatewayError
	require.ErrorAs(t, err, &gatewayErr)
	assert.Equal(t, core.ErrorTypeInvalidRequest, gatewayErr.Type)
}

func TestNew_使用ProviderConfig创建(t *testing.T) {
	// 测试目的：验证通过 openai.New 创建的 Provider 能正确使用自定义 BaseURL
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/models", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{},
		})
	}))
	defer fakeServer.Close()

	p := openai.New(providers.ProviderConfig{
		APIKey:  "custom-key",
		BaseURL: fakeServer.URL,
	}, providers.ProviderOptions{})

	resp, err := p.ListModels(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "list", resp.Object)
}

func TestRegistration_类型正确(t *testing.T) {
	// 测试目的：验证 openai.Registration 的 Type 和 New 构造函数不为空
	assert.Equal(t, "openai", openai.Registration.Type)
	assert.NotNil(t, openai.Registration.New)
}

func TestChatCompletion_模型名回填(t *testing.T) {
	// 测试目的：当上游响应中 model 字段为空时，Provider 应回填请求中的 model 字段
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// 响应中故意不设置 model 字段
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-noop",
			"object":  "chat.completion",
			"model":   "",
			"created": 1700000000,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "test",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     1,
				"completion_tokens": 1,
				"total_tokens":      2,
			},
		})
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	resp, err := p.ChatCompletion(context.Background(), &core.ChatRequest{
		Model:    "gpt-4-turbo",
		Messages: []core.Message{{Role: "user", Content: "Hi"}},
	})

	require.NoError(t, err)
	// 上游返回空 model 时，CompatibleProvider 回填请求中的 model
	assert.Equal(t, "gpt-4-turbo", resp.Model)
}

func TestListModels_Object字段标准化(t *testing.T) {
	// 测试目的：当上游 ListModels 响应缺少 object 字段时，Provider 应补全为 "list"/"model"
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// 故意不设置 object 字段
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":       "local-model",
					"owned_by": "custom",
				},
			},
		})
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	resp, err := p.ListModels(context.Background())

	require.NoError(t, err)
	require.NotNil(t, resp)
	// normalizeModelsResponse 应补全缺失的 object 字段
	assert.Equal(t, "list", resp.Object)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "model", resp.Data[0].Object)
}
