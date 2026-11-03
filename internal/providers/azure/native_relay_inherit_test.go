package azure

// azure 通过指针嵌入 *openai.CompatibleProvider 自动继承原生旁路能力。
// 这里用编译期断言锁住这件事：一旦有人把嵌入改成字段委托（或换掉实现），
// 编译立即失败，提醒补上转发方法。

import (
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

func TestNativeRelayInheritedFromCompatibleProvider(t *testing.T) {
	var _ core.NativeRelayProvider = (*Provider)(nil)

	p := NewWithHTTPClient("test-key", nil, llmclient.Hooks{})
	if got, ok := p.NativeRelayFormatFor("gpt-5", core.NativeRelayOpenAIChat); !ok || got != core.NativeRelayOpenAIChat {
		t.Errorf("azure 应继承 Chat 原生能力: format=%q ok=%v", got, ok)
	}
	if _, ok := p.NativeRelayFormatFor("gpt-5", core.NativeRelayClaude); ok {
		t.Error("azure 不应声明 Claude 原生能力")
	}
}
