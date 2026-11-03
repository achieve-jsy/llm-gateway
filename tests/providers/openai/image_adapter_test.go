package openai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/bailian"
	"github.com/example/llm-gateway/internal/providers/cohere"
	"github.com/example/llm-gateway/internal/providers/litellm"
	"github.com/example/llm-gateway/internal/providers/minimax"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/example/llm-gateway/internal/providers/oracle"
	"github.com/example/llm-gateway/internal/providers/vllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 Batch 1 · B1 的三类 provider 断言，以及适配器本身的出站契约
// （请求翻译、Raw 原样保留、NoRetry）。
//
// 三类划分（任务书 B1）：
//
//	① 匿名内嵌 *openai.CompatibleProvider  → 方法提升**自动**获得能力（openai / azure / openrouter）
//	② 命名字段持有 *openai.CompatibleProvider → 必须显式委派（litellm / vllm / minimax / oracle / cohere / bailian）
//	③ 两者都没有                        → 类型断言失败 → handler 映射 501
//
// ③ 无法在 openai 包的测试里断言（枚举全仓 provider 会引入循环依赖），
// 由 tests/gateway/handler/image_handler_test.go 的 501 用例覆盖。

// ── ① 匿名内嵌：方法提升自动获得能力 ─────────────────────────────────────────

func TestB1_AnonymousEmbed_AutoGainsImageProvider(t *testing.T) {
	var provider core.Provider = openai.New(providers.ProviderConfig{
		APIKey:  "test-key",
		BaseURL: "http://127.0.0.1:0/v1",
	}, providers.ProviderOptions{})

	// 方法提升：*openai.Provider 匿名内嵌 *openai.CompatibleProvider，
	// GenerateImage / EditImage 由内嵌类型提升而来，无需在 openai.go 里写任何委派。
	imageProvider, ok := provider.(core.ImageProvider)
	require.True(t, ok, "匿名内嵌 CompatibleProvider 的 provider 必须自动实现 core.ImageProvider")
	require.NotNil(t, imageProvider)
}

// ── ② 命名字段：显式委派 ──────────────────────────────────────────────────────

func TestB1_NamedField_ExplicitDelegation(t *testing.T) {
	tests := []struct {
		name   string
		desc   string
		build  func(baseURL string) core.Provider
		images []core.ImageFile
	}{
		{
			name: "litellm",
			desc: "命名字段 compatible：委派生成与编辑",
			build: func(baseURL string) core.Provider {
				return litellm.New(providers.ProviderConfig{APIKey: "k", BaseURL: baseURL}, providers.ProviderOptions{})
			},
		},
		{
			name: "vllm",
			desc: "命名字段 compatible + rootClient",
			build: func(baseURL string) core.Provider {
				return vllm.New(providers.ProviderConfig{APIKey: "k", BaseURL: baseURL}, providers.ProviderOptions{})
			},
		},
		{
			name: "minimax",
			desc: "命名字段 compatible",
			build: func(baseURL string) core.Provider {
				return minimax.New(providers.ProviderConfig{APIKey: "k", BaseURL: baseURL}, providers.ProviderOptions{})
			},
		},
		{
			name: "oracle",
			desc: "命名字段名为 compat（不是 compatible）也要委派",
			build: func(baseURL string) core.Provider {
				return oracle.New(providers.ProviderConfig{APIKey: "k", BaseURL: baseURL}, providers.ProviderOptions{})
			},
		},
		{
			name: "cohere",
			desc: "命名字段 compatible（Rerank 走原生端点，图像仍走兼容端点）",
			build: func(baseURL string) core.Provider {
				return cohere.New(providers.ProviderConfig{APIKey: "k", BaseURL: baseURL}, providers.ProviderOptions{})
			},
		},
		{
			name: "bailian",
			desc: "命名字段 compatible + nativeClient + 并发信号量",
			build: func(baseURL string) core.Provider {
				return bailian.New(providers.ProviderConfig{APIKey: "k", BaseURL: baseURL}, providers.ProviderOptions{})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var generationCalls, editCalls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/images/generations"):
					atomic.AddInt32(&generationCalls, 1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"Z2Vu"}]}`))
				case strings.HasSuffix(r.URL.Path, "/images/edits"):
					atomic.AddInt32(&editCalls, 1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"created":2,"data":[{"b64_json":"ZWRpdA=="}]}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			provider := tt.build(server.URL + "/v1")
			imageProvider, ok := provider.(core.ImageProvider)
			require.True(t, ok, "provider %s 必须显式委派从而实现 core.ImageProvider", tt.name)

			genResp, err := imageProvider.GenerateImage(context.Background(), &core.ImageGenerationRequest{
				Model:  "gpt-image-1",
				Prompt: "a red cube",
			})
			require.NoError(t, err)
			require.NotNil(t, genResp)
			assert.Equal(t, int32(1), atomic.LoadInt32(&generationCalls), "委派必须真正打到 /images/generations")
			assert.Equal(t, `{"created":1,"data":[{"b64_json":"Z2Vu"}]}`, string(genResp.Raw))

			editResp, err := imageProvider.EditImage(context.Background(), &core.ImageEditRequest{
				Model:  "gpt-image-1",
				Prompt: "make it blue",
				Images: []core.ImageFile{{FieldName: "image[]", FileName: "a.png", ContentType: "image/png", Data: []byte("PNG")}},
			})
			require.NoError(t, err)
			require.NotNil(t, editResp)
			assert.Equal(t, int32(1), atomic.LoadInt32(&editCalls), "委派必须真正打到 /images/edits")
			assert.Equal(t, `{"created":2,"data":[{"b64_json":"ZWRpdA=="}]}`, string(editResp.Raw))
		})
	}
}

// ── 适配器出站契约（B2/B3 的 provider 侧）────────────────────────────────────

func TestGenerateImage_RequestTranslationAndRawPassthrough(t *testing.T) {
	var gotBody map[string]any
	var gotContentType string
	var gotAuth string
	var calls int32
	// 上游返回体刻意带「原样回写」需要逐字节一致的形状（b64 + 未知字段）。
	upstreamBody := `{"created":1731000000,"data":[{"b64_json":"AAAABBBBCCCC","revised_prompt":"r","provider_specific_fields":{"x":1}}],"usage":{"input_tokens":11,"output_tokens":22,"total_tokens":33,"input_tokens_details":{"image_tokens":7,"text_tokens":4}},"unknown_extra":"keep-me"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		assert.Equal(t, "/v1/images/generations", r.URL.Path)
		gotContentType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamBody))
	}))
	defer server.Close()

	provider := openai.New(providers.ProviderConfig{APIKey: "sk-test", BaseURL: server.URL + "/v1"}, providers.ProviderOptions{})
	imageProvider := provider.(core.ImageProvider)

	n := 2
	compression := 80
	resp, err := imageProvider.GenerateImage(context.Background(), &core.ImageGenerationRequest{
		Model:             "gpt-image-1",
		Prompt:            "a cat",
		N:                 &n,
		Size:              "1024x1024",
		Quality:           "high",
		Background:        "transparent",
		Moderation:        "low",
		OutputFormat:      "webp",
		OutputCompression: &compression,
		User:              "u-1",
	})
	require.NoError(t, err)

	t.Run("认证头与 Content-Type", func(t *testing.T) {
		assert.Equal(t, "Bearer sk-test", gotAuth)
		assert.Contains(t, gotContentType, "application/json")
	})

	t.Run("字段白名单逐项翻译", func(t *testing.T) {
		assert.Equal(t, "gpt-image-1", gotBody["model"])
		assert.Equal(t, "a cat", gotBody["prompt"])
		assert.Equal(t, float64(2), gotBody["n"])
		assert.Equal(t, "1024x1024", gotBody["size"])
		assert.Equal(t, "high", gotBody["quality"])
		assert.Equal(t, "transparent", gotBody["background"])
		assert.Equal(t, "low", gotBody["moderation"])
		assert.Equal(t, "webp", gotBody["output_format"])
		assert.Equal(t, float64(80), gotBody["output_compression"])
		assert.Equal(t, "u-1", gotBody["user"])
	})

	t.Run("类型层没有的字段绝不外发", func(t *testing.T) {
		// core.ImageGenerationRequest 没有这些字段；即使有人改了结构体，
		// 也不允许它们出现在出站 body 里（generations 上游会 400）。
		for _, forbidden := range []string{"response_format", "style", "seed"} {
			_, exists := gotBody[forbidden]
			assert.False(t, exists, "出站 body 不应包含 %s", forbidden)
		}
	})

	t.Run("Raw 与上游响应逐字节一致", func(t *testing.T) {
		assert.Equal(t, upstreamBody, string(resp.Raw), "Raw 必须是上游原始字节，不得重新序列化")
		// 结构化解析成功（供 B6 算用量），但绝不用于回写。
		require.NotNil(t, resp.Usage)
		assert.Equal(t, 33, resp.Usage.TotalTokens)
		require.Len(t, resp.Data, 1)
		assert.Equal(t, "AAAABBBBCCCC", resp.Data[0].B64JSON)
	})

	t.Run("提交类请求至多一次", func(t *testing.T) {
		assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
	})
}

func TestGenerateImage_NoRetryOn429(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	}))
	defer server.Close()

	provider := openai.New(providers.ProviderConfig{APIKey: "k", BaseURL: server.URL + "/v1"}, providers.ProviderOptions{})
	imageProvider := provider.(core.ImageProvider)

	_, err := imageProvider.GenerateImage(context.Background(), &core.ImageGenerationRequest{
		Model: "gpt-image-1", Prompt: "x",
	})
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls),
		"提交类图像请求在 429 下必须至多发出一次（重发 = 上游再出一张图并重复计费）")

	var gwErr *core.GatewayError
	require.ErrorAs(t, err, &gwErr)
	assert.Equal(t, http.StatusTooManyRequests, gwErr.StatusCode, "上游 429 需保留状态码供 handler 透传")
	assert.Equal(t, "rate limited", gwErr.Message, "上游错误 message 需保留")
}

func TestEditImage_MultipartRebuild(t *testing.T) {
	upstreamBody := `{"created":7,"data":[{"b64_json":"ZWRpdA=="}]}`
	var (
		receivedFields  map[string]string
		receivedFiles   []core.ImageFile
		fileOrder       []string
		gotBoundary     string
		inboundBoundary = "INBOUND-BOUNDARY-MUST-CHANGE"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/images/edits", r.URL.Path)
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		assert.Equal(t, "multipart/form-data", mediaType)
		gotBoundary = params["boundary"]

		reader := multipart.NewReader(r.Body, gotBoundary)
		receivedFields = map[string]string{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			data, err := io.ReadAll(part)
			require.NoError(t, err)
			if part.FileName() != "" {
				receivedFiles = append(receivedFiles, core.ImageFile{
					FieldName:   part.FormName(),
					FileName:    part.FileName(),
					ContentType: part.Header.Get("Content-Type"),
					Data:        data,
				})
				fileOrder = append(fileOrder, part.FileName())
				continue
			}
			receivedFields[part.FormName()] = string(data)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamBody))
	}))
	defer server.Close()

	provider := openai.New(providers.ProviderConfig{APIKey: "k", BaseURL: server.URL + "/v1"}, providers.ProviderOptions{})
	imageProvider := provider.(core.ImageProvider)

	editReq := &core.ImageEditRequest{
		Model:  "gpt-image-upstream-id",
		Prompt: "join these",
		Images: []core.ImageFile{
			{FieldName: "image[]", FileName: "first.png", ContentType: "image/png", Data: []byte("FIRST")},
			{FieldName: "image[]", FileName: "second.png", ContentType: "image/png", Data: []byte("SECOND")},
			{FieldName: "image[]", FileName: "third.jpg", ContentType: "image/jpeg", Data: []byte("THIRD")},
		},
		Mask:           &core.ImageFile{FieldName: "mask", FileName: "my-mask.png", ContentType: "image/png", Data: []byte("MASK")},
		Size:           "1024x1024",
		Quality:        "high",
		ResponseFormat: "b64_json",
		InputFidelity:  "high",
		N:              intPtr(1),
	}

	resp, err := imageProvider.EditImage(context.Background(), editReq)
	require.NoError(t, err)
	assert.Equal(t, upstreamBody, string(resp.Raw))

	t.Run("boundary 必须重建（不复用入站）", func(t *testing.T) {
		require.NotEmpty(t, gotBoundary)
		assert.NotEqual(t, inboundBoundary, gotBoundary)
	})

	t.Run("model 已改写且其余文本字段保留", func(t *testing.T) {
		assert.Equal(t, "gpt-image-upstream-id", receivedFields["model"])
		assert.Equal(t, "join these", receivedFields["prompt"])
		assert.Equal(t, "1024x1024", receivedFields["size"])
		assert.Equal(t, "high", receivedFields["quality"])
		assert.Equal(t, "b64_json", receivedFields["response_format"], "edits 支持 response_format，必须透传")
		assert.Equal(t, "high", receivedFields["input_fidelity"])
		assert.Equal(t, "1", receivedFields["n"])
	})

	t.Run("文件顺序 image[] → mask 与真实 filename 全部保留", func(t *testing.T) {
		require.Len(t, receivedFiles, 4)
		assert.Equal(t, []string{"first.png", "second.png", "third.jpg", "my-mask.png"}, fileOrder)
		assert.Equal(t, "image[]", receivedFiles[0].FieldName)
		assert.Equal(t, "image/png", receivedFiles[0].ContentType)
		assert.Equal(t, []byte("FIRST"), receivedFiles[0].Data)
		assert.Equal(t, "image/jpeg", receivedFiles[2].ContentType, "客户端声明的 MIME 不得改写")
		assert.Equal(t, "mask", receivedFiles[3].FieldName)
		assert.Equal(t, "image/png", receivedFiles[3].ContentType)
		assert.Equal(t, []byte("MASK"), receivedFiles[3].Data)
	})
}

func TestEditImage_MaskDefaultsAndFieldFallbacks(t *testing.T) {
	var received []core.ImageFile
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			if part.FileName() == "" {
				continue
			}
			data, _ := io.ReadAll(part)
			received = append(received, core.ImageFile{
				FieldName:   part.FormName(),
				FileName:    part.FileName(),
				ContentType: part.Header.Get("Content-Type"),
				Data:        data,
			})
		}
		_, _ = w.Write([]byte(`{"created":1,"data":[]}`))
	}))
	defer server.Close()

	provider := openai.New(providers.ProviderConfig{APIKey: "k", BaseURL: server.URL + "/v1"}, providers.ProviderOptions{})
	imageProvider := provider.(core.ImageProvider)

	// 文件名与字段名都缺省 → 用 core.ImageDefault* 兜底（上游对字段名敏感）。
	_, err := imageProvider.EditImage(context.Background(), &core.ImageEditRequest{
		Model:  "m",
		Prompt: "p",
		Images: []core.ImageFile{{Data: []byte("RAW")}},
		Mask:   &core.ImageFile{Data: []byte("M")},
	})
	require.NoError(t, err)
	require.Len(t, received, 2)
	assert.Equal(t, "image", received[0].FieldName)
	assert.Equal(t, core.ImageDefaultImageFileName, received[0].FileName)
	assert.Equal(t, "application/octet-stream", received[0].ContentType)
	assert.Equal(t, []byte("RAW"), received[0].Data)
	assert.Equal(t, "mask", received[1].FieldName)
	assert.Equal(t, core.ImageDefaultMaskFileName, received[1].FileName)
	assert.Equal(t, []byte("M"), received[1].Data)
}

func TestEditImage_ValidationErrors(t *testing.T) {
	provider := openai.New(providers.ProviderConfig{APIKey: "k", BaseURL: "http://127.0.0.1:0/v1"}, providers.ProviderOptions{})
	imageProvider := provider.(core.ImageProvider)

	tests := []struct {
		name string
		req  *core.ImageEditRequest
	}{
		{name: "缺 model", req: &core.ImageEditRequest{Prompt: "p", Images: []core.ImageFile{{Data: []byte("x")}}}},
		{name: "缺 prompt", req: &core.ImageEditRequest{Model: "m", Images: []core.ImageFile{{Data: []byte("x")}}}},
		{name: "缺 image", req: &core.ImageEditRequest{Model: "m", Prompt: "p"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := imageProvider.EditImage(context.Background(), tt.req)
			require.Error(t, err)
			var gwErr *core.GatewayError
			require.ErrorAs(t, err, &gwErr)
			assert.Equal(t, http.StatusBadRequest, gwErr.StatusCode)
		})
	}
}

func TestGenerateImage_Upstream4xxIsPreserved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid size","type":"invalid_request_error","param":"size","code":"invalid_size"}}`))
	}))
	defer server.Close()

	provider := openai.New(providers.ProviderConfig{APIKey: "k", BaseURL: server.URL + "/v1"}, providers.ProviderOptions{})
	_, err := provider.(core.ImageProvider).GenerateImage(context.Background(), &core.ImageGenerationRequest{Model: "m", Prompt: "p"})
	require.Error(t, err)
	var gwErr *core.GatewayError
	require.ErrorAs(t, err, &gwErr)
	assert.Equal(t, http.StatusBadRequest, gwErr.StatusCode)
	assert.Equal(t, "invalid size", gwErr.Message)
	require.NotNil(t, gwErr.Body)
	assert.True(t, bytes.Contains(gwErr.Body, []byte("invalid_size")), "上游原始错误体需保留供 handler 原样透传")
}

func intPtr(v int) *int { return &v }
