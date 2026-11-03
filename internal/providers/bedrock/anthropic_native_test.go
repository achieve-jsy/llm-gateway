package bedrock

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream/eventstreamapi"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// relayTestUpstream 是一个典型的 Bedrock 跨区域推理配置文件形态的 Claude 上游 ID。
const relayTestUpstream = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"

// relayTestProvider 构造一个指向本地假 Bedrock 服务端的 Provider。
// 使用静态 IAM 凭证 + 固定 region，避免 SDK 去读真实凭证链 / EC2 metadata。
func relayTestProvider(t *testing.T, upstreamIDs map[string]string, handler http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("AWS_REGION", "us-east-1")

	p, ok := New(providers.ProviderConfig{
		BaseURL:          srv.URL,
		APIKey:           `{"access_key_id":"AKIARELAYTEST","secret_access_key":"relay-test-secret"}`,
		ModelUpstreamIDs: upstreamIDs,
	}, providers.ProviderOptions{}).(*Provider)
	if !ok {
		t.Fatalf("New() 返回的不是 *Provider")
	}
	if p.configErr != nil {
		t.Fatalf("New() 配置失败: %v", p.configErr)
	}
	return p
}

// relayTestBody 是一个最小的 Anthropic Messages 请求体，带 top_k 与
// context_management —— 这两个字段在旧路径（Claude→Chat→Converse）里会被丢弃。
const relayTestBody = `{
	"model": "claude-sonnet-4-5",
	"max_tokens": 64,
	"top_k": 7,
	"context_management": {"edits": [{"type": "clear_tool_uses_20250919"}]},
	"messages": [{"role": "user", "content": "hi"}]
}`

// TestNativeRelayFormatFor 覆盖模型分流：Claude 模型（组名或上游名）走原生，
// 其余厂商族只能走 Converse。
func TestNativeRelayFormatFor(t *testing.T) {
	p := &Provider{modelUpstreamIDs: map[string]string{
		"claude-sonnet-4-5": "global.anthropic.claude-sonnet-4-5-20250929-v1:0",
		"gpt-5-group":       "global.openai.gpt-5.6-sol",
	}}

	cases := []struct {
		model  string
		format core.NativeRelayFormat
		want   bool
	}{
		// 上游名（含 anthropic.claude）
		{relayTestUpstream, core.NativeRelayClaude, true},
		{"bedrock/global.anthropic.claude-opus-4-6-v1", core.NativeRelayClaude, true},
		{"anthropic.claude-3-5-haiku-20241022-v1:0", core.NativeRelayClaude, true},
		// 模型组名（含 claude，但不含 anthropic.claude）
		{"claude-sonnet-4-5", core.NativeRelayClaude, true},
		// 非 Claude 家族一律 false
		{"global.openai.gpt-5.6-sol", core.NativeRelayClaude, false},
		{"global.moonshotai.kimi-k2-thinking", core.NativeRelayClaude, false},
		{"global.xai.grok-4-fast-reasoning", core.NativeRelayClaude, false},
		{"openai.gpt-oss-120b-v1:0", core.NativeRelayClaude, false},
		{"gpt-5-group", core.NativeRelayClaude, false},
		// 协议不匹配
		{relayTestUpstream, core.NativeRelayFormat("openai"), false},
		// 空模型
		{"", core.NativeRelayClaude, false},
	}

	for _, tc := range cases {
		gotFormat, ok := p.NativeRelayFormatFor(tc.model, tc.format)
		if ok != tc.want {
			t.Errorf("NativeRelayFormatFor(%q, %q) ok=%v, want %v", tc.model, tc.format, ok, tc.want)
			continue
		}
		if ok && gotFormat != core.NativeRelayClaude {
			t.Errorf("NativeRelayFormatFor(%q) format=%q, want %q", tc.model, gotFormat, core.NativeRelayClaude)
		}
	}
}

// TestBuildNativeRelayBody 覆盖请求体构造：注入 anthropic_version、替换 model，
// 且 top_k / context_management 等协议独有字段原样保留。
func TestBuildNativeRelayBody(t *testing.T) {
	p := &Provider{modelUpstreamIDs: map[string]string{
		"claude-sonnet-4-5": relayTestUpstream,
	}}

	model, payload, err := p.buildNativeRelayBody([]byte(relayTestBody))
	if err != nil {
		t.Fatalf("buildNativeRelayBody: %v", err)
	}
	if model != relayTestUpstream {
		t.Errorf("model = %q, want %q", model, relayTestUpstream)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("构造出的 body 不是合法 JSON: %v", err)
	}
	if got["anthropic_version"] != bedrockAnthropicVersion {
		t.Errorf("anthropic_version = %v, want %q", got["anthropic_version"], bedrockAnthropicVersion)
	}
	if got["model"] != relayTestUpstream {
		t.Errorf("body.model = %v, want %q", got["model"], relayTestUpstream)
	}
	if topK, ok := got["top_k"].(float64); !ok || topK != 7 {
		t.Errorf("top_k 丢失或被改写: %v", got["top_k"])
	}
	if got["context_management"] == nil {
		t.Error("context_management 丢失")
	}

	// Router 已把 model 改写成上游 ID 的情况：不再二次映射，原样使用。
	preResolved := strings.Replace(relayTestBody, `"model": "claude-sonnet-4-5"`, `"model": "`+relayTestUpstream+`"`, 1)
	model, _, err = p.buildNativeRelayBody([]byte(preResolved))
	if err != nil {
		t.Fatalf("buildNativeRelayBody(上游 ID): %v", err)
	}
	if model != relayTestUpstream {
		t.Errorf("上游 ID 被二次改写: %q", model)
	}

	// 非法 body 必须报错而不是把半成品发出去。
	if _, _, err := p.buildNativeRelayBody([]byte("{not json")); err == nil {
		t.Error("非法 JSON body 应返回错误")
	}
	if _, _, err := p.buildNativeRelayBody([]byte(`{"messages":[]}`)); err == nil {
		t.Error("缺少 model 应返回错误")
	}
}

// TestRelayNative_InvokeModel 校验非流式调用打到 InvokeModel，且上游返回体原样透出。
func TestRelayNative_InvokeModel(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	p := relayTestProvider(t, nil, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_relay_1","type":"message","role":"assistant"}`))
	})

	out, err := p.RelayNative(context.Background(), []byte(relayTestBody))
	if err != nil {
		t.Fatalf("RelayNative: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/invoke") {
		t.Errorf("path = %q, want 以 /invoke 结尾", gotPath)
	}
	if !strings.Contains(string(out), "msg_relay_1") {
		t.Errorf("响应体未透出: %s", out)
	}
	if gotBody["anthropic_version"] != bedrockAnthropicVersion {
		t.Errorf("上游收到的 anthropic_version = %v", gotBody["anthropic_version"])
	}
	if topK, ok := gotBody["top_k"].(float64); !ok || topK != 7 {
		t.Errorf("上游收到的 top_k = %v", gotBody["top_k"])
	}
}

// writeChunkFrames 把标准 Anthropic SSE 帧编码成 AWS event stream 的 chunk 事件。
func writeChunkFrames(t *testing.T, buf *bytes.Buffer, frames []string) {
	t.Helper()
	enc := eventstream.NewEncoder()
	for _, frame := range frames {
		payload, err := json.Marshal(map[string]string{
			"bytes": base64.StdEncoding.EncodeToString([]byte(frame)),
		})
		if err != nil {
			t.Fatalf("marshal chunk payload: %v", err)
		}
		msg := eventstream.Message{
			Headers: eventstream.Headers{
				{Name: eventstreamapi.MessageTypeHeader, Value: eventstream.StringValue(eventstreamapi.EventMessageType)},
				{Name: eventstreamapi.EventTypeHeader, Value: eventstream.StringValue("chunk")},
			},
			Payload: payload,
		}
		if err := enc.Encode(buf, msg); err != nil {
			t.Fatalf("encode event stream: %v", err)
		}
	}
}

// TestRelayNativeStream_AnthropicSSE 校验流式调用打到 InvokeModelWithResponseStream，
// 且 chunk 字节按序写出为合法 Anthropic SSE。
func TestRelayNativeStream_AnthropicSSE(t *testing.T) {
	frames := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_relay_2\",\"role\":\"assistant\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	var stream bytes.Buffer
	writeChunkFrames(t, &stream, frames)

	var gotPath string
	p := relayTestProvider(t, nil, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(stream.Bytes())
	})

	rc, err := p.RelayNativeStream(context.Background(), []byte(relayTestBody))
	if err != nil {
		t.Fatalf("RelayNativeStream: %v", err)
	}
	defer rc.Close()
	out, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("读取 SSE 流: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/invoke-with-response-stream") {
		t.Errorf("path = %q, want 以 /invoke-with-response-stream 结尾", gotPath)
	}
	got := string(out)
	if !strings.Contains(got, "event: message_start") || !strings.Contains(got, `"type":"message_start"`) {
		t.Errorf("缺少 message_start 帧: %s", got)
	}
	if want := strings.Join(frames, ""); got != want {
		t.Errorf("SSE 输出与 chunk 顺序不一致:\n got=%q\nwant=%q", got, want)
	}
}

// TestRelayNativeStream_UpstreamError 校验上游异常事件会被映射成读错误，
// 而不是静默 EOF（客户端会把空流当成正常回答）。
func TestRelayNativeStream_UpstreamError(t *testing.T) {
	var stream bytes.Buffer
	enc := eventstream.NewEncoder()
	msg := eventstream.Message{
		Headers: eventstream.Headers{
			{Name: eventstreamapi.MessageTypeHeader, Value: eventstream.StringValue(eventstreamapi.ExceptionMessageType)},
			{Name: eventstreamapi.ExceptionTypeHeader, Value: eventstream.StringValue("throttlingException")},
		},
		Payload: []byte(`{"message":"slow down"}`),
	}
	if err := enc.Encode(&stream, msg); err != nil {
		t.Fatalf("encode exception event: %v", err)
	}

	p := relayTestProvider(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(stream.Bytes())
	})

	rc, err := p.RelayNativeStream(context.Background(), []byte(relayTestBody))
	if err != nil {
		t.Fatalf("RelayNativeStream: %v", err)
	}
	defer rc.Close()
	if _, err := io.ReadAll(rc); err == nil {
		t.Fatal("上游异常事件应让读取返回错误")
	}
}
