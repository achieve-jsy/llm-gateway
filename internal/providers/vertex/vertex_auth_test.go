package vertex

import (
	"testing"

	"github.com/example/llm-gateway/internal/providers"
)

func TestNormalizeAuthType_AutoDetectAPIKey(t *testing.T) {
	cases := []struct {
		name string
		cfg  providers.ProviderConfig
		want string
	}{
		{
			name: "api_key present, no SA -> api_key (SDK vertexai+api_key semantic)",
			cfg:  providers.ProviderConfig{APIKey: "AQ.Ab8RN6..."},
			want: authTypeAPIKey,
		},
		{
			name: "explicit api_key -> api_key",
			cfg:  providers.ProviderConfig{APIKey: "AQ.Ab8RN6...", AuthType: "api_key"},
			want: authTypeAPIKey,
		},
		{
			name: "api_key present but SA set -> service_account wins",
			cfg: providers.ProviderConfig{
				APIKey:             "AQ.Ab8RN6...",
				ServiceAccountJSON: `{"type":"service_account"}`,
			},
			want: authTypeServiceAccount,
		},
		{
			name: "explicit gcp_adc -> gcp_adc",
			cfg:  providers.ProviderConfig{APIKey: "AQ.Ab8RN6...", AuthType: "gcp_adc"},
			want: authTypeGCPADC,
		},
		{
			name: "no api_key, no SA -> gcp_adc (unchanged default)",
			cfg:  providers.ProviderConfig{},
			want: authTypeGCPADC,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeAuthType(tc.cfg); got != tc.want {
				t.Errorf("normalizeAuthType(%+v) = %q, want %q", tc.cfg, got, tc.want)
			}
		})
	}
}
