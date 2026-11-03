package providers_test

// Router → vertex provider → 假 Gemini 上游的跨层验证。
//
// 分层测试各自覆盖了本层（router 的哨兵、vertex 的转发、gemini 的 URL 拼接），
// 但都没验证一个关键契约：Router 会把 body.model 改写成 resolveProvider 解析出的
// 模型名，gemini provider 再把它抽出来拼 URL。这条链断了不会有任何单层测试失败。
//
// 放在外部测试包是因为 providers 包不能 import vertex（会成环）。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/vertex"
)

// singleModelLookup 是只登记一个模型的注册表。
type singleModelLookup struct {
	model        string
	providerName string
	providerType string
	provider     core.Provider
}

func (l *singleModelLookup) match(selector string) bool {
	if i := strings.LastIndex(selector, "/"); i >= 0 {
		selector = selector[i+1:]
	}
	return selector == l.model
}

func (l *singleModelLookup) Supports(model string) bool { return l.match(model) }
func (l *singleModelLookup) GetProvider(model string) core.Provider {
	if l.match(model) {
		return l.provider
	}
	return nil
}
func (l *singleModelLookup) GetProviderType(model string) string {
	if l.match(model) {
		return l.providerType
	}
	return ""
}
func (l *singleModelLookup) ListModels() []core.Model { return []core.Model{{ID: l.model}} }
func (l *singleModelLookup) ModelCount() int          { return 1 }
func (l *singleModelLookup) GetProviderName(model string) string {
	if l.match(model) {
		return l.providerName
	}
	return ""
}
func (l *singleModelLookup) GetProviderNameForType(string) string { return l.providerName }
func (l *singleModelLookup) GetProviderTypeForName(string) string { return l.providerType }

func TestRouterRelayNativeGeminiThroughVertex(t *testing.T) {
	var gotPath string
	var gotBody []byte
	upstreamResp := `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":3,"totalTokenCount":12}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamResp)
	}))
	defer srv.Close()

	p := vertex.New(providers.ProviderConfig{
		Type:     "vertex",
		BaseURL:  srv.URL + "/publishers/google",
		APIKey:   "fake-key",
		AuthType: "api_key",
		APIMode:  "native",
	}, providers.ProviderOptions{})

	lookup := &singleModelLookup{model: "gemini-2.5-pro", providerName: "vertex-main", providerType: "vertex", provider: p}
	router, err := providers.NewRouter(lookup)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	// 客户端 body 里的 model 是客户端自己写的，Router 必须改写成路由解析结果
	reqBody := `{"model":"whatever-the-client-sent","contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"topK":40,"topP":0.95}}`
	out, err := router.RelayNative(context.Background(), &core.ChatRequest{Model: "gemini-2.5-pro"}, []byte(reqBody), core.NativeRelayGemini)
	if err != nil {
		t.Fatalf("RelayNative: %v", err)
	}

	if string(out) != upstreamResp {
		t.Errorf("响应应逐字节透传: got=%.200s", out)
	}
	if want := "/publishers/google/models/gemini-2.5-pro:generateContent"; gotPath != want {
		t.Errorf("path = %q, want %q（URL 模型名必须来自 Router 解析结果）", gotPath, want)
	}

	var up map[string]any
	if err := json.Unmarshal(gotBody, &up); err != nil {
		t.Fatalf("上游 body 不是 JSON: %v", err)
	}
	if _, exists := up["model"]; exists {
		t.Error("model 字段应被 gemini provider 摘除")
	}
	gc, ok := up["generationConfig"].(map[string]any)
	if !ok {
		t.Fatal("generationConfig 必须保留")
	}
	if v, _ := gc["topK"].(float64); v != 40 {
		t.Errorf("topK = %v, want 40", gc["topK"])
	}
	if v, _ := gc["topP"].(float64); v != 0.95 {
		t.Errorf("topP = %v, want 0.95", gc["topP"])
	}
}

// TestRouterRelayNativeGeminiRejectsClaudeProvider 验证格式不会被错发：
// 只支持 Claude 的 provider 收到 Gemini 格式必须返回哨兵且不触达上游。
func TestRouterRelayNativeGeminiRejectsClaudeProvider(t *testing.T) {
	lookup := &singleModelLookup{model: "claude-on-bedrock", providerName: "aws", providerType: "bedrock", provider: &claudeOnlyProvider{}}
	router, err := providers.NewRouter(lookup)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	_, err = router.RelayNative(context.Background(), &core.ChatRequest{Model: "claude-on-bedrock"}, []byte(`{"model":"claude-on-bedrock"}`), core.NativeRelayGemini)
	if err == nil {
		t.Fatal("Claude provider 收到 Gemini 格式应返回错误")
	}
	if !errors.Is(err, core.ErrNativeNotSupported) {
		t.Errorf("err = %v, want ErrNativeNotSupported", err)
	}
}

// claudeOnlyProvider 只声明支持 Claude 原生格式，用来验证 format 分流。
type claudeOnlyProvider struct {
	called bool
}

func (p *claudeOnlyProvider) ChatCompletion(context.Context, *core.ChatRequest) (*core.ChatResponse, error) {
	return nil, errors.New("not used")
}
func (p *claudeOnlyProvider) StreamChatCompletion(context.Context, *core.ChatRequest) (io.ReadCloser, error) {
	return nil, errors.New("not used")
}
func (p *claudeOnlyProvider) ListModels(context.Context) (*core.ModelsResponse, error) {
	return nil, nil
}
func (p *claudeOnlyProvider) Responses(context.Context, *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return nil, errors.New("not used")
}
func (p *claudeOnlyProvider) StreamResponses(context.Context, *core.ResponsesRequest) (io.ReadCloser, error) {
	return nil, errors.New("not used")
}
func (p *claudeOnlyProvider) Embeddings(context.Context, *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return nil, errors.New("not used")
}
func (p *claudeOnlyProvider) Rerank(context.Context, *core.RerankRequest) (*core.RerankResponse, error) {
	return nil, errors.New("not used")
}
func (p *claudeOnlyProvider) NativeRelayFormatFor(_ string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	if format == core.NativeRelayClaude {
		return core.NativeRelayClaude, true
	}
	return "", false
}
func (p *claudeOnlyProvider) RelayNative(context.Context, []byte) ([]byte, error) {
	p.called = true
	return []byte("should-not-happen"), nil
}
func (p *claudeOnlyProvider) RelayNativeStream(context.Context, []byte) (io.ReadCloser, error) {
	p.called = true
	return io.NopCloser(strings.NewReader("")), nil
}
