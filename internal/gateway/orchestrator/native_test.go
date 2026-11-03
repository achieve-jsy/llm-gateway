package orchestrator

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// 测试替身：最小 core.ModelLookup + 可切换原生能力的 provider
// ---------------------------------------------------------------------------

type nativeTestLookup struct {
	model        string
	providerName string
	providerType string
	provider     core.Provider
}

func (l *nativeTestLookup) qualified() string { return l.providerName + "/" + l.model }

// modelOf 取 selector 里的裸模型 ID，兼容 "aws/x" 与 "bedrock/x" 两种限定前缀。
func (l *nativeTestLookup) modelOf(selector string) string {
	if i := strings.LastIndex(selector, "/"); i >= 0 {
		return selector[i+1:]
	}
	return selector
}

func (l *nativeTestLookup) Supports(model string) bool { return l.modelOf(model) == l.model }

func (l *nativeTestLookup) GetProvider(model string) core.Provider {
	if l.modelOf(model) == l.model {
		return l.provider
	}
	return nil
}

func (l *nativeTestLookup) GetProviderType(model string) string {
	if l.modelOf(model) == l.model {
		return l.providerType
	}
	return ""
}

func (l *nativeTestLookup) GetProviderName(model string) string {
	if model == l.model {
		return l.providerName
	}
	return ""
}

func (l *nativeTestLookup) GetProviderNameForType(_ string) string { return l.providerName }
func (l *nativeTestLookup) GetProviderTypeForName(_ string) string { return l.providerType }
func (l *nativeTestLookup) ListModels() []core.Model               { return []core.Model{{ID: l.model}} }
func (l *nativeTestLookup) ModelCount() int                        { return 1 }

type nativeTestProvider struct {
	supports   bool
	streamBody string
	nativeBody string                 // RelayNative 返回体，空则用默认最小响应
	format     core.NativeRelayFormat // 声明支持的格式，空表示 Claude
	body       []byte
}

func (p *nativeTestProvider) ChatCompletion(_ context.Context, _ *core.ChatRequest) (*core.ChatResponse, error) {
	return &core.ChatResponse{}, nil
}

func (p *nativeTestProvider) StreamChatCompletion(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (p *nativeTestProvider) ListModels(_ context.Context) (*core.ModelsResponse, error) {
	return &core.ModelsResponse{}, nil
}

func (p *nativeTestProvider) Responses(_ context.Context, _ *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return &core.ResponsesResponse{}, nil
}

func (p *nativeTestProvider) StreamResponses(_ context.Context, _ *core.ResponsesRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (p *nativeTestProvider) Embeddings(_ context.Context, _ *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return &core.EmbeddingResponse{}, nil
}

func (p *nativeTestProvider) Rerank(_ context.Context, _ *core.RerankRequest) (*core.RerankResponse, error) {
	return &core.RerankResponse{}, nil
}

// format 为空时默认为 Claude（兼容既有用例），置为其他值可测其它格式的分流。
func (p *nativeTestProvider) NativeRelayFormatFor(_ string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	if !p.supports {
		return "", false
	}
	if p.format != "" && format != p.format {
		return "", false
	}
	if p.format == "" && format != core.NativeRelayClaude {
		return "", false
	}
	return format, true
}

func (p *nativeTestProvider) RelayNative(_ context.Context, body []byte) ([]byte, error) {
	p.body = body
	if p.nativeBody != "" {
		return []byte(p.nativeBody), nil
	}
	return []byte(`{"id":"msg_1","type":"message"}`), nil
}

func (p *nativeTestProvider) RelayNativeStream(_ context.Context, body []byte) (io.ReadCloser, error) {
	p.body = body
	return io.NopCloser(strings.NewReader(p.streamBody)), nil
}

func newNativeTestOrchestrator(t *testing.T, provider *nativeTestProvider) *ChatOrchestrator {
	t.Helper()
	lookup := &nativeTestLookup{
		model:        "claude-model",
		providerName: "aws",
		providerType: "bedrock",
		provider:     provider,
	}
	router, err := providers.NewRouter(lookup)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return NewChatOrchestrator(router)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestChatNativeUnsupportedPropagatesSentinel 验证路由不支持原生格式时，
// ChatNative 原样上抛 ErrNativeNotSupported，且不被 errormap 转成 UnifiedError。
func TestChatNativeUnsupportedPropagatesSentinel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())

	o := newNativeTestOrchestrator(t, &nativeTestProvider{supports: false})
	ctx := WithGinContext(context.Background(), gc)

	body, err := o.ChatNative(ctx, &core.ChatRequest{Model: "claude-model"}, []byte(`{"a":1}`), core.NativeRelayClaude)
	if body != nil {
		t.Fatalf("unsupported 时应返回 nil body, got %q", body)
	}
	if !errors.Is(err, core.ErrNativeNotSupported) {
		t.Fatalf("err=%v 应可被 errors.Is 命中 core.ErrNativeNotSupported", err)
	}
	var ue *model.UnifiedError
	if errors.As(err, &ue) {
		t.Fatalf("哨兵错误不应被 errormap 转换, got %#v", ue)
	}
	// 降级路径不应写 error_message，避免 call_logs 记录假错误
	if v, ok := gc.Get("error_message"); ok {
		t.Fatalf("降级路径不应写 error_message, got %v", v)
	}
}

// TestChatNativeStreamUnsupportedPropagatesSentinel 验证流式路径同样原样上抛哨兵。
func TestChatNativeStreamUnsupportedPropagatesSentinel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())

	o := newNativeTestOrchestrator(t, &nativeTestProvider{supports: false})
	ctx := WithGinContext(context.Background(), gc)

	stream, err := o.ChatNativeStream(ctx, &core.ChatRequest{Model: "claude-model"}, []byte(`{"a":1}`), core.NativeRelayClaude)
	if stream != nil {
		t.Fatalf("unsupported 时应返回 nil stream")
	}
	if !errors.Is(err, core.ErrNativeNotSupported) {
		t.Fatalf("err=%v 应可被 errors.Is 命中 core.ErrNativeNotSupported", err)
	}
	var ue *model.UnifiedError
	if errors.As(err, &ue) {
		t.Fatalf("哨兵错误不应被 errormap 转换, got %#v", ue)
	}
}

// TestChatNativePassthrough 验证非流式旁路原样透传请求体并返回上游原始响应体。
func TestChatNativePassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())

	provider := &nativeTestProvider{supports: true}
	o := newNativeTestOrchestrator(t, provider)
	ctx := WithGinContext(context.Background(), gc)
	// 带 provider 前缀请求：验证上游拿到的是解析后的模型名，协议独有字段保留
	req := &core.ChatRequest{Model: "bedrock/claude-model"}
	reqBody := []byte(`{"model":"claude-sonnet-4-5","top_k":7}`)

	respBody, err := o.ChatNative(ctx, req, reqBody, core.NativeRelayClaude)
	if err != nil {
		t.Fatalf("ChatNative: %v", err)
	}
	if string(respBody) != `{"id":"msg_1","type":"message"}` {
		t.Fatalf("响应体应原样返回, got %s", respBody)
	}
	if got := string(provider.body); got != `{"model":"claude-model","top_k":7}` {
		t.Fatalf("body.model 应改写为解析后模型名且其余字段原样, got %s", got)
	}
	if v, _ := gc.Get("stream"); v != false {
		t.Fatalf("stream 应为 false, got %v", v)
	}
	// 假 lookup 不做 type→name 映射，GetProviderName 回落到 selector 的 provider 段
	if v, _ := gc.Get("provider"); v != "bedrock" {
		t.Fatalf("provider 应写入 gin context, got %v", v)
	}
}

// TestChatNativeStreamUsageObserved 验证流式旁路成功时 usageObserver 能从
// 上游原始 SSE 的末尾 usage chunk 提取 usage 并回写gin context（否则计费漏）。
func TestChatNativeStreamUsageObserved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())

	provider := &nativeTestProvider{
		supports: true,
		streamBody: "event: message_start\n" +
			`data: {"type":"message_start","message":{"usage":{"input_tokens":12,"output_tokens":0}}}` + "\n\n" +
			"event: content_block_delta\n" +
			`data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n" +
			"event: message_delta\n" +
			`data: {"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}` + "\n\n",
	}
	o := newNativeTestOrchestrator(t, provider)
	ctx := WithGinContext(context.Background(), gc)

	stream, err := o.ChatNativeStream(ctx, &core.ChatRequest{Model: "claude-model"}, []byte(`{"stream":true}`), core.NativeRelayClaude)
	if err != nil {
		t.Fatalf("ChatNativeStream: %v", err)
	}

	read, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if !strings.Contains(string(read), "content_block_delta") {
		t.Fatalf("字节流应原样透传, got %s", read)
	}
	// usage 只在流关闭时回写
	if err := stream.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}

	v, ok := gc.Get("usage")
	if !ok {
		t.Fatal("usage 未被写入 gin context（计费会漏）")
	}
	usage, ok := v.(core.Usage)
	if !ok {
		t.Fatalf("usage 类型应为 core.Usage, got %T", v)
	}
	if usage.PromptTokens != 12 || usage.CompletionTokens != 3 || usage.TotalTokens != 15 {
		t.Fatalf("usage=%+v, want 12/3/15", usage)
	}
	if v, _ := gc.Get("stream"); v != true {
		t.Fatalf("stream 应为 true, got %v", v)
	}
}
