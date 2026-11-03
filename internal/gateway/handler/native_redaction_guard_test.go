package handler

// 脱敏 × 原生旁路互斥守卫。
//
// 原生旁路发的是客户端原始 body，脱敏改的是解析后的 ChatRequest。
// 两者同时成立等价于把未脱敏原文直发上游；detect 模式下虽然不改写，
// 但原生旁路会让检测引擎看不到正文，统计直接失效。
// 所以除 off 外一律禁用原生旁路。

import (
	"testing"

	"github.com/example/llm-gateway/internal/gateway/redact"
)

func TestNativeRelayBlockedByRedaction(t *testing.T) {
	cases := []struct {
		name string
		mode string
		want bool
	}{
		{"nil engine", "", false},
		{"off", redact.ModeOff, false},
		{"detect", redact.ModeDetect, true},
		{"mask", redact.ModeMask, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var engine *redact.Engine
			if tc.mode != "" {
				e, err := redact.New(redact.Config{Mode: tc.mode, RulesOnly: true})
				if err != nil {
					t.Fatalf("构造脱敏引擎失败: %v", err)
				}
				engine = e
			}

			if got := nativeRelayBlockedByRedaction(engine); got != tc.want {
				t.Fatalf("nativeRelayBlockedByRedaction(mode=%q) = %v, 期望 %v", tc.mode, got, tc.want)
			}
		})
	}
}
