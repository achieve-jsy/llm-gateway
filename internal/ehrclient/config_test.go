package ehrclient

import (
	"os"
	"testing"
)

func TestDefaultAPIPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		base   string
		expect string
	}{
		{"https://open.example.com", prodAPIPrefix},
		{"https://open.test.daqun.chat.com", testAPIPrefix},
		{"http://127.0.0.1:8080", prodAPIPrefix},
	}
	for _, tc := range tests {
		if got := DefaultAPIPrefix(tc.base); got != tc.expect {
			t.Fatalf("DefaultAPIPrefix(%q) = %q, want %q", tc.base, got, tc.expect)
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("EHR_BASE_URL", "https://open.test.daqun.chat.com")
	t.Setenv("EHR_API_TOKEN", "test-token-xyz")
	t.Setenv("EHR_API_PREFIX", "")

	cfg := ConfigFromEnv()
	if cfg.BaseURL != "https://open.test.daqun.chat.com" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Token != "test-token-xyz" {
		t.Fatalf("Token = %q", cfg.Token)
	}
	if cfg.APIPrefix != testAPIPrefix {
		t.Fatalf("APIPrefix = %q, want %q", cfg.APIPrefix, testAPIPrefix)
	}
}

func TestConfigFromEnv_CustomPrefix(t *testing.T) {
	t.Setenv("EHR_BASE_URL", "")
	t.Setenv("EHR_API_TOKEN", "")
	t.Setenv("EHR_API_PREFIX", "open-apis/custom")

	cfg := ConfigFromEnv()
	if cfg.APIPrefix != "/open-apis/custom" {
		t.Fatalf("APIPrefix = %q", cfg.APIPrefix)
	}
}

func TestClient_resourcePath(t *testing.T) {
	c := NewClient(Config{BaseURL: "http://example.com", APIPrefix: testAPIPrefix, Token: "t"})
	if got := c.resourcePath("organizations"); got != "/open-apis/ehrapi/resource/v1/organizations" {
		t.Fatalf("resourcePath = %q", got)
	}
}

func TestConfigFromEnv_EmptyUsesDefaults(t *testing.T) {
	os.Unsetenv("EHR_BASE_URL")
	os.Unsetenv("EHR_API_TOKEN")
	os.Unsetenv("EHR_API_PREFIX")

	cfg := ConfigFromEnv()
	if cfg.BaseURL != defaultBaseURL {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.APIPrefix != prodAPIPrefix {
		t.Fatalf("APIPrefix = %q", cfg.APIPrefix)
	}
}
