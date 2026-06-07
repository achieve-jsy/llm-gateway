package middleware

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// TestSanitizeBodyRedacts 验证 P0 修复：递归脱敏 + key/credentials/sk- 识别。
func TestSanitizeBodyRedacts(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantKey string
	}{
		{name: "nested data.key", in: `{"data":{"key":"sk-plaintext-123","name":"myname"}}`, wantKey: "data.key"},
		{name: "top-level key", in: `{"key":"sk-plaintext-123","name":"myname"}`, wantKey: "key"},
		{name: "credentials key", in: `{"name":"p","credentials":"whatever"}`, wantKey: "credentials"},
		{name: "sk value under neutral key", in: `{"data":{"value":"sk-12345"}}`, wantKey: "data.value"},
		{name: "top-level array", in: `[{"api_key":"x"},{"name":"keep"}]`, wantKey: "0.api_key"},
		{name: "nested token", in: `{"data":{"token":"abc"}}`, wantKey: "data.token"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeBody([]byte(tc.in))
			var root any
			if err := json.Unmarshal([]byte(got), &root); err != nil {
				t.Fatalf("sanitizeBody produced invalid JSON %q: %v", got, err)
			}
			val := getByPath(root, strings.Split(tc.wantKey, "."))
			if s, ok := val.(string); !ok || s != "***" {
				t.Fatalf("path %q not redacted, got %v (full=%s)", tc.wantKey, val, got)
			}
		})
	}
}

// TestSanitizeBodyPreserves 验证非敏感字段不被误伤。
func TestSanitizeBodyPreserves(t *testing.T) {
	in := `{"name":"myname","data":{"id":"550e8400-e29b-41d4-a716-446655440000","models":["m1","m2"]}}`
	got := sanitizeBody([]byte(in))
	var root map[string]any
	if err := json.Unmarshal([]byte(got), &root); err != nil {
		t.Fatalf("invalid JSON: %v (%s)", err, got)
	}
	data := root["data"].(map[string]any)
	if data["id"] != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("id should be preserved, got %v", data["id"])
	}
	if data["models"] == nil {
		t.Errorf("models should be preserved, got %v", data["models"])
	}
}

// getByPath 沿点分路径取 JSON 值（支持数组下标）。
func getByPath(v any, path []string) any {
	cur := v
	for _, seg := range path {
		switch t := cur.(type) {
		case map[string]any:
			cur = t[seg]
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(t) {
				return nil
			}
			cur = t[idx]
		default:
			return nil
		}
	}
	return cur
}
