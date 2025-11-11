package llmclientconfig

import "time"

// RetryConfig holds resolved retry settings for an LLM client.
// This is the canonical type shared between config and llmclient.
type RetryConfig struct {
	MaxRetries     int           `yaml:"max_retries"     env:"RETRY_MAX_RETRIES"`
	InitialBackoff time.Duration `yaml:"initial_backoff" env:"RETRY_INITIAL_BACKOFF"`
	MaxBackoff     time.Duration `yaml:"max_backoff"     env:"RETRY_MAX_BACKOFF"`
	BackoffFactor  float64       `yaml:"backoff_factor"  env:"RETRY_BACKOFF_FACTOR"`
	JitterFactor   float64       `yaml:"jitter_factor"   env:"RETRY_JITTER_FACTOR"`
}

// DefaultRetryConfig returns the default retry settings.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
		JitterFactor:   0.1,
	}
}

// CircuitBreakerConfig holds resolved circuit breaker settings.
// This is the canonical type shared between config and llmclient.
type CircuitBreakerConfig struct {
	FailureThreshold int           `yaml:"failure_threshold" env:"CIRCUIT_BREAKER_FAILURE_THRESHOLD"`
	SuccessThreshold int           `yaml:"success_threshold" env:"CIRCUIT_BREAKER_SUCCESS_THRESHOLD"`
	Timeout          time.Duration `yaml:"timeout"           env:"CIRCUIT_BREAKER_TIMEOUT"`
}

// DefaultCircuitBreakerConfig returns the default circuit breaker settings.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
	}
}

// ResilienceConfig holds resolved resilience settings (retry and circuit breaker).
type ResilienceConfig struct {
	Retry          RetryConfig          `yaml:"retry"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`
}

// FallbackMode represents the fallback behavior mode.
type FallbackMode string

const (
	FallbackModeOff    FallbackMode = "off"
	FallbackModeManual FallbackMode = "manual"
	FallbackModeAuto   FallbackMode = "auto"
)

// FallbackModelOverride holds per-model mode overrides.
type FallbackModelOverride struct {
	Mode FallbackMode `yaml:"mode" json:"mode"`
}

// FallbackConfig holds translated-route model fallback policy.
type FallbackConfig struct {
	// DefaultMode controls the fallback behavior when no per-model override exists.
	// Supported values: "auto", "manual", "off". Default: "manual".
	DefaultMode FallbackMode `yaml:"default_mode" env:"FEATURE_FALLBACK_MODE"`

	// ManualRulesPath points to a JSON file that maps source model selectors to
	// ordered fallback model selector lists. Empty disables manual rules.
	ManualRulesPath string `yaml:"manual_rules_path" env:"FALLBACK_MANUAL_RULES_PATH"`

	// Overrides controls per-model mode overrides. Keys may be bare models
	// ("gpt-4o") or provider-qualified public selectors ("azure/gpt-4o").
	Overrides map[string]FallbackModelOverride `yaml:"overrides"`

	// Manual holds the parsed manual fallback lists loaded from ManualRulesPath.
	Manual map[string][]string `yaml:"-"`
}
