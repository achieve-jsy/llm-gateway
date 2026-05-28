package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnknownJSONFields_Delete(t *testing.T) {
	tests := []struct {
		name     string
		initial  string
		deleteKey string
		want     string
	}{
		{
			name:     "删除单个字段",
			initial:  `{"enable_thinking":false,"custom_field":"value"}`,
			deleteKey: "enable_thinking",
			want:     `{"custom_field":"value"}`,
		},
		{
			name:     "删除不存在的字段不变",
			initial:  `{"custom_field":"value"}`,
			deleteKey: "enable_thinking",
			want:     `{"custom_field":"value"}`,
		},
		{
			name:     "删除唯一字段后为空",
			initial:  `{"enable_thinking":false}`,
			deleteKey: "enable_thinking",
			want:     `{}`,
		},
		{
			name:     "空对象删除字段不变",
			initial:  `{}`,
			deleteKey: "enable_thinking",
			want:     `{}`,
		},
		{
			name:     "nil 对象删除字段不变",
			initial:  "",
			deleteKey: "enable_thinking",
			want:     "",
		},
		{
			name:     "删除中间字段",
			initial:  `{"a":1,"b":2,"c":3}`,
			deleteKey: "b",
			want:     `{"a":1,"c":3}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fields UnknownJSONFields
			if tt.initial != "" {
				fields = UnknownJSONFields{raw: json.RawMessage(tt.initial)}
			}

			fields.Delete(tt.deleteKey)

			if tt.want == "" {
				assert.True(t, fields.IsEmpty())
			} else {
				assert.JSONEq(t, tt.want, string(fields.raw))
			}
		})
	}
}

