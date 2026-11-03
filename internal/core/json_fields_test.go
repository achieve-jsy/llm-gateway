package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnknownJSONFields_Delete(t *testing.T) {
	tests := []struct {
		name      string
		initial   string
		deleteKey string
		want      string
	}{
		{
			name:      "删除单个字段",
			initial:   `{"enable_thinking":false,"custom_field":"value"}`,
			deleteKey: "enable_thinking",
			want:      `{"custom_field":"value"}`,
		},
		{
			name:      "删除不存在的字段不变",
			initial:   `{"custom_field":"value"}`,
			deleteKey: "enable_thinking",
			want:      `{"custom_field":"value"}`,
		},
		{
			name:      "删除唯一字段后为空",
			initial:   `{"enable_thinking":false}`,
			deleteKey: "enable_thinking",
			want:      `{}`,
		},
		{
			name:      "空对象删除字段不变",
			initial:   `{}`,
			deleteKey: "enable_thinking",
			want:      `{}`,
		},
		{
			name:      "nil 对象删除字段不变",
			initial:   "",
			deleteKey: "enable_thinking",
			want:      "",
		},
		{
			name:      "删除中间字段",
			initial:   `{"a":1,"b":2,"c":3}`,
			deleteKey: "b",
			want:      `{"a":1,"c":3}`,
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

func TestUnknownJSONFields_Set(t *testing.T) {
	tests := []struct {
		name     string
		initial  string
		setKey   string
		setValue string
		want     string
	}{
		{
			name:     "空对象上新增",
			initial:  `{}`,
			setKey:   "response_format",
			setValue: `{"type":"json_object"}`,
			want:     `{"response_format":{"type":"json_object"}}`,
		},
		{
			name:     "nil 对象上新增",
			initial:  "",
			setKey:   "response_format",
			setValue: `{"type":"json_object"}`,
			want:     `{"response_format":{"type":"json_object"}}`,
		},
		{
			name:     "覆盖已有键",
			initial:  `{"response_format":{"type":"json_schema","strict":true},"custom_field":"v"}`,
			setKey:   "response_format",
			setValue: `{"type":"json_object"}`,
			want:     `{"response_format":{"type":"json_object"},"custom_field":"v"}`,
		},
		{
			name:     "新增键且保留其他键",
			initial:  `{"custom_field":"v"}`,
			setKey:   "response_format",
			setValue: `{"type":"json_object"}`,
			want:     `{"response_format":{"type":"json_object"},"custom_field":"v"}`,
		},
		{
			name:     "空值删除键",
			initial:  `{"response_format":{"type":"json_object"},"custom_field":"v"}`,
			setKey:   "response_format",
			setValue: "",
			want:     `{"custom_field":"v"}`,
		},
		{
			name:     "空值删除唯一键后为空",
			initial:  `{"response_format":{"type":"json_object"}}`,
			setKey:   "response_format",
			setValue: "",
			want:     `{}`,
		},
		{
			name:     "对象值保留嵌套结构",
			initial:  `{"a":1}`,
			setKey:   "b",
			setValue: `{"x":[1,2],"y":null}`,
			want:     `{"a":1,"b":{"x":[1,2],"y":null}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fields UnknownJSONFields
			if tt.initial != "" {
				fields = UnknownJSONFields{raw: json.RawMessage(tt.initial)}
			}

			fields.Set(tt.setKey, json.RawMessage(tt.setValue))

			assert.JSONEq(t, tt.want, string(fields.raw))
			if tt.want == "{}" {
				assert.True(t, fields.IsEmpty())
			}
		})
	}
}

func TestUnknownJSONFields_Set_LookupRoundTrip(t *testing.T) {
	fields := UnknownJSONFields{raw: json.RawMessage(`{"a":1}`)}
	fields.Set("response_format", json.RawMessage(`{"type":"json_object"}`))

	got := fields.Lookup("response_format")
	if got == nil {
		t.Fatal("expected response_format to be present after Set")
	}
	assert.JSONEq(t, `{"type":"json_object"}`, string(got))
}
