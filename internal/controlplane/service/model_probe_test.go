package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractRequiredTemperature(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   *float64
	}{
		{
			name:   "kimi-k3 实际错误格式",
			errMsg: "http 400: {\"error\":{\"message\":\"invalid temperature: only 0.6 is allowed for this model\",\"type\":\"InvalidParameter\"}}",
			want:   floatPtr(0.6),
		},
		{
			name:   "整数 temperature",
			errMsg: "invalid temperature: only 1 is allowed",
			want:   floatPtr(1.0),
		},
		{
			name:   "不匹配的错误信息",
			errMsg: "model not found",
			want:   nil,
		},
		{
			name:   "有 invalid temperature 但无 only",
			errMsg: "invalid temperature parameter",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractRequiredTemperature(tt.errMsg)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.InDelta(t, *tt.want, *got, 0.0001)
			}
		})
	}
}

func floatPtr(f float64) *float64 {
	return &f
}