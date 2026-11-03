package providers

// 给"自持 llmclient.Client、不用 openai.CompatibleProvider"的 OpenAI 兼容渠道
// 提供一份原生旁路实现（deepseek / groq / xai / ollama 属此类）。
//
// 为什么单独一份而不是复用 openai 包：那些包没有 CompatibleProvider 字段，
// 无法用转发方法继承；但它们都用同一个 llmclient.Client 与 /chat/completions
// 端点约定，所以一份薄 helper 就够，不必各自复制 40 行。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

// NativeOpenAIPath 按原始请求体判定 OpenAI 兼容端点。
//
// 命名与 internal/providers/openai/passthrough_semantics.go 保持一致：
// messages → /chat/completions（Chat），input → /responses（Responses）。
func NativeOpenAIPath(body []byte) (string, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return "", fmt.Errorf("%w: native relay body is not a JSON object: %v", core.ErrNativeNotSupported, err)
	}
	if _, ok := probe["messages"]; ok {
		return "/chat/completions", nil
	}
	if _, ok := probe["input"]; ok {
		return "/responses", nil
	}
	return "", fmt.Errorf("%w: native relay body has neither messages nor input", core.ErrNativeNotSupported)
}

// NativeFormatForOpenAI 是 OpenAI 兼容渠道的能力判定：
// 它们同时吃得下 Chat 与 Responses 两种原生格式，其他格式一律拒绝。
//
// 签名收 model 是为了与 core.NativeRelayProvider 的契约同形 —— 判定必须显式
// 面对「按模型判断」这件事，而不是靠调用方写 `_ string` 把它藏起来。
//
// 这里的实现**有意不看 model**，前提是被 deepseek / groq / xai / ollama 这类
// 纯 OpenAI 兼容服务共用：它们名下全部模型都支持 /chat/completions 与
// /responses，渠道内不存在能力差异，逐一判断等于恒真。这个前提一旦不成立
// （例如某家开始提供只支持 Chat 的模型，或只支持 Responses 的推理模型），
// 就必须在这里按 model 分支，而不是在调用方各自补判断。
func NativeFormatForOpenAI(model string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	_ = model // 见上：共用本函数的三家渠道内无模型级差异
	switch format {
	case core.NativeRelayOpenAIChat, core.NativeRelayOpenAIResponses:
		return format, true
	default:
		return "", false
	}
}

// RelayNativeRaw 用 provider 自持的 client 把原始请求体直发上游，返回原始响应体。
func RelayNativeRaw(ctx context.Context, client *llmclient.Client, body []byte) ([]byte, error) {
	path, err := NativeOpenAIPath(body)
	if err != nil {
		return nil, err
	}
	return RelayNativeRawAt(ctx, client, path, body)
}

// RelayNativeRawAt 与 RelayNativeRaw 相同，但端点由调用方显式给出。
//
// 需要它的场景：整条渠道只有一个原生端点，且该端点**无法**从 body 形状推断。
// 典型是 Anthropic —— 它的请求体同样带 messages 字段，交给 NativeOpenAIPath
// 会被误判成 OpenAI Chat 的 /chat/completions。
func RelayNativeRawAt(ctx context.Context, client *llmclient.Client, endpoint string, body []byte) ([]byte, error) {
	resp, err := client.DoRaw(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: endpoint,
		RawBody:  body,
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// RelayNativeRawStream 同上，但返回上游原始 SSE 流（调用方关闭）。
func RelayNativeRawStream(ctx context.Context, client *llmclient.Client, body []byte) (io.ReadCloser, error) {
	path, err := NativeOpenAIPath(body)
	if err != nil {
		return nil, err
	}
	return RelayNativeRawStreamAt(ctx, client, path, body)
}

// RelayNativeRawStreamAt 是 RelayNativeRawAt 的流式版本。
func RelayNativeRawStreamAt(ctx context.Context, client *llmclient.Client, endpoint string, body []byte) (io.ReadCloser, error) {
	return client.DoStream(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: endpoint,
		RawBody:  body,
	})
}
