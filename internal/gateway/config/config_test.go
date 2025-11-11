package config

import (
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLoad_Defaults tests config loading with default values
func TestLoad_Defaults(t *testing.T) {
	tests := []struct {
		name               string
		setupEnv           func()
		cleanupEnv         func()
		expectGatewayPort  string
		expectRedisAddr    string
		expectRedisDB      int
		expectOTLPEndpoint string
		expectOTLPEnabled  bool
		expectCPURL        string
	}{
		{
			name: "load defaults when no env vars set",
			setupEnv: func() {
				// Clear all gateway env vars
				os.Unsetenv("GATEWAY_PORT")
				os.Unsetenv("REDIS_ADDR")
				os.Unsetenv("REDIS_DB")
				os.Unsetenv("REDIS_PASSWORD")
				os.Unsetenv("OTLP_ENDPOINT")
				os.Unsetenv("OTLP_ENABLED")
				os.Unsetenv("INTERNAL_TOKEN")
				os.Unsetenv("CONTROL_PLANE_URL")
			},
			cleanupEnv: func() {
				// Cleanup
			},
			expectGatewayPort:  "8080",
			expectRedisAddr:    "localhost:6379",
			expectRedisDB:      0,
			expectOTLPEndpoint: "localhost:4317",
			expectOTLPEnabled:  false,
			expectCPURL:        "http://localhost:8081",
		},
		{
			name: "load partial defaults",
			setupEnv: func() {
				os.Unsetenv("GATEWAY_PORT")
				os.Setenv("REDIS_ADDR", "redis.custom:6380")
				os.Unsetenv("REDIS_DB")
				os.Unsetenv("OTLP_ENDPOINT")
				os.Unsetenv("CONTROL_PLANE_URL")
			},
			cleanupEnv: func() {
				os.Unsetenv("REDIS_ADDR")
			},
			expectGatewayPort:  "8080",
			expectRedisAddr:    "redis.custom:6380",
			expectRedisDB:      0,
			expectOTLPEndpoint: "localhost:4317",
			expectOTLPEnabled:  false,
			expectCPURL:        "http://localhost:8081",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment
			tt.setupEnv()
			defer tt.cleanupEnv()

			config := Load()

			assert.Equal(t, tt.expectGatewayPort, config.Server.Port)
			assert.Equal(t, tt.expectRedisAddr, config.Redis.Addr)
			assert.Equal(t, tt.expectRedisDB, config.Redis.DB)
			assert.Equal(t, tt.expectOTLPEndpoint, config.OTLP.Endpoint)
			assert.Equal(t, tt.expectOTLPEnabled, config.OTLP.Enabled)
			assert.Equal(t, tt.expectCPURL, config.Provider.ControlPlaneURL)
		})
	}
}

// TestLoad_EnvOverrides tests config loading with environment variable overrides
func TestLoad_EnvOverrides(t *testing.T) {
	tests := []struct {
		name              string
		setupEnv          func()
		cleanupEnv        func()
		expectGatewayPort string
		expectRedisAddr   string
		expectRedisDB     int
		expectRedisPass   string
		expectOTLPEnabled bool
		expectCPURL       string
		expectToken       string
	}{
		{
			name: "override gateway port",
			setupEnv: func() {
				os.Setenv("GATEWAY_PORT", "9000")
				os.Unsetenv("REDIS_ADDR")
				os.Unsetenv("REDIS_DB")
				os.Unsetenv("REDIS_PASSWORD")
				os.Unsetenv("OTLP_ENDPOINT")
				os.Unsetenv("OTLP_ENABLED")
				os.Unsetenv("INTERNAL_TOKEN")
				os.Unsetenv("CONTROL_PLANE_URL")
			},
			cleanupEnv: func() {
				os.Unsetenv("GATEWAY_PORT")
			},
			expectGatewayPort: "9000",
			expectRedisAddr:   "localhost:6379",
			expectRedisDB:     0,
			expectOTLPEnabled: false,
			expectCPURL:       "http://localhost:8081",
		},
		{
			name: "override redis configuration",
			setupEnv: func() {
				os.Unsetenv("GATEWAY_PORT")
				os.Setenv("REDIS_ADDR", "redis-cluster:6379")
				os.Setenv("REDIS_DB", "5")
				os.Setenv("REDIS_PASSWORD", "secret-password")
				os.Unsetenv("OTLP_ENDPOINT")
				os.Unsetenv("OTLP_ENABLED")
				os.Unsetenv("INTERNAL_TOKEN")
				os.Unsetenv("CONTROL_PLANE_URL")
			},
			cleanupEnv: func() {
				os.Unsetenv("REDIS_ADDR")
				os.Unsetenv("REDIS_DB")
				os.Unsetenv("REDIS_PASSWORD")
			},
			expectGatewayPort: "8080",
			expectRedisAddr:   "redis-cluster:6379",
			expectRedisDB:     5,
			expectRedisPass:   "secret-password",
			expectOTLPEnabled: false,
			expectCPURL:       "http://localhost:8081",
		},
		{
			name: "override otlp configuration",
			setupEnv: func() {
				os.Unsetenv("GATEWAY_PORT")
				os.Unsetenv("REDIS_ADDR")
				os.Unsetenv("REDIS_DB")
				os.Unsetenv("REDIS_PASSWORD")
				os.Setenv("OTLP_ENDPOINT", "otel-collector:4317")
				os.Setenv("OTLP_ENABLED", "true")
				os.Unsetenv("INTERNAL_TOKEN")
				os.Unsetenv("CONTROL_PLANE_URL")
			},
			cleanupEnv: func() {
				os.Unsetenv("OTLP_ENDPOINT")
				os.Unsetenv("OTLP_ENABLED")
			},
			expectGatewayPort: "8080",
			expectRedisAddr:   "localhost:6379",
			expectRedisDB:     0,
			expectOTLPEnabled: true,
			expectCPURL:       "http://localhost:8081",
		},
		{
			name: "override control plane url",
			setupEnv: func() {
				os.Unsetenv("GATEWAY_PORT")
				os.Unsetenv("REDIS_ADDR")
				os.Unsetenv("REDIS_DB")
				os.Unsetenv("REDIS_PASSWORD")
				os.Unsetenv("OTLP_ENDPOINT")
				os.Unsetenv("OTLP_ENABLED")
				os.Unsetenv("INTERNAL_TOKEN")
				os.Setenv("CONTROL_PLANE_URL", "http://cp.example.com:9000")
			},
			cleanupEnv: func() {
				os.Unsetenv("CONTROL_PLANE_URL")
			},
			expectGatewayPort: "8080",
			expectRedisAddr:   "localhost:6379",
			expectRedisDB:     0,
			expectOTLPEnabled: false,
			expectCPURL:       "http://cp.example.com:9000",
		},
		{
			name: "override internal token",
			setupEnv: func() {
				os.Unsetenv("GATEWAY_PORT")
				os.Unsetenv("REDIS_ADDR")
				os.Unsetenv("REDIS_DB")
				os.Unsetenv("REDIS_PASSWORD")
				os.Unsetenv("OTLP_ENDPOINT")
				os.Unsetenv("OTLP_ENABLED")
				os.Setenv("INTERNAL_TOKEN", "secret-token-123")
				os.Unsetenv("CONTROL_PLANE_URL")
			},
			cleanupEnv: func() {
				os.Unsetenv("INTERNAL_TOKEN")
			},
			expectGatewayPort: "8080",
			expectRedisAddr:   "localhost:6379",
			expectRedisDB:     0,
			expectOTLPEnabled: false,
			expectCPURL:       "http://localhost:8081",
			expectToken:       "secret-token-123",
		},
		{
			name: "override all configuration",
			setupEnv: func() {
				os.Setenv("GATEWAY_PORT", "3000")
				os.Setenv("REDIS_ADDR", "redis-prod:6379")
				os.Setenv("REDIS_DB", "10")
				os.Setenv("REDIS_PASSWORD", "prod-password")
				os.Setenv("OTLP_ENDPOINT", "otel-prod:4317")
				os.Setenv("OTLP_ENABLED", "true")
				os.Setenv("INTERNAL_TOKEN", "token-xyz")
				os.Setenv("CONTROL_PLANE_URL", "http://cp-prod:8081")
			},
			cleanupEnv: func() {
				os.Unsetenv("GATEWAY_PORT")
				os.Unsetenv("REDIS_ADDR")
				os.Unsetenv("REDIS_DB")
				os.Unsetenv("REDIS_PASSWORD")
				os.Unsetenv("OTLP_ENDPOINT")
				os.Unsetenv("OTLP_ENABLED")
				os.Unsetenv("INTERNAL_TOKEN")
				os.Unsetenv("CONTROL_PLANE_URL")
			},
			expectGatewayPort: "3000",
			expectRedisAddr:   "redis-prod:6379",
			expectRedisDB:     10,
			expectRedisPass:   "prod-password",
			expectOTLPEnabled: true,
			expectCPURL:       "http://cp-prod:8081",
			expectToken:       "token-xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv()
			defer tt.cleanupEnv()

			config := Load()

			assert.Equal(t, tt.expectGatewayPort, config.Server.Port)
			assert.Equal(t, tt.expectRedisAddr, config.Redis.Addr)
			assert.Equal(t, tt.expectRedisDB, config.Redis.DB)
			assert.Equal(t, tt.expectRedisPass, config.Redis.Password)
			assert.Equal(t, tt.expectOTLPEnabled, config.OTLP.Enabled)
			assert.Equal(t, tt.expectCPURL, config.Provider.ControlPlaneURL)
			assert.Equal(t, tt.expectToken, config.Provider.InternalToken)
		})
	}
}

// TestLoad_RedisDBValidation tests REDIS_DB parsing
func TestLoad_RedisDBValidation(t *testing.T) {
	tests := []struct {
		name       string
		dbEnv      string
		expectDB   int
		expectPanic bool
	}{
		{
			name:     "valid db number 0",
			dbEnv:    "0",
			expectDB: 0,
		},
		{
			name:     "valid db number 15",
			dbEnv:    "15",
			expectDB: 15,
		},
		{
			name:     "default when empty",
			dbEnv:    "",
			expectDB: 0,
		},
		{
			name:     "invalid db number",
			dbEnv:    "not-a-number",
			expectDB: 0, // Atoi returns 0 on error, no panic
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("GATEWAY_PORT")
			os.Unsetenv("REDIS_ADDR")
			os.Unsetenv("REDIS_PASSWORD")
			os.Unsetenv("OTLP_ENDPOINT")
			os.Unsetenv("OTLP_ENABLED")
			os.Unsetenv("INTERNAL_TOKEN")
			os.Unsetenv("CONTROL_PLANE_URL")

			if tt.dbEnv != "" {
				os.Setenv("REDIS_DB", tt.dbEnv)
				defer os.Unsetenv("REDIS_DB")
			}

			config := Load()
			assert.Equal(t, tt.expectDB, config.Redis.DB)
		})
	}
}

// TestLoad_OTLPEnabledParsing tests OTLP_ENABLED parsing
func TestLoad_OTLPEnabledParsing(t *testing.T) {
	tests := []struct {
		name          string
		otlpEnabledEnv string
		expectEnabled bool
	}{
		{
			name:           "true string enables OTLP",
			otlpEnabledEnv: "true",
			expectEnabled: true,
		},
		{
			name:           "empty string disables OTLP",
			otlpEnabledEnv: "",
			expectEnabled: false,
		},
		{
			name:           "other values disable OTLP",
			otlpEnabledEnv: "false",
			expectEnabled: false,
		},
		{
			name:           "yes disables OTLP (case sensitive)",
			otlpEnabledEnv: "yes",
			expectEnabled: false,
		},
		{
			name:           "1 disables OTLP (must be true exactly)",
			otlpEnabledEnv: "1",
			expectEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("GATEWAY_PORT")
			os.Unsetenv("REDIS_ADDR")
			os.Unsetenv("REDIS_DB")
			os.Unsetenv("REDIS_PASSWORD")
			os.Unsetenv("OTLP_ENDPOINT")
			os.Unsetenv("INTERNAL_TOKEN")
			os.Unsetenv("CONTROL_PLANE_URL")

			if tt.otlpEnabledEnv != "" {
				os.Setenv("OTLP_ENABLED", tt.otlpEnabledEnv)
				defer os.Unsetenv("OTLP_ENABLED")
			} else {
				os.Unsetenv("OTLP_ENABLED")
			}

			config := Load()
			assert.Equal(t, tt.expectEnabled, config.OTLP.Enabled)
		})
	}
}

// TestLoad_ConfigStructure tests complete config structure is populated
func TestLoad_ConfigStructure(t *testing.T) {
	os.Unsetenv("GATEWAY_PORT")
	os.Unsetenv("REDIS_ADDR")
	os.Unsetenv("REDIS_DB")
	os.Unsetenv("REDIS_PASSWORD")
	os.Unsetenv("OTLP_ENDPOINT")
	os.Unsetenv("OTLP_ENABLED")
	os.Unsetenv("INTERNAL_TOKEN")
	os.Unsetenv("CONTROL_PLANE_URL")

	config := Load()

	// Verify all config sections exist
	assert.NotNil(t, config.Server)
	assert.NotNil(t, config.Redis)
	assert.NotNil(t, config.OTLP)
	assert.NotNil(t, config.Provider)

	// Verify all fields have values
	assert.NotEmpty(t, config.Server.Port)
	assert.NotEmpty(t, config.Redis.Addr)
	assert.NotEmpty(t, config.OTLP.Endpoint)
	assert.NotEmpty(t, config.Provider.ControlPlaneURL)
}

// TestLoad_EmptyPasswordAllowed tests empty Redis password is allowed
func TestLoad_EmptyPasswordAllowed(t *testing.T) {
	os.Unsetenv("REDIS_PASSWORD")

	config := Load()

	assert.Empty(t, config.Redis.Password)
}

// TestLoad_EmptyTokenAllowed tests empty internal token is allowed
func TestLoad_EmptyTokenAllowed(t *testing.T) {
	os.Unsetenv("INTERNAL_TOKEN")

	config := Load()

	assert.Empty(t, config.Provider.InternalToken)
}

// TestLoad_ServerPortEdgeCases tests various port values
func TestLoad_ServerPortEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		portEnv    string
		expectPort string
	}{
		{
			name:       "low port number",
			portEnv:    "80",
			expectPort: "80",
		},
		{
			name:       "high port number",
			portEnv:    "65535",
			expectPort: "65535",
		},
		{
			name:       "dynamic port",
			portEnv:    "0",
			expectPort: "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("GATEWAY_PORT")
			os.Unsetenv("REDIS_ADDR")
			os.Unsetenv("REDIS_DB")
			os.Unsetenv("REDIS_PASSWORD")
			os.Unsetenv("OTLP_ENDPOINT")
			os.Unsetenv("OTLP_ENABLED")
			os.Unsetenv("INTERNAL_TOKEN")
			os.Unsetenv("CONTROL_PLANE_URL")

			os.Setenv("GATEWAY_PORT", tt.portEnv)
			defer os.Unsetenv("GATEWAY_PORT")

			config := Load()
			assert.Equal(t, tt.expectPort, config.Server.Port)
		})
	}
}

// TestLoad_ViperYAML tests that Viper loads from YAML config file
func TestLoad_ViperYAML(t *testing.T) {
	// When APP_ENV is set to "dev", Viper should load config/dev.yaml
	// This test verifies the mechanism works (file may not exist in test env)
	os.Setenv("APP_ENV", "dev")
	defer os.Unsetenv("APP_ENV")

	// Should not panic even if config file not found
	config := Load()
	assert.NotNil(t, config)
}

// TestLoad_MultipleLoads tests loading config multiple times returns consistent results
func TestLoad_MultipleLoads(t *testing.T) {
	os.Unsetenv("GATEWAY_PORT")
	os.Unsetenv("REDIS_ADDR")
	os.Unsetenv("REDIS_DB")
	os.Unsetenv("REDIS_PASSWORD")
	os.Unsetenv("OTLP_ENDPOINT")
	os.Unsetenv("OTLP_ENABLED")
	os.Unsetenv("INTERNAL_TOKEN")
	os.Unsetenv("CONTROL_PLANE_URL")

	config1 := Load()
	config2 := Load()

	assert.Equal(t, config1.Server.Port, config2.Server.Port)
	assert.Equal(t, config1.Redis.Addr, config2.Redis.Addr)
	assert.Equal(t, config1.Redis.DB, config2.Redis.DB)
	assert.Equal(t, config1.OTLP.Endpoint, config2.OTLP.Endpoint)
	assert.Equal(t, config1.OTLP.Enabled, config2.OTLP.Enabled)
	assert.Equal(t, config1.Provider.ControlPlaneURL, config2.Provider.ControlPlaneURL)
}

// TestLoad_RedisDBStrconv tests strconv.Atoi behavior with invalid input
func TestLoad_RedisDBStrconv(t *testing.T) {
	// This tests the behavior of strconv.Atoi with invalid input
	tests := []struct {
		name   string
		input  string
		expect int
	}{
		{
			name:   "valid number",
			input:  "5",
			expect: 5,
		},
		{
			name:   "invalid string defaults to 0",
			input:  "not-a-number",
			expect: 0,
		},
		{
			name:   "empty string defaults to 0",
			input:  "",
			expect: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := strconv.Atoi(tt.input)
			if tt.input == "" {
				result = 0
			}
			assert.Equal(t, tt.expect, result)
		})
	}
}
