package service

import (
	"encoding/json"
	"testing"
)

// TestUpdateModelInputVideoTriState 钉住 override_supports_video_input 的三态语义：
// 缺省（字段没出现）= 不动，显式 null = 清除（跟随 catalog），true/false = 强制。
// 搞错的后果很实在：把"缺省"当成"清除"，任何只改价格的 PUT 都会顺手把视频能力打掉。
func TestUpdateModelInputVideoTriState(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantSet   bool
		wantValue *bool
	}{
		{"缺省", `{"name":"m"}`, false, nil},
		{"显式 null", `{"override_supports_video_input":null}`, true, nil},
		{"true", `{"override_supports_video_input":true}`, true, func() *bool { v := true; return &v }()},
		{"false", `{"override_supports_video_input":false}`, true, func() *bool { v := false; return &v }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var input UpdateModelInput
			if err := json.Unmarshal([]byte(tc.body), &input); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got := input.OverrideSupportsVideoInput
			if got.Set != tc.wantSet {
				t.Fatalf("Set = %v, want %v", got.Set, tc.wantSet)
			}
			if (got.Value == nil) != (tc.wantValue == nil) {
				t.Fatalf("Value = %v, want %v", got.Value, tc.wantValue)
			}
			if got.Value != nil && *got.Value != *tc.wantValue {
				t.Fatalf("Value = %v, want %v", *got.Value, *tc.wantValue)
			}
		})
	}
}
