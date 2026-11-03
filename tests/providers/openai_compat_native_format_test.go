package providers_test

// 「按模型判断」约定的行为钉桩。
//
// core.NativeRelayProvider 的契约默认要求「按模型判断而非按渠道」。
// deepseek / groq / xai / ollama 是纯 OpenAI 兼容服务，名下全部模型都支持
// /chat/completions 与 /responses，所以它们共用 providers.NativeFormatForOpenAI
// 并有意忽略 model —— 这是契约允许的例外，前提是渠道内确实没有能力差异。
//
// 本测试锁住两件事：
//   1. 四家对 Chat / Responses 都声明原生、对 Claude / Gemini 一律拒绝；
//   2. 判定结果与 model 无关（换任何模型名结果相同）。
// 一旦某家开始提供能力不同的模型，第 2 条会失败，提示必须改成按模型分支。

import (
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers/deepseek"
	"github.com/example/llm-gateway/internal/providers/groq"
	"github.com/example/llm-gateway/internal/providers/ollama"
	"github.com/example/llm-gateway/internal/providers/xai"
	"github.com/stretchr/testify/require"
)

// nativeFormatter 让四家能以同一张表跑测试。
type nativeFormatter interface {
	NativeRelayFormatFor(model string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool)
}

func openAICompatProviders() map[string]nativeFormatter {
	return map[string]nativeFormatter{
		"deepseek": deepseek.NewWithHTTPClient("k", "http://localhost:1", nil, llmclient.Hooks{}),
		"groq":     groq.NewWithHTTPClient("k", nil, llmclient.Hooks{}),
		"xai":      xai.NewWithHTTPClient("k", nil, llmclient.Hooks{}),
		"ollama":   ollama.NewWithHTTPClient("k", nil, llmclient.Hooks{}),
	}
}

func TestOpenAICompatProviders_DeclareChatAndResponsesNative(t *testing.T) {
	formats := []core.NativeRelayFormat{
		core.NativeRelayOpenAIChat,
		core.NativeRelayOpenAIResponses,
	}
	for name, p := range openAICompatProviders() {
		for _, f := range formats {
			got, ok := p.NativeRelayFormatFor("any-model", f)
			require.True(t, ok, "%s 应声明支持 %s 原生", name, f)
			require.Equal(t, f, got, "%s 对 %s 应原样返回格式", name, f)
		}
	}
}

func TestOpenAICompatProviders_RejectForeignFormats(t *testing.T) {
	for name, p := range openAICompatProviders() {
		for _, f := range []core.NativeRelayFormat{core.NativeRelayClaude, core.NativeRelayGemini} {
			got, ok := p.NativeRelayFormatFor("any-model", f)
			require.False(t, ok, "%s 不该声明支持 %s 原生", name, f)
			require.Equal(t, core.NativeRelayFormat(""), got)
		}
	}
}

// 判定与 model 无关 —— 这是「忽略 model」这一例外的显式契约。
// 若将来某家出现能力不同的模型，此测试会失败，正是改代码的信号。
func TestOpenAICompatProviders_FormatDecisionIsModelIndependent(t *testing.T) {
	modelNames := []string{
		"",
		"gpt-4o",
		"deepseek-chat",
		"deepseek-reasoner",
		"some-embedding-model",
		"namespace/arbitrary:tag",
	}
	for name, p := range openAICompatProviders() {
		for _, f := range []core.NativeRelayFormat{core.NativeRelayOpenAIChat, core.NativeRelayOpenAIResponses} {
			base, baseOK := p.NativeRelayFormatFor(modelNames[0], f)
			for _, m := range modelNames[1:] {
				got, ok := p.NativeRelayFormatFor(m, f)
				require.Equal(t, baseOK, ok, "%s 对 %q 的 %s 判定应与其它模型一致", name, m, f)
				require.Equal(t, base, got, "%s 对 %q 的 %s 格式应与其它模型一致", name, m, f)
			}
		}
	}
}
