package auth

import "os"

// Config holds OAuth2/OIDC configuration for DaQun SSO
type Config struct {
	// OIDC Issuer URL (discovery endpoint base)
	IssuerURL string
	// OAuth2 Client ID
	ClientID string
	// OAuth2 Client Secret
	ClientSecret string
	// OAuth2 Redirect URI (callback URL)
	RedirectURI string
	// Scopes to request
	Scopes []string
	// JWT signing secret for session tokens issued by this service
	JWTSecret string
	// Frontend URL to redirect after login
	FrontendURL string
}

// LoadConfig reads OAuth/SSO configuration from environment variables
func LoadConfig() *Config {
	scopes := os.Getenv("OAUTH_SCOPES")
	if scopes == "" {
		scopes = "openid profile email"
	}

	return &Config{
		IssuerURL:    getEnvOrDefault("OAUTH_ISSUER_URL", "https://oauth.example.net"),
		ClientID:     os.Getenv("OAUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("OAUTH_CLIENT_SECRET"),
		RedirectURI:  os.Getenv("OAUTH_REDIRECT_URI"),
		Scopes:       splitScopes(scopes),
		JWTSecret:    getEnvOrDefault("JWT_SECRET", "change-me-in-production"),
		FrontendURL:  getEnvOrDefault("FRONTEND_URL", "http://localhost:5173"),
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func splitScopes(s string) []string {
	var scopes []string
	current := ""
	for _, c := range s {
		if c == ' ' || c == ',' {
			if current != "" {
				scopes = append(scopes, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		scopes = append(scopes, current)
	}
	return scopes
}
