package service_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/service"
)

func TestIsMaskedSecret(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"••••••", true},
		{"******", true},
		{"********", true},
		{"  ••••••  ", true},
		{"JHk#7wt#3v)R@W2jP)", false},
		{"real-password", false},
	}
	for _, tc := range cases {
		if got := service.IsMaskedSecret(tc.in); got != tc.want {
			t.Fatalf("IsMaskedSecret(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}
