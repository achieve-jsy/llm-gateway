// Package providers 测试 Router 原生旁路（NativeRelay）的路由逻辑。
// 复用 router_rerank_test.go 中的 mockLookup，额外用 nativeMockProvider 覆盖能力断言分支。
package providers

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainMockProvider 只实现 core.Provider，不实现 core.NativeRelayProvider。
// 嵌入 *mockProvider 复用已有空实现，避免重复样板。
type plainMockProvider struct {
	*mockProvider
}

// nativeMockProvider 同时实现 core.Provider 与 core.NativeRelayProvider。
type nativeMockProvider struct {
	*mockProvider
	// supports 控制 NativeRelayFormatFor 的返回
	supports bool
	// gotBody 记录透传给上游的原始请求体
	gotBody []byte
	// nonStreamResp 非流式返回值
	nonStreamResp []byte
	// streamBody 流式返回的 SSE 内容
	streamBody string
	// streams 统计 RelayNativeStream 调用次数
	streams int
}

func newNativeMockProvider(supports bool) *nativeMockProvider {
	return &nativeMockProvider{mockProvider: &mockProvider{}, supports: supports}
}

func (m *nativeMockProvider) NativeRelayFormatFor(_ string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	if !m.supports || format != core.NativeRelayClaude {
		return "", false
	}
	return core.NativeRelayClaude, true
}

func (m *nativeMockProvider) RelayNative(_ context.Context, body []byte) ([]byte, error) {
	m.gotBody = append([]byte(nil), body...)
	return m.nonStreamResp, nil
}

func (m *nativeMockProvider) RelayNativeStream(_ context.Context, body []byte) (io.ReadCloser, error) {
	m.gotBody = append([]byte(nil), body...)
	m.streams++
	return io.NopCloser(strings.NewReader(m.streamBody)), nil
}

// TestRouterRelayNativeProviderWithoutCapability 验证 provider 未实现 NativeRelayProvider 时
// 返回可被 errors.Is 命中的 ErrNativeNotSupported。
func TestRouterRelayNativeProviderWithoutCapability(t *testing.T) {
	lookup := newMockLookup()
	lookup.addModel("claude-model", "aws", "bedrock", &plainMockProvider{mockProvider: &mockProvider{}})
	router, err := NewRouter(lookup)
	require.NoError(t, err)

	req := &core.ChatRequest{Model: "claude-model"}

	body, err := router.RelayNative(context.Background(), req, []byte(`{"a":1}`), core.NativeRelayClaude)
	assert.Nil(t, body)
	require.Error(t, err)
	assert.True(t, errors.Is(err, core.ErrNativeNotSupported), "err=%v 应可被 errors.Is 命中 core 哨兵", err)

	stream, err := router.RelayNativeStream(context.Background(), req, []byte(`{"a":1}`), core.NativeRelayClaude)
	assert.Nil(t, stream)
	require.Error(t, err)
	assert.True(t, errors.Is(err, core.ErrNativeNotSupported), "err=%v 应可被 errors.Is 命中 core 哨兵", err)
}

// TestRouterRelayNativeFormatRejected 验证 provider 实现了能力接口但按模型拒绝该格式时，
// 同样返回 ErrNativeNotSupported。
func TestRouterRelayNativeFormatRejected(t *testing.T) {
	provider := newNativeMockProvider(false)
	lookup := newMockLookup()
	lookup.addModel("gpt-on-bedrock", "aws", "bedrock", provider)
	router, err := NewRouter(lookup)
	require.NoError(t, err)

	req := &core.ChatRequest{Model: "gpt-on-bedrock"}

	body, err := router.RelayNative(context.Background(), req, []byte(`{"a":1}`), core.NativeRelayClaude)
	assert.Nil(t, body)
	require.Error(t, err)
	assert.True(t, errors.Is(err, core.ErrNativeNotSupported), "err=%v 应可被 errors.Is 命中 core 哨兵", err)
	assert.Nil(t, provider.gotBody, "能力不满足时不应触达上游")

	_, err = router.RelayNativeStream(context.Background(), req, []byte(`{"a":1}`), core.NativeRelayClaude)
	require.Error(t, err)
	assert.True(t, errors.Is(err, core.ErrNativeNotSupported))
	assert.Equal(t, 0, provider.streams, "能力不满足时不应触达上游流式调用")
}

// TestRouterRelayNativePassthrough 验证能力满足时请求体透传、响应原样返回，
// 且 body.model 被改写为路由解析后的实际模型名（客户端发组名，provider 无法自行映射）。
func TestRouterRelayNativePassthrough(t *testing.T) {
	provider := newNativeMockProvider(true)
	provider.nonStreamResp = []byte(`{"id":"msg_1","type":"message"}`)
	provider.streamBody = "data: {}\n\n"

	lookup := newMockLookup()
	lookup.addModel("claude-model", "aws", "bedrock", provider)
	router, err := NewRouter(lookup)
	require.NoError(t, err)

	// 带 provider 类型前缀请求：req.Model != selector.Model，能验证改写生效
	req := &core.ChatRequest{Model: "bedrock/claude-model"}
	// 含协议独有字段（top_k / cache_control），验证零转换
	body := []byte(`{"model":"claude-sonnet-4-5","top_k":5,"system":[{"cache_control":{"type":"ephemeral"}}]}`)

	respBody, err := router.RelayNative(context.Background(), req, body, core.NativeRelayClaude)
	require.NoError(t, err)
	assert.Equal(t, provider.nonStreamResp, respBody)
	assert.JSONEq(t, `{"model":"claude-model","top_k":5,"system":[{"cache_control":{"type":"ephemeral"}}]}`,
		string(provider.gotBody), "body.model 应改写为 selector.Model，其余字段原样透传")

	stream, err := router.RelayNativeStream(context.Background(), req, body, core.NativeRelayClaude)
	require.NoError(t, err)
	defer stream.Close()
	read, err := io.ReadAll(stream)
	require.NoError(t, err)
	assert.Equal(t, provider.streamBody, string(read))
	assert.JSONEq(t, `{"model":"claude-model","top_k":5,"system":[{"cache_control":{"type":"ephemeral"}}]}`,
		string(provider.gotBody), "流式路径同样要改写 body.model")
	assert.Equal(t, 1, provider.streams)
}

// TestRouterRelayNativeRewritesGroupName 验证模型组名被改写为解析后的上游模型 ID。
func TestRouterRelayNativeRewritesGroupName(t *testing.T) {
	provider := newNativeMockProvider(true)
	lookup := newMockLookup()
	lookup.addModel("global.anthropic.claude-sonnet-4-5-20250929-v1:0", "aws", "bedrock", provider)
	router, err := NewRouter(lookup)
	require.NoError(t, err)

	// 请求用模型组名，lookup 里注册的是模型记录名
	req := &core.ChatRequest{Model: "aws/global.anthropic.claude-sonnet-4-5-20250929-v1:0"}
	_, err = router.RelayNative(context.Background(), req, []byte(`{"model":"claude-sonnet-4-5","max_tokens":16}`), core.NativeRelayClaude)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"global.anthropic.claude-sonnet-4-5-20250929-v1:0","max_tokens":16}`,
		string(provider.gotBody))
}

// TestRouterRelayNativeRewritesMissingModelField 验证 body 缺 model 字段时补上。
func TestRouterRelayNativeRewritesMissingModelField(t *testing.T) {
	provider := newNativeMockProvider(true)
	lookup := newMockLookup()
	lookup.addModel("claude-model", "aws", "bedrock", provider)
	router, err := NewRouter(lookup)
	require.NoError(t, err)

	_, err = router.RelayNative(context.Background(), &core.ChatRequest{Model: "claude-model"}, []byte(`{"max_tokens":8}`), core.NativeRelayClaude)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"claude-model","max_tokens":8}`, string(provider.gotBody))
}

// TestRouterRelayNativeBodyRewriteFailure 验证 body 无法改写时返回哨兵错误、
// 且不触达上游（宁可降级也不能把错模型发出去）。
func TestRouterRelayNativeBodyRewriteFailure(t *testing.T) {
	provider := newNativeMockProvider(true)
	lookup := newMockLookup()
	lookup.addModel("claude-model", "aws", "bedrock", provider)
	router, err := NewRouter(lookup)
	require.NoError(t, err)

	req := &core.ChatRequest{Model: "claude-model"}
	for _, body := range [][]byte{[]byte(`not-json`), []byte(`[1,2]`), []byte(`null`), nil} {
		_, err := router.RelayNative(context.Background(), req, body, core.NativeRelayClaude)
		require.Error(t, err, "body=%q", body)
		assert.True(t, errors.Is(err, core.ErrNativeNotSupported), "body=%q err=%v", body, err)

		_, err = router.RelayNativeStream(context.Background(), req, body, core.NativeRelayClaude)
		require.Error(t, err, "body=%q", body)
		assert.True(t, errors.Is(err, core.ErrNativeNotSupported), "body=%q err=%v", body, err)
	}
	assert.Nil(t, provider.gotBody, "改写失败时不应触达上游")
	assert.Equal(t, 0, provider.streams)
}

// TestRouterRelayNativeModelNotFound 验证路由失败也归一到 ErrNativeNotSupported（调用方只需识别一个信号）。
func TestRouterRelayNativeModelNotFound(t *testing.T) {
	provider := newNativeMockProvider(true)
	lookup := newMockLookup()
	lookup.addModel("claude-model", "aws", "bedrock", provider)
	router, err := NewRouter(lookup)
	require.NoError(t, err)

	_, err = router.RelayNative(context.Background(), &core.ChatRequest{Model: "no-such-model"}, []byte(`{}`), core.NativeRelayClaude)
	require.Error(t, err)
	assert.True(t, errors.Is(err, core.ErrNativeNotSupported), "err=%v 应可被 errors.Is 命中 core 哨兵", err)
	assert.Contains(t, err.Error(), "no-such-model")
}