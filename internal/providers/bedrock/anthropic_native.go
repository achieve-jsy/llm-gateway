package bedrock

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/llm-gateway/internal/core"
)

// bedrockAnthropicVersion 是 AWS Bedrock 原生 Anthropic Messages 接口要求的版本标记。
// 缺了它 InvokeModel 会直接 ValidationException。
const bedrockAnthropicVersion = "bedrock-2023-05-31"

// nativeRelayExcludedPrefixes 是只能走 Converse 的厂商族前缀（规范化后）。
// 它们在 Bedrock 上没有原生 Anthropic Messages 入口，即便模型名里带 "claude"
// 字样也不能声明成原生。
var nativeRelayExcludedPrefixes = []string{"openai.", "xai.", "moonshotai."}

// NativeRelayFormatFor 报告 model 能否零转换接收 Anthropic Messages 请求体。
//
// model 可能有两种形态，都要能判定：
//   - 上游模型 ID（global.anthropic.claude-sonnet-4-5-20250929-v1:0）
//   - 模型组名 / 记录名（claude-sonnet-4-5）—— 经 p.modelUpstreamIDs 才能落到上游
//
// 注意与 RelayNative 的分工：真正的请求体 model 已由 Router 改写成上游 ID，
// 这里只做「能不能走原生」的分流判断，不改写调用方的任何数据。
func (p *Provider) NativeRelayFormatFor(model string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	if format != core.NativeRelayClaude {
		return "", false
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false
	}

	// 映射前与映射后都要试：组名查表得到上游 ID，上游 ID 直接用。
	candidates := []string{model}
	if base := stripProviderPrefix(model, providerName); base != model {
		candidates = append(candidates, base)
	}
	if p.modelUpstreamIDs != nil {
		for _, key := range []string{model, stripProviderPrefix(model, providerName)} {
			if id, ok := p.modelUpstreamIDs[key]; ok && id != "" && id != model {
				candidates = append(candidates, id)
			}
		}
	}

	// 先排除只能走 Converse 的厂商族，再判断是否 Claude。
	for _, c := range candidates {
		id := normalizeBedrockModelID(c)
		for _, prefix := range nativeRelayExcludedPrefixes {
			if strings.HasPrefix(id, prefix) {
				return "", false
			}
		}
	}
	for _, c := range candidates {
		// 上游 ID 命中 "anthropic.claude"；组名（claude-sonnet-4-5）只能靠 "claude" 片段命中。
		if strings.Contains(normalizeBedrockModelID(c), "claude") {
			return core.NativeRelayClaude, true
		}
	}
	return "", false
}

// RelayNative 以原生 Anthropic Messages 请求体调用 Bedrock InvokeModel，
// 返回上游原始响应体（不做任何协议转换）。
func (p *Provider) RelayNative(ctx context.Context, body []byte) ([]byte, error) {
	if err := p.ready(); err != nil {
		return nil, err
	}
	modelID, payload, err := p.buildNativeRelayBody(body)
	if err != nil {
		return nil, err
	}

	out, err := p.runtime.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     awssdk.String(modelID),
		Body:        payload,
		ContentType: awssdk.String("application/json"),
		Accept:      awssdk.String("application/json"),
	})
	if err != nil {
		return nil, mapAWSError(err)
	}
	return out.Body, nil
}

// RelayNativeStream 以原生 Anthropic Messages 请求体调用
// InvokeModelWithResponseStream，返回的字节流本身就是标准 Anthropic SSE
// （AWS 每个 chunk 的 Bytes 已是完整 SSE 帧，直接按序写出即可）。
func (p *Provider) RelayNativeStream(ctx context.Context, body []byte) (io.ReadCloser, error) {
	if err := p.ready(); err != nil {
		return nil, err
	}
	modelID, payload, err := p.buildNativeRelayBody(body)
	if err != nil {
		return nil, err
	}

	// 独立子 context：调用方 Close 时才能中断仍在读的 HTTP 响应体，
	// 否则 SDK 的读 goroutine 会一直挂在网络上。
	streamCtx, cancel := context.WithCancel(ctx)
	out, err := p.runtime.InvokeModelWithResponseStream(streamCtx, &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     awssdk.String(modelID),
		Body:        payload,
		ContentType: awssdk.String("application/json"),
		Accept:      awssdk.String("application/json"),
	})
	if err != nil {
		cancel()
		return nil, mapAWSError(err)
	}
	return newNativeRelayStream(streamCtx, cancel, out.GetStream()), nil
}

// buildNativeRelayBody 注入 Bedrock 必需的 anthropic_version，并把 body 里的
// model 换成上游 ID（查不到映射就原样使用 —— Router 通常已经改写好了）。
// 其余字段（top_k / context_management / thinking 等）原样保留，这正是本次改造的目的。
func (p *Provider) buildNativeRelayBody(body []byte) (string, []byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, core.NewInvalidRequestError("bedrock native relay body must be a JSON object: "+err.Error(), err)
	}

	rawModel, _ := payload["model"].(string)
	model := strings.TrimSpace(rawModel)
	if model == "" {
		return "", nil, core.NewInvalidRequestError("bedrock native relay body requires a model", nil)
	}

	modelID := p.relayUpstreamModelID(model)
	payload["model"] = modelID
	payload["anthropic_version"] = bedrockAnthropicVersion

	out, err := json.Marshal(payload)
	if err != nil {
		return "", nil, core.NewInvalidRequestError("bedrock native relay body marshal failed: "+err.Error(), err)
	}
	return modelID, out, nil
}

// relayUpstreamModelID 只做兜底映射：body 里的 model 能查到映射就替换，
// 查不到就原样使用（Router 已把 model 改写成上游 ID 是常态）。
func (p *Provider) relayUpstreamModelID(model string) string {
	if p.modelUpstreamIDs != nil {
		if id, ok := p.modelUpstreamIDs[model]; ok && id != "" {
			return id
		}
		if id, ok := p.modelUpstreamIDs[stripProviderPrefix(model, providerName)]; ok && id != "" {
			return id
		}
	}
	return stripProviderPrefix(model, providerName)
}

// nativeRelayStream 把 Bedrock 的 event stream 逐块转成 SSE 字节流。
type nativeRelayStream struct {
	pr     *io.PipeReader
	cancel context.CancelFunc
	events *bedrockruntime.InvokeModelWithResponseStreamEventStream
	once   sync.Once
}

// newNativeRelayStream 用 io.Pipe + goroutine 把事件流桥接成 io.ReadCloser。
func newNativeRelayStream(ctx context.Context, cancel context.CancelFunc, events *bedrockruntime.InvokeModelWithResponseStreamEventStream) *nativeRelayStream {
	pr, pw := io.Pipe()

	go func() {
		// 顺序：先关写端让 reader 看到 EOF，再释放 context 与 SDK 流。
		defer cancel()
		defer events.Close()
		defer pw.Close()

		ch := events.Events()
		for {
			select {
			case <-ctx.Done():
				// 调用方 Close 或上游已结束：不再关心剩余事件。
				return
			case ev, ok := <-ch:
				if !ok {
					// 事件流结束：上游异常事件（限流/校验失败等）走 Err()，
					// 必须透给 reader，否则客户端会把空流当成正常回答。
					if err := events.Err(); err != nil {
						_ = pw.CloseWithError(mapAWSError(err))
					}
					return
				}
				chunk, ok := ev.(*brtypes.ResponseStreamMemberChunk)
				if !ok || len(chunk.Value.Bytes) == 0 {
					continue // 未知事件类型直接跳过
				}
				if _, err := pw.Write(chunk.Value.Bytes); err != nil {
					return // reader 已关闭
				}
			}
		}
	}()

	return &nativeRelayStream{pr: pr, cancel: cancel, events: events}
}

func (s *nativeRelayStream) Read(p []byte) (int, error) {
	return s.pr.Read(p)
}

// Close 关闭读端并取消 context，让写 goroutine 与 SDK 读 goroutine 一起退出。
func (s *nativeRelayStream) Close() error {
	var err error
	s.once.Do(func() {
		err = s.pr.Close()
		s.cancel()
		s.events.Close()
	})
	return err
}

var (
	_ core.NativeRelayProvider = (*Provider)(nil)
	_ io.ReadCloser            = (*nativeRelayStream)(nil)
)
