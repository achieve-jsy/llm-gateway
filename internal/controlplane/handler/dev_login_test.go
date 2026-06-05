package handler

import "testing"

func TestDevLoginEnabled(t *testing.T) {
	cases := []struct {
		name      string
		appEnv    string
		enabled   string
		prodAllow string
		want      bool
	}{
		{"default all off", "", "", "", false},
		{"dev/test explicit on", "test", "true", "", true},
		{"prod explicit on but no prod-allow", "prod", "true", "", false},
		{"prod explicit on with prod-allow", "prod", "true", "true", true},
		{"prod enabled flag only", "prod", "true", "false", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.appEnv)
			t.Setenv("DEV_LOGIN_ENABLED", tc.enabled)
			t.Setenv("DEV_LOGIN_PROD_ALLOWED", tc.prodAllow)
			if got := DevLoginEnabled(); got != tc.want {
				t.Fatalf("DevLoginEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
