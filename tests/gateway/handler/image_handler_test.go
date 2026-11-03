package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 Batch 1 · B2/B3/B4/B5/B6 的 handler 侧行为：
//
//	B2 组名改写 / 直连模型 403 / **响应原样回写**
//	B3 multipart 重建（保序、filename、只改写 model）
//	B5 不支持参数 400 + 替代建议 / 上游错误映射 / 501
//	B6 计价闸门（无价 402）+ 媒体用量精确类型写入
//
// 测试放在 tests/（防护栏 Q3），用外部测试包，只能依赖导出面。

// ── 假实现 ───────────────────────────────────────────────────────────────────

// fakeImageProvider 记录调用并返回可控响应（用于断言原样回写与用量写入）。
//
// 内嵌 *plainProvider 只是为了补齐 core.Provider 的其余方法（图像 handler 不会调用它们）；
// 真正被调用的是下面两个图像方法。
type fakeImageProvider struct {
	*plainProvider

	genResp  *core.ImageGenerationResponse
	genErr   error
	editResp *core.ImageGenerationResponse
	editErr  error

	genCalls  int
	editCalls int
	// seenCtx 记录 provider 看到的 context（用于断言 NoRetry 标记透传）。
	seenCtx context.Context
	// lastGenReq / lastEditReq 记录归一请求（断言 model 已被改写）。
	lastGenReq  *core.ImageGenerationRequest
	lastEditReq *core.ImageEditRequest
}

func newFakeImageProvider() *fakeImageProvider {
	return &fakeImageProvider{plainProvider: &plainProvider{}}
}

func (f *fakeImageProvider) GenerateImage(ctx context.Context, req *core.ImageGenerationRequest) (*core.ImageGenerationResponse, error) {
	f.genCalls++
	f.seenCtx = ctx
	if req != nil {
		cp := *req
		f.lastGenReq = &cp
	}
	return f.genResp, f.genErr
}

func (f *fakeImageProvider) EditImage(ctx context.Context, req *core.ImageEditRequest) (*core.ImageGenerationResponse, error) {
	f.editCalls++
	f.seenCtx = ctx
	if req != nil {
		cp := *req
		f.lastEditReq = &cp
	}
	return f.editResp, f.editErr
}

// plainProvider 只实现 core.Provider，**不**实现 core.ImageProvider：
// 用于断言第三类 provider 走 501 not_implemented。
type plainProvider struct{}

func (p *plainProvider) ChatCompletion(context.Context, *core.ChatRequest) (*core.ChatResponse, error) {
	return nil, nil
}
func (p *plainProvider) StreamChatCompletion(context.Context, *core.ChatRequest) (io.ReadCloser, error) {
	return nil, nil
}
func (p *plainProvider) ListModels(context.Context) (*core.ModelsResponse, error) { return nil, nil }
func (p *plainProvider) Responses(context.Context, *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return nil, nil
}
func (p *plainProvider) StreamResponses(context.Context, *core.ResponsesRequest) (io.ReadCloser, error) {
	return nil, nil
}
func (p *plainProvider) Embeddings(context.Context, *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return nil, nil
}
func (p *plainProvider) Rerank(context.Context, *core.RerankRequest) (*core.RerankResponse, error) {
	return nil, nil
}

// fakeImageScheduler 是 orchestrator.Scheduler 的最小假实现（组内只有一个成员）。
type fakeImageScheduler struct {
	decision *scheduler.RouteDecision
}

func (f *fakeImageScheduler) Route(_ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	if f.decision == nil {
		return nil, core.NewNotFoundError("group not found")
	}
	return f.decision, nil
}

func (f *fakeImageScheduler) Failover(_, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, core.NewNotFoundError("no failover")
}

func (f *fakeImageScheduler) FailoverExcluding(_ string, _ []string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, core.NewNotFoundError("no failover")
}

func (f *fakeImageScheduler) GetGroupConfig(_ string) *scheduler.GroupRoutingConfig { return nil }
func (f *fakeImageScheduler) NotifyFailure(_ string)                                {}
func (f *fakeImageScheduler) SetCooldown(_ string, _ time.Duration)                 {}
func (f *fakeImageScheduler) PersistSticky(_ context.Context, _, _ string, _ *scheduler.RouteDecision) {
}
func (f *fakeImageScheduler) UpdateSticky(_ context.Context, _, _, _, _ string) bool { return false }

// newImageDispatcher 构造「组名 → 上游模型」的真实调度器（复用 embeddings 测试的范式）。
func newImageDispatcher(groupName, routedModel, channelType string) *orchestrator.GroupAwareDispatcher {
	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{groupName})
	sch := &fakeImageScheduler{decision: &scheduler.RouteDecision{
		GroupName:    groupName,
		ModelID:      routedModel + "-uuid",
		ModelName:    routedModel,
		ModelType:    "image",
		ChannelType:  channelType,
		ProviderName: channelType,
	}}
	return orchestrator.NewGroupAwareDispatcher(sch, nc)
}

func newImageTestRouter(h *handler.ImageHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/v1/images/generations", h.HandleImageGeneration)
	router.POST("/v1/images/edits", h.HandleImageEdit)
	return router
}

// ── B2：组名改写、403、原样回写 ──────────────────────────────────────────────

func TestImageHandler_Generations_RewritesGroupNameAndPassesRawThrough(t *testing.T) {
	// 上游响应刻意包含 b64 + 未知字段 + 非规范空白，用于逐字节一致性断言。
	rawUpstream := []byte("{\"created\":1731000000,\"data\":[{\"b64_json\":\"QUJD\"}],\"unknown\":\"keep\"}\n")
	provider := &fakeImageProvider{plainProvider: &plainProvider{}, genResp: &core.ImageGenerationResponse{Raw: rawUpstream}}

	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("image-group", "gpt-image-upstream", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)
	router := newImageTestRouter(h)

	body := `{"model":"image-group","prompt":"a cat","size":"1024x1024"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, rawUpstream, w.Body.Bytes(), "响应必须与上游字节完全一致（禁止重新序列化）")
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

	require.NotNil(t, provider.lastGenReq)
	assert.Equal(t, "gpt-image-upstream", provider.lastGenReq.Model, "model 必须被改写为上游模型 ID")

	// 提交类请求：provider 必须看到 NoRetry 标记（A6/B7 同源风险的另一半）。
	assert.True(t, llmclient.IsNoRetry(provider.seenCtx), "图像提交请求必须带 NoRetry 标记")
}

func TestImageHandler_Generations_RejectsDirectModelID(t *testing.T) {
	provider := &fakeImageProvider{plainProvider: &plainProvider{}, genResp: &core.ImageGenerationResponse{Raw: []byte(`{}`)}}
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("image-group", "gpt-image-upstream", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)
	router := newImageTestRouter(h)

	body := `{"model":"gpt-image-1","prompt":"a cat"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code, "直连模型 ID 必须被拒（不存在的模型名也走同一分支）")
	assert.Equal(t, 0, provider.genCalls, "被拒的请求不得触达上游适配器")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, "direct_model_not_allowed", errObj["code"])
	assert.Equal(t, "model", errObj["param"])
}

func TestImageHandler_Generations_EmptyModelIs400(t *testing.T) {
	provider := newFakeImageProvider()
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("image-group", "gpt-image-upstream", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)
	router := newImageTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"prompt":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 0, provider.genCalls)
}

func TestImageHandler_Generations_InvalidJSONIs400(t *testing.T) {
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("image-group", "gpt-image-upstream", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return newFakeImageProvider() }),
	)
	router := newImageTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── B5：不支持参数 → 400 + 替代建议 ──────────────────────────────────────────

func TestImageHandler_Generations_UnsupportedParams(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantParam  string
		wantWords  []string
		wantReject bool
	}{
		{
			name:       "response_format 被拒并建议 output_format",
			body:       `{"model":"g","prompt":"p","response_format":"b64_json"}`,
			wantParam:  "response_format",
			wantWords:  []string{"output_format", "b64_json"},
			wantReject: true,
		},
		{
			name:       "style 被拒（仅 dall-e-3）",
			body:       `{"model":"g","prompt":"p","style":"vivid"}`,
			wantParam:  "style",
			wantReject: true,
		},
		{
			name:       "seed 被拒",
			body:       `{"model":"g","prompt":"p","seed":42}`,
			wantParam:  "seed",
			wantReject: true,
		},
		{
			name:       "null 值不算传参（SDK 会把未设置字段序列化成 null）",
			body:       `{"model":"g","prompt":"p","response_format":null,"style":null,"seed":null}`,
			wantReject: false,
		},
		{
			name:       "白名单参数全部放行",
			body:       `{"model":"g","prompt":"p","n":1,"size":"1024x1024","quality":"high","background":"opaque","moderation":"auto","output_format":"png","output_compression":50,"user":"u"}`,
			wantReject: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeImageProvider{plainProvider: &plainProvider{}, genResp: &core.ImageGenerationResponse{Raw: []byte(`{"created":1,"data":[]}`)}}
			h := handler.NewImageHandler(
				handler.WithImageGroupDispatcher(newImageDispatcher("g", "up", "openai")),
				handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
			)
			router := newImageTestRouter(h)

			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if !tt.wantReject {
				assert.Equal(t, http.StatusOK, w.Code)
				assert.Equal(t, 1, provider.genCalls)
				// 类型层没有的字段绝不能出现在出站 JSON 里（provider 拿到的是结构体，天然没有）。
				require.NotNil(t, provider.lastGenReq)
				return
			}

			require.Equal(t, http.StatusBadRequest, w.Code, "不支持的参数必须 400，不得静默丢弃")
			assert.Equal(t, 0, provider.genCalls, "参数校验必须在调上游之前")

			var resp map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			errObj := resp["error"].(map[string]any)
			assert.Equal(t, "invalid_request_error", errObj["code"])
			assert.Equal(t, tt.wantParam, errObj["param"])
			message, _ := errObj["message"].(string)
			for _, word := range tt.wantWords {
				assert.Contains(t, message, word)
			}
		})
	}
}

func TestImageHandler_Edit_ResponseFormatIsSupported(t *testing.T) {
	// edits 支持 response_format（设计 §3.3），不得按 generations 的规则误拒。
	provider := &fakeImageProvider{plainProvider: &plainProvider{}, editResp: &core.ImageGenerationResponse{Raw: []byte(`{"created":1,"data":[]}`)}}
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("g", "up", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)
	router := newImageTestRouter(h)

	body, contentType := buildMultipartBody(t, []multipartField{
		{name: "model", value: "g"},
		{name: "prompt", value: "edit it"},
		{name: "response_format", value: "b64_json"},
		{name: "style", value: "vivid"},
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// style 在 edits 上同样不支持 → 400（但 response_format 不被点名）。
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 0, provider.editCalls)
	assert.Contains(t, w.Body.String(), "style")
	assert.NotContains(t, w.Body.String(), "response_format")
}

// ── B3：multipart 解析与重建 ─────────────────────────────────────────────────

type multipartField struct {
	name  string
	value string
}

type multipartFile struct {
	fieldName   string
	fileName    string
	contentType string
	data        string
}

func buildMultipartBody(t *testing.T, fields []multipartField, files []multipartFile) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, f := range fields {
		require.NoError(t, writer.WriteField(f.name, f.value))
	}
	for _, f := range files {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="`+f.fieldName+`"; filename="`+f.fileName+`"`)
		header.Set("Content-Type", f.contentType)
		part, err := writer.CreatePart(header)
		require.NoError(t, err)
		_, err = part.Write([]byte(f.data))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return &buf, writer.FormDataContentType()
}

func TestImageHandler_Edit_RebuildsMultipartInOrder(t *testing.T) {
	provider := &fakeImageProvider{plainProvider: &plainProvider{}, editResp: &core.ImageGenerationResponse{Raw: []byte(`{"created":9,"data":[{"b64_json":"QQ=="}]}`)}}
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("image-group", "gpt-image-upstream", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)
	router := newImageTestRouter(h)

	body, contentType := buildMultipartBody(t, []multipartField{
		{name: "model", value: "image-group"},
		{name: "prompt", value: "blend"},
		{name: "size", value: "1024x1024"},
	}, []multipartFile{
		{fieldName: "image[]", fileName: "first.png", contentType: "image/png", data: "FIRST"},
		{fieldName: "image[]", fileName: "second.png", contentType: "image/png", data: "SECOND"},
		{fieldName: "mask", fileName: "real-mask.png", contentType: "image/png", data: "MASK"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	assert.Equal(t, `{"created":9,"data":[{"b64_json":"QQ=="}]}`, w.Body.String())

	require.NotNil(t, provider.lastEditReq)
	assert.Equal(t, "gpt-image-upstream", provider.lastEditReq.Model, "model 必须改写为上游模型 ID")
	assert.Equal(t, "blend", provider.lastEditReq.Prompt)
	assert.Equal(t, "1024x1024", provider.lastEditReq.Size)

	require.Len(t, provider.lastEditReq.Images, 2, "两张 image[] 必须保序进入 Images")
	assert.Equal(t, "first.png", provider.lastEditReq.Images[0].FileName)
	assert.Equal(t, "FIRST", string(provider.lastEditReq.Images[0].Data))
	assert.Equal(t, "second.png", provider.lastEditReq.Images[1].FileName)
	assert.Equal(t, "SECOND", string(provider.lastEditReq.Images[1].Data))
	require.NotNil(t, provider.lastEditReq.Mask, "mask 必须独立成 Mask，不能混进 Images")
	assert.Equal(t, "real-mask.png", provider.lastEditReq.Mask.FileName)
	assert.Equal(t, "image/png", provider.lastEditReq.Mask.ContentType)
}

func TestParseImageEditForm_PreservesOrderAndFieldNames(t *testing.T) {
	body, contentType := buildMultipartBody(t, []multipartField{
		{name: "model", value: "grp"},
		{name: "prompt", value: "p"},
		{name: "input_fidelity", value: "high"},
	}, []multipartFile{
		{fieldName: "image", fileName: "single.png", contentType: "image/png", data: "A"},
		{fieldName: "image[]", fileName: "b.png", contentType: "image/png", data: "B"},
		{fieldName: "image[]", fileName: "c.png", contentType: "image/png", data: "C"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	req.Header.Set("Content-Type", contentType)

	form, err := handler.ParseImageEditForm(req)
	require.NoError(t, err)
	assert.Equal(t, "grp", form.Model())
	assert.Equal(t, "p", form.Prompt())
	assert.Equal(t, "high", form.Fields()["input_fidelity"])

	editReq := form.ToEditRequest("upstream-model")
	assert.Equal(t, "upstream-model", editReq.Model)
	require.Len(t, editReq.Images, 3)
	assert.Equal(t, []string{"single.png", "b.png", "c.png"}, []string{
		editReq.Images[0].FileName, editReq.Images[1].FileName, editReq.Images[2].FileName,
	}, "混合 image / image[] 时顺序必须原样保持")
	assert.Equal(t, "image", editReq.Images[0].FieldName)
	assert.Equal(t, "image[]", editReq.Images[1].FieldName)
	assert.Nil(t, editReq.Mask)
}

func TestParseImageEditForm_RejectsNonMultipart(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(`{"model":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	_, err := handler.ParseImageEditForm(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multipart/form-data")
}

func TestImageHandler_Edit_RequiresImage(t *testing.T) {
	provider := newFakeImageProvider()
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("grp", "up", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)
	router := newImageTestRouter(h)

	body, contentType := buildMultipartBody(t, []multipartField{
		{name: "model", value: "grp"},
		{name: "prompt", value: "p"},
	}, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "image")
	assert.Equal(t, 0, provider.editCalls)
}

// ── B5：上游错误映射与 501 ───────────────────────────────────────────────────

func TestImageHandler_UpstreamErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantBody   string
	}{
		{
			name:       "上游 4xx 原样透传（含原始错误体）",
			err:        core.NewInvalidRequestErrorWithStatus(http.StatusBadRequest, "invalid size", nil).WithCode("invalid_size"),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_size",
		},
		{
			name:       "上游 401 透传（区分客户端 Key 401 与上游凭证 401）",
			err:        core.NewAuthenticationError("openai", "incorrect api key"),
			wantStatus: http.StatusUnauthorized,
			wantCode:   "authentication_error",
		},
		{
			name:       "上游 5xx 归一为 502 upstream_error",
			err:        core.NewProviderError("openai", http.StatusInternalServerError, "boom", nil),
			wantStatus: http.StatusBadGateway,
			wantCode:   "upstream_error",
			wantBody:   "boom",
		},
		{
			name:       "上游 503 同样归一为 502",
			err:        core.NewProviderError("openai", http.StatusServiceUnavailable, "overloaded", nil),
			wantStatus: http.StatusBadGateway,
			wantCode:   "upstream_error",
		},
		{
			name:       "ErrMediaNotSupported 映射为 501",
			err:        core.ErrMediaNotSupported,
			wantStatus: http.StatusNotImplemented,
			wantCode:   "not_implemented",
		},
		{
			name:       "普通 error 归一为 502",
			err:        io.ErrUnexpectedEOF,
			wantStatus: http.StatusBadGateway,
			wantCode:   "upstream_error",
		},
		{
			name:       "上游超时 → 504",
			err:        context.DeadlineExceeded,
			wantStatus: http.StatusGatewayTimeout,
			wantCode:   "upstream_timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeImageProvider{plainProvider: &plainProvider{}, genErr: tt.err}
			h := handler.NewImageHandler(
				handler.WithImageGroupDispatcher(newImageDispatcher("grp", "up", "openai")),
				handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
			)
			router := newImageTestRouter(h)

			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations",
				strings.NewReader(`{"model":"grp","prompt":"p"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			var resp map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			errObj := resp["error"].(map[string]any)
			assert.Equal(t, tt.wantCode, errObj["code"])
			if tt.wantBody != "" {
				assert.Contains(t, errObj["message"], tt.wantBody)
			}
		})
	}
}

func TestImageHandler_ProviderWithoutImageCapability_501(t *testing.T) {
	// 第三类 provider：既无匿名内嵌、也无命名字段委派 → 类型断言失败 → 501。
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("grp", "up", "gemini")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return &plainProvider{} }),
	)
	router := newImageTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations",
		strings.NewReader(`{"model":"grp","prompt":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotImplemented, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, "not_implemented", errObj["code"])
}

func TestImageHandler_NoResolverConfigured_501(t *testing.T) {
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("grp", "up", "openai")),
	)
	router := newImageTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations",
		strings.NewReader(`{"model":"grp","prompt":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestImageHandler_NoGroupDispatch_403(t *testing.T) {
	// 未启用模型组：图像链路没有可用语义，明确 403 而不是静默直连上游。
	provider := newFakeImageProvider()
	h := handler.NewImageHandler(
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)
	router := newImageTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, provider.genCalls)
}

// ── B6：计价闸门与媒体用量 ───────────────────────────────────────────────────

func TestImageHandler_UnpricedModel_402BeforeUpstream(t *testing.T) {
	// 价表里没有该模型 → MediaPriced=false → 闸门写 402，**且不得触达上游**。
	middleware.ConfigureMediaPricingGate(middleware.MediaPricingGateConfig{
		RequirePricing: true,
		Resolver: gwobs.PricingResolverFunc(func(string) (core.ModelPricing, bool) {
			return core.ModelPricing{}, false
		}),
	})
	t.Cleanup(func() {
		middleware.ConfigureMediaPricingGate(middleware.MediaPricingGateConfig{RequirePricing: false})
	})

	provider := &fakeImageProvider{plainProvider: &plainProvider{}, genResp: &core.ImageGenerationResponse{Raw: []byte(`{}`)}}
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("grp", "up", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)
	router := newImageTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations",
		strings.NewReader(`{"model":"grp","prompt":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.Equal(t, 0, provider.genCalls, "无价必须在调上游之前就拒绝（否则已经产生上游费用）")
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, "media_pricing_missing", errObj["code"])
}

func TestImageHandler_PricedModel_PassesGate(t *testing.T) {
	perImage := 0.04
	middleware.ConfigureMediaPricingGate(middleware.MediaPricingGateConfig{
		RequirePricing: true,
		Resolver: gwobs.ProviderPricingResolverFunc(func(string, string) (core.ModelPricing, bool) {
			return core.ModelPricing{PerImage: &perImage}, true
		}),
	})
	t.Cleanup(func() {
		middleware.ConfigureMediaPricingGate(middleware.MediaPricingGateConfig{RequirePricing: false})
	})

	provider := &fakeImageProvider{plainProvider: &plainProvider{}, genResp: &core.ImageGenerationResponse{Raw: []byte(`{"created":1,"data":[{"b64_json":"QQ=="}]}`)}}
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("grp", "up", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)
	router := newImageTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations",
		strings.NewReader(`{"model":"grp","prompt":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, provider.genCalls)
}

func TestImageMediaCostInput_Buckets(t *testing.T) {
	tests := []struct {
		name string
		resp *core.ImageGenerationResponse
		want core.MediaCostInput
	}{
		{
			name: "有 image token usage 时优先 token 口径",
			resp: &core.ImageGenerationResponse{
				Data:  []core.ImageData{{B64JSON: "x"}},
				Usage: &core.ImageUsage{InputTokens: 11, OutputTokens: 22, TotalTokens: 33},
			},
			want: core.MediaCostInput{InputImageTokens: 11, OutputImageTokens: 22},
		},
		{
			name: "usage 带 input_tokens_details 时用 image_tokens 细分",
			resp: &core.ImageGenerationResponse{
				Data: []core.ImageData{{B64JSON: "x"}},
				Usage: &core.ImageUsage{
					InputTokens: 30, OutputTokens: 40, TotalTokens: 70,
					InputTokensDetails: &core.ImageInputTokensDetails{ImageTokens: 25, TextTokens: 5},
				},
			},
			want: core.MediaCostInput{InputImageTokens: 25, OutputImageTokens: 40},
		},
		{
			name: "无 usage 时退化为 len(data) 张数 + 一次请求",
			resp: &core.ImageGenerationResponse{Data: []core.ImageData{{B64JSON: "a"}, {B64JSON: "b"}}},
			want: core.MediaCostInput{Images: 2, Requests: 1},
		},
		{
			name: "usage 全为 0（字段存在但无值）时也退化为张数",
			resp: &core.ImageGenerationResponse{
				Data:  []core.ImageData{{B64JSON: "a"}},
				Usage: &core.ImageUsage{TotalTokens: 0},
			},
			want: core.MediaCostInput{Images: 1, Requests: 1},
		},
		{
			name: "完全无产物无 usage → 零值（采集侧按 usage_missing 处理）",
			resp: &core.ImageGenerationResponse{},
			want: core.MediaCostInput{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.ImageMediaCostInput(tt.resp)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestImageHandler_WritesMediaUsageWithExactType(t *testing.T) {
	// 采集器对 core.MediaCostInput 做**精确类型断言**：传别名/指针会静默丢用量
	// → 静默 0 元。这里直接断言 gin context 里存的就是 core.MediaCostInput 值类型。
	provider := &fakeImageProvider{plainProvider: &plainProvider{}, genResp: &core.ImageGenerationResponse{
		Raw:   []byte(`{"created":1,"data":[{"b64_json":"QQ=="},{"b64_json":"Qg=="}]}`),
		Data:  []core.ImageData{{B64JSON: "QQ=="}, {B64JSON: "Qg=="}},
		Usage: &core.ImageUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7},
	}}
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("grp", "up", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	var captured any
	var capturedOK bool
	router.POST("/v1/images/generations", func(c *gin.Context) {
		h.HandleImageGeneration(c)
		captured, capturedOK = c.Get(gwobs.CtxKeyMediaUsage)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations",
		strings.NewReader(`{"model":"grp","prompt":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), req)

	require.True(t, capturedOK, "成功路径必须写媒体用量")
	media, ok := captured.(core.MediaCostInput)
	require.True(t, ok, "必须是 core.MediaCostInput 值类型（精确类型断言，禁止别名/指针）")
	assert.Equal(t, 3, media.InputImageTokens)
	assert.Equal(t, 4, media.OutputImageTokens)
	assert.True(t, media.HasUsage())
}

// ── B4：媒体路径不落 trace 正文（纯函数级验证）───────────────────────────────

func TestImageHandler_DoesNotIncludeBodyInErrorMessages(t *testing.T) {
	// 错误文案里绝不能回显请求正文（generations 的 body 可能含 base64）。
	provider := &fakeImageProvider{plainProvider: &plainProvider{}, genErr: core.NewProviderError("openai", http.StatusInternalServerError, "upstream down", nil)}
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("grp", "up", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return provider }),
	)
	router := newImageTestRouter(h)

	secret := "SUPER-SECRET-BASE64-PAYLOAD"
	body := `{"model":"grp","prompt":"` + secret + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.NotContains(t, w.Body.String(), secret)
}

// ── B3 边界：part 分类与体积上限 ──────────────────────────────────────────────

// buildRawMultipart 允许精确控制每个 part 的 header（用于构造「无 filename 的图像
// part」与「带 Content-Type 的文本字段」这两种非常规形状）。
func buildRawMultipart(t *testing.T, parts []multipartFile) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, f := range parts {
		header := make(textproto.MIMEHeader)
		disposition := `form-data; name="` + f.fieldName + `"`
		if f.fileName != "" {
			disposition += `; filename="` + f.fileName + `"`
		}
		header.Set("Content-Disposition", disposition)
		if f.contentType != "" {
			header.Set("Content-Type", f.contentType)
		}
		part, err := writer.CreatePart(header)
		require.NoError(t, err)
		_, err = part.Write([]byte(f.data))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return &buf, writer.FormDataContentType()
}

func TestParseImageEditForm_FilePartWithoutFilenameStillCountsAsImage(t *testing.T) {
	// 有字段名 image[] 但没写 filename（部分手写/代理客户端会这样发）：
	// 必须仍按图像文件处理，否则编辑请求会因「没有 image」被 400。
	body, contentType := buildRawMultipart(t, []multipartFile{
		{fieldName: "model", data: "grp", contentType: "text/plain"},
		{fieldName: "prompt", data: "edit", contentType: "text/plain"},
		{fieldName: "image[]", data: "PNGBYTES", contentType: "image/png"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	req.Header.Set("Content-Type", contentType)

	form, err := handler.ParseImageEditForm(req)
	require.NoError(t, err)
	editReq := form.ToEditRequest("up")
	require.Len(t, editReq.Images, 1)
	assert.Equal(t, "PNGBYTES", string(editReq.Images[0].Data))
	assert.Equal(t, "image/png", editReq.Images[0].ContentType)
}

func TestParseImageEditForm_TextFieldWithContentTypeStaysAField(t *testing.T) {
	// 文本字段带了 Content-Type：不能被误判成文件（否则会被塞进 Images 并发给上游）。
	body, contentType := buildRawMultipart(t, []multipartFile{
		{fieldName: "model", data: "grp", contentType: "text/plain"},
		{fieldName: "prompt", data: "a photo", contentType: "text/plain; charset=utf-8"},
		{fieldName: "image[]", data: "PNG", contentType: "image/png"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	req.Header.Set("Content-Type", contentType)

	form, err := handler.ParseImageEditForm(req)
	require.NoError(t, err)
	assert.Equal(t, "a photo", form.Fields()["prompt"])
	editReq := form.ToEditRequest("up")
	require.Len(t, editReq.Images, 1)
	assert.Equal(t, "PNG", string(editReq.Images[0].Data))
}

func TestImageHandler_Edit_OversizedFieldIs400(t *testing.T) {
	// 文本字段体积上限（1MB）：超限必须 400，而不是默默内存膨胀。
	h := handler.NewImageHandler(
		handler.WithImageGroupDispatcher(newImageDispatcher("grp", "up", "openai")),
		handler.WithImageModelResolver(func(_, _ string) core.Provider { return newFakeImageProvider() }),
	)
	router := newImageTestRouter(h)

	body, contentType := buildMultipartBody(t, []multipartField{
		{name: "model", value: "grp"},
		{name: "prompt", value: strings.Repeat("x", 2<<20)},
	}, []multipartFile{
		{fieldName: "image[]", fileName: "a.png", contentType: "image/png", data: "PNG"},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "exceeds")
}

func TestParseImageEditForm_AcceptsImagesLargerThan8MB(t *testing.T) {
	// 回归守卫：OpenAI Images edits 允许单张图最大 25MB，早期实现用 8MB 作为
	// 「内存上限」，会把合法请求 400 掉。这里用 12MB 证明 8MB 已不再是墙。
	const twelveMB = 12 << 20
	big := strings.Repeat("P", twelveMB)

	body, contentType := buildRawMultipart(t, []multipartFile{
		{fieldName: "model", data: "grp", contentType: "text/plain"},
		{fieldName: "prompt", data: "edit", contentType: "text/plain"},
		{fieldName: "image[]", fileName: "big.png", contentType: "image/png", data: big},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	req.Header.Set("Content-Type", contentType)

	form, err := handler.ParseImageEditForm(req)
	require.NoError(t, err, "12MB 的图像必须被接受（上限 32MB）")
	editReq := form.ToEditRequest("up")
	require.Len(t, editReq.Images, 1)
	assert.Len(t, editReq.Images[0].Data, twelveMB, "字节必须完整保留")
}
