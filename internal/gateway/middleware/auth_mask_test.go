package middleware

import "testing"

// 日志里不能出现完整密钥，只能留末 4 位。
func TestMaskKeySuffix(t *testing.T) {
	cases := map[string]string{
		"":                   "****",
		"short":              "****",
		"sk-1234567890abcdef": "****cdef",
	}
	for in, want := range cases {
		if got := maskKeySuffix(in); got != want {
			t.Fatalf("maskKeySuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
