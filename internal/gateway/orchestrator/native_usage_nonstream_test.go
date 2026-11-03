package orchestrator

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/gin-gonic/gin"
)

// TestChatNativeAnthropicWritesUsage 钉死非流式原生旁路的计费输入。
//
// 原生响应是上游原始字节，网关不做协议转换，所以没有任何既有代码会去解析
// usage——必须由 ChatNative 显式回填，否则 UsageCollector 读到 UsageMissing，
// 这条路径的请求计费为 0。
func TestChatNativeAnthropicWritesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &nativeTestProvider{
		supports:   true,
		nativeBody: `{"id":"msg_1","type":"message","usage":{"input_tokens":25,"output_tokens":15}}`,
	}
	orch := newNativeTestOrchestrator(t, provider)

	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := WithGinContext(context.Background(), gc)

	if _, err := orch.ChatNative(ctx, &core.ChatRequest{Model: "claude-model"}, []byte(`{"model":"claude-model"}`), core.NativeRelayClaude); err != nil {
		t.Fatalf("ChatNative: %v", err)
	}

	raw, ok := gc.Get("usage")
	if !ok {
		t.Fatal("usage 未回填：非流式原生路径计费会记为 0")
	}
	u, ok := raw.(core.Usage)
	if !ok {
		t.Fatalf("usage 类型错误: %T", raw)
	}
	if u.PromptTokens != 25 || u.CompletionTokens != 15 {
		t.Errorf("usage = %+v, want prompt=25 completion=15", u)
	}
	if u.TotalTokens != 40 {
		t.Errorf("TotalTokens = %d, want 40（Anthropic 不返回 total，需用 input+output 补全）", u.TotalTokens)
	}
}

// TestChatNativeOpenAIUsageStillWorks 确认 OpenAI 风格 usage 也能被回填。
func TestChatNativeOpenAIUsageStillWorks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &nativeTestProvider{
		supports:   true,
		nativeBody: `{"id":"chatcmpl-1","usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`,
	}
	orch := newNativeTestOrchestrator(t, provider)

	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := WithGinContext(context.Background(), gc)

	if _, err := orch.ChatNative(ctx, &core.ChatRequest{Model: "claude-model"}, []byte(`{"model":"claude-model"}`), core.NativeRelayClaude); err != nil {
		t.Fatalf("ChatNative: %v", err)
	}
	u, ok := gc.MustGet("usage").(core.Usage)
	if !ok {
		t.Fatal("usage 未回填")
	}
	if u.PromptTokens != 7 || u.CompletionTokens != 3 || u.TotalTokens != 10 {
		t.Errorf("usage = %+v, want 7/3/10", u)
	}
}

// TestChatNativeNoUsageFieldNoCrash 响应里没有 usage 时不得 panic，也不得写入伪造值。
func TestChatNativeNoUsageFieldNoCrash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &nativeTestProvider{supports: true, nativeBody: `{"id":"msg_1","type":"message"}`}
	orch := newNativeTestOrchestrator(t, provider)

	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := WithGinContext(context.Background(), gc)

	if _, err := orch.ChatNative(ctx, &core.ChatRequest{Model: "claude-model"}, []byte(`{"model":"claude-model"}`), core.NativeRelayClaude); err != nil {
		t.Fatalf("ChatNative: %v", err)
	}
	if _, ok := gc.Get("usage"); ok {
		t.Error("响应无 usage 时不应写入 usage（会污染成本估算）")
	}
}
