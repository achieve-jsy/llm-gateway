package handler

// 脱敏 × 原生旁路互斥守卫。
//
// 原生旁路发的是客户端原始 body，脱敏改的是解析后的 ChatRequest。
// 两者同时成立等价于把未脱敏原文直发上游，所以 mask 必须禁用原生。
// detect 只统计不改写，转发原文不构成泄漏，因此不禁用（否则测试环境
// 常驻 detect 会让原生旁路永远无法验证）。

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
		{"detect", redact.ModeDetect, false},
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
