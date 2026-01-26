package model

import (
	"testing"
)

func TestModel_UpstreamName(t *testing.T) {
	cases := []struct {
		name            string
		modelName       string
		upstreamModelID string
		want            string
	}{
		{
			name:            "UpstreamModelID 优先",
			modelName:       "ZHIPU/GLM-5.2",
			upstreamModelID: "GLM-5.2-FP8",
			want:            "GLM-5.2-FP8",
		},
		{
			name:      "剥离 provider/ 前缀",
			modelName: "ZHIPU/GLM-5.2",
			want:      "GLM-5.2",
		},
		{
			name:      "bedrock 前缀剥离",
			modelName: "bedrock/global.anthropic.claude-opus-4-6-v1",
			want:      "global.anthropic.claude-opus-4-6-v1",
		},
		{
			name:      "无前缀直接返回",
			modelName: "deepseek-v4-pro",
			want:      "deepseek-v4-pro",
		},
		{
			name:      "空 Name",
			modelName: "",
			want:      "",
		},
		{
			name:            "UpstreamModelID 为空字符串时走 Name",
			modelName:       "ZHIPU/GLM-5.2",
			upstreamModelID: "",
			want:            "GLM-5.2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{
				Name:            tc.modelName,
				UpstreamModelID: tc.upstreamModelID,
			}
			got := m.UpstreamName()
			if got != tc.want {
				t.Errorf("UpstreamName() = %q, want %q", got, tc.want)
			}
		})
	}
}
