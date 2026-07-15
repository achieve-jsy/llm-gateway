package providers

import "testing"

func TestValidVertexProviderConfig_ApiKeyAuth(t *testing.T) {
	cases := []struct {
		name string
		raw  RawProviderConfig
		want bool
	}{
		{
			name: "vertex api_key auth accepted when key present",
			raw: RawProviderConfig{
				Type:           "vertex",
				AuthType:       "api_key",
				APIKey:         "AQ.Ab8RN6...",
				VertexProject:  "793586848164",
				VertexLocation: "asia-southeast1",
			},
			want: true,
		},
		{
			name: "gemini+backend=vertex treated as vertex api_key accepted",
			raw: RawProviderConfig{
				Type:           "gemini",
				Backend:        "vertex",
				AuthType:       "api_key",
				APIKey:         "AQ.Ab8RN6...",
				VertexProject:  "791838655877",
				VertexLocation: "asia-southeast1",
			},
			want: true,
		},
		{
			name: "vertex api_key rejected when key absent",
			raw: RawProviderConfig{
				Type:           "vertex",
				AuthType:       "api_key",
				VertexProject:  "793586848164",
				VertexLocation: "asia-southeast1",
			},
			want: false,
		},
		{
			name: "vertex api_key rejected when project/location absent",
			raw: RawProviderConfig{
				Type:     "vertex",
				AuthType: "api_key",
				APIKey:   "AQ.Ab8RN6...",
			},
			want: false,
		},
		{
			name: "vertex gcp_adc still accepted (unchanged)",
			raw: RawProviderConfig{
				Type:           "vertex",
				AuthType:       "gcp_adc",
				VertexProject:  "793586848164",
				VertexLocation: "asia-southeast1",
			},
			want: true,
		},
		{
			name: "vertex unsupported auth type rejected",
			raw: RawProviderConfig{
				Type:           "vertex",
				AuthType:       "something-else",
				APIKey:         "AQ.Ab8RN6...",
				VertexProject:  "793586848164",
				VertexLocation: "asia-southeast1",
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validVertexProviderConfig(tc.raw); got != tc.want {
				t.Errorf("validVertexProviderConfig(%+v) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeProviderType_GeminiBackendVertex(t *testing.T) {
	raw := RawProviderConfig{Type: "gemini", Backend: "vertex"}
	if got := normalizeProviderType(raw); got != "vertex" {
		t.Errorf("normalizeProviderType(gemini+backend=vertex) = %q, want %q", got, "vertex")
	}
	// plain gemini (no backend) stays gemini
	if got := normalizeProviderType(RawProviderConfig{Type: "gemini"}); got != "gemini" {
		t.Errorf("normalizeProviderType(gemini) = %q, want %q", got, "gemini")
	}
}
