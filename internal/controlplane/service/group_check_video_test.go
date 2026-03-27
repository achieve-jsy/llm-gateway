package service

import "testing"

func TestIsVideoGroupModelType(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"video", true},
		{"VIDEO", true},
		{"video_generation", true},
		{" chat ", false},
		{"embedding", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isVideoGroupModelType(tc.in); got != tc.want {
			t.Fatalf("isVideoGroupModelType(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}
