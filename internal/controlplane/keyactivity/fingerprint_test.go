package keyactivity

import (
	"strings"
	"testing"
)

func TestNormalizeUA(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"OpenAI Python", "OpenAI/Python 1.55.3", "openai/python"},
		{"OpenAI Python newer version", "OpenAI/Python 1.55.4", "openai/python"},
		{"python-requests", "python-requests/2.31.0", "python-requests/"},
		{"curl", "curl/8.0", "curl/"},
		{"empty", "", "unknown"},
		{"whitespace only", "   ", "unknown"},
		{"uppercase", "CURL/7.88.1", "curl/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeUA(tt.raw)
			if got != tt.want {
				t.Errorf("NormalizeUA(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestUAFingerprintStability(t *testing.T) {
	for _, ua := range []string{"OpenAI/Python 1.55.3", "curl/8.0", "python-requests/2.31.0", ""} {
		fp1 := UAFingerprint(ua)
		fp2 := UAFingerprint(ua)
		if fp1 != fp2 {
			t.Errorf("UAFingerprint not stable for %q: %q != %q", ua, fp1, fp2)
		}
		if len(fp1) != 16 {
			t.Errorf("UAFingerprint length = %d, want 16", len(fp1))
		}
	}
}

func TestUAFingerprintDistinct(t *testing.T) {
	openai := UAFingerprint("OpenAI/Python 1.55.3")
	curl := UAFingerprint("curl/8.0")
	reqs := UAFingerprint("python-requests/2.31.0")
	empty := UAFingerprint("")

	fps := []string{openai, curl, reqs, empty}
	for i := 0; i < len(fps); i++ {
		for j := i + 1; j < len(fps); j++ {
			if fps[i] == fps[j] {
				t.Errorf("fingerprints collide: %q and %q", fps[i], fps[j])
			}
		}
	}
}

func TestUAFingerprintVersionsMerged(t *testing.T) {
	fp1 := UAFingerprint("OpenAI/Python 1.55.3")
	fp2 := UAFingerprint("OpenAI/Python 1.55.4")
	fp3 := UAFingerprint("OpenAI/Python 2.0.0")
	if fp1 != fp2 || fp1 != fp3 {
		t.Errorf("same client different versions should produce same fp: %q, %q, %q", fp1, fp2, fp3)
	}
}

func TestSourceFP(t *testing.T) {
	fp1 := SourceFP("curl/8.0", "10.0.0.1", true)
	if fp1 != "10.0.0.1" {
		t.Errorf("SourceFP should be just IP: got %q", fp1)
	}
	fp2 := SourceFP("curl/8.0", "", true)
	if len(fp2) != 16 {
		t.Errorf("empty ip should fallback to UA fingerprint (16 hex chars): got %q (len=%d)", fp2, len(fp2))
	}
	fp3 := SourceFP("curl/8.0", "10.0.0.1", false)
	if strings.Contains(fp3, "|") {
		t.Errorf("ipAsSource=false should not include ip: got %q", fp3)
	}
}

func TestIsSilentUserAgent(t *testing.T) {
	cases := []struct {
		ua   string
		want bool
	}{
		{"Trae/2.3.47348", true},
		{"Mozilla/5.0 TRAE/2.3.47348 Electron/33.2.1", true},
		{"Codex Desktop/0.145.0", false},
		{"curl/8.0", false},
	}
	for _, tc := range cases {
		if got := IsSilentUserAgent(tc.ua); got != tc.want {
			t.Fatalf("IsSilentUserAgent(%q) = %v, want %v", tc.ua, got, tc.want)
		}
	}
}
