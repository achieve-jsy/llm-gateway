package providers

import (
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

// TestNativeOpenAIPath 钉死端点判定：与 passthrough_semantics.go 的命名保持一致。
func TestNativeOpenAIPath(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "chat", body: `{"model":"m","messages":[]}`, want: "/chat/completions"},
		{name: "responses", body: `{"model":"m","input":"hi"}`, want: "/responses"},
		{name: "both prefers chat", body: `{"model":"m","messages":[],"input":"hi"}`, want: "/chat/completions"},
		{name: "invalid json", body: `{oops`, wantErr: true},
		{name: "neither", body: `{"model":"m"}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NativeOpenAIPath([]byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatal("应返回错误")
				}
				if !errors.Is(err, core.ErrNativeNotSupported) {
					t.Errorf("err = %v, 应包装 ErrNativeNotSupported 以便降级", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("path = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNativeFormatForOpenAI 确认 OpenAI 兼容渠道接受 Chat/Responses、拒绝其他格式。
//
// model 参数是契约要求的形式（按模型判断），本实现有意忽略它——
// 理由见 NativeFormatForOpenAI 的注释；跨 provider 的行为矩阵由
// tests/providers/openai_compat_native_format_test.go 覆盖。
func TestNativeFormatForOpenAI(t *testing.T) {
	for _, f := range []core.NativeRelayFormat{core.NativeRelayOpenAIChat, core.NativeRelayOpenAIResponses} {
		if _, ok := NativeFormatForOpenAI("any-model", f); !ok {
			t.Errorf("format %q 应被接受", f)
		}
	}
	for _, f := range []core.NativeRelayFormat{core.NativeRelayClaude, core.NativeRelayGemini, ""} {
		if _, ok := NativeFormatForOpenAI("any-model", f); ok {
			t.Errorf("format %q 不应被接受", f)
		}
	}
	// 判定与 model 无关：换模型名结果不变。
	for _, m := range []string{"", "gpt-4o", "deepseek-chat"} {
		if _, ok := NativeFormatForOpenAI(m, core.NativeRelayOpenAIChat); !ok {
			t.Errorf("model %q 的 Chat 判定不应变化", m)
		}
	}
}
