// Package providers provides config types ported from GoModel's config package.
// These types are the provider-specific subset needed by the providers package:
// RawProviderConfig, RawProviderModel, RawResilienceConfig and helpers.
package providers

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclientconfig"
)

// ResilienceConfig is an alias for llmclientconfig.ResilienceConfig so the
// providers package can reference it without a long import path in every file.
type ResilienceConfig = llmclientconfig.ResilienceConfig

// RetryConfig is an alias for llmclientconfig.RetryConfig.
type RetryConfig = llmclientconfig.RetryConfig

// CircuitBreakerConfig is an alias for llmclientconfig.CircuitBreakerConfig.
type CircuitBreakerConfig = llmclientconfig.CircuitBreakerConfig

// ConfiguredProviderModelsMode controls how explicitly configured provider
// model lists are applied to the discovered model inventory.
type ConfiguredProviderModelsMode string

const (
	ConfiguredProviderModelsModeFallback  ConfiguredProviderModelsMode = "fallback"
	ConfiguredProviderModelsModeAllowlist ConfiguredProviderModelsMode = "allowlist"
)

// Valid reports whether mode is one of the supported configured-provider-models modes.
func (m ConfiguredProviderModelsMode) Valid() bool {
	switch NormalizeConfiguredProviderModelsMode(m) {
	case ConfiguredProviderModelsModeFallback, ConfiguredProviderModelsModeAllowlist:
		return true
	default:
		return false
	}
}

// NormalizeConfiguredProviderModelsMode canonicalizes a configured provider models mode.
func NormalizeConfiguredProviderModelsMode(mode ConfiguredProviderModelsMode) ConfiguredProviderModelsMode {
	return ConfiguredProviderModelsMode(strings.ToLower(strings.TrimSpace(string(mode))))
}

// ResolveConfiguredProviderModelsMode canonicalizes mode and applies the process default.
func ResolveConfiguredProviderModelsMode(mode ConfiguredProviderModelsMode) ConfiguredProviderModelsMode {
	mode = NormalizeConfiguredProviderModelsMode(mode)
	if mode == "" {
		return ConfiguredProviderModelsModeFallback
	}
	return mode
}

// RawRetryConfig holds optional per-provider retry overrides from YAML.
// Nil fields inherit from the global RetryConfig.
type RawRetryConfig struct {
	MaxRetries     *int           `yaml:"max_retries"`
	InitialBackoff *time.Duration `yaml:"initial_backoff"`
	MaxBackoff     *time.Duration `yaml:"max_backoff"`
	BackoffFactor  *float64       `yaml:"backoff_factor"`
	JitterFactor   *float64       `yaml:"jitter_factor"`
}

// RawCircuitBreakerConfig holds optional per-provider circuit breaker overrides from YAML.
// Nil fields inherit from the global CircuitBreakerConfig.
type RawCircuitBreakerConfig struct {
	FailureThreshold *int           `yaml:"failure_threshold"`
	SuccessThreshold *int           `yaml:"success_threshold"`
	Timeout          *time.Duration `yaml:"timeout"`
}

// RawResilienceConfig holds optional per-provider resilience overrides from YAML.
// Nil fields inherit from the global ResilienceConfig.
type RawResilienceConfig struct {
	Retry          *RawRetryConfig          `yaml:"retry"`
	CircuitBreaker *RawCircuitBreakerConfig `yaml:"circuit_breaker"`
}

// RawProviderConfig is the YAML-sourced provider configuration before env var
// overrides, credential filtering, or resilience merging. Exported so the
// providers package can resolve it into a fully-configured ProviderConfig.
type RawProviderConfig struct {
	Type                     string               `yaml:"type"`
	APIKey                   string               `yaml:"api_key"`
	BaseURL                  string               `yaml:"base_url"`
	APIVersion               string               `yaml:"api_version"`
	Backend                  string               `yaml:"backend"`
	AuthType                 string               `yaml:"auth_type"`
	APIMode                  string               `yaml:"api_mode"`
	VertexProject            string               `yaml:"vertex_project"`
	VertexLocation           string               `yaml:"vertex_location"`
	ServiceAccountFile       string               `yaml:"service_account_file"`
	ServiceAccountJSON       string               `yaml:"service_account_json"`
	ServiceAccountJSONBase64 string               `yaml:"service_account_json_base64"`
	GCPScope                 string               `yaml:"gcp_scope"`
	Models                   []RawProviderModel   `yaml:"models"`
	Resilience               *RawResilienceConfig `yaml:"resilience"`
}

// RawProviderModel is a single entry under providers.<name>.models. It supports
// two YAML shapes so operators can opt into rich metadata without churning
// simple configs:
//
//	models:
//	  - some-model-id                    # bare string
//	  - id: local-model                  # mapping with optional metadata
//	    metadata:
//	      context_window: 131072
//	      capabilities:
//	        tools: true
//
// Metadata is merged onto whatever the remote model registry supplies, with
// config-declared fields taking precedence. This lets local providers (Ollama,
// custom OpenAI-compatible endpoints) advertise their context windows, pricing,
// and capabilities via /v1/models even when the remote registry has no entry.
type RawProviderModel struct {
	ID       string              `yaml:"id"`
	Metadata *core.ModelMetadata `yaml:"metadata,omitempty"`
}

// UnmarshalYAML accepts either a bare string (model ID) or a mapping with id and metadata.
func (m *RawProviderModel) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var id string
		if err := node.Decode(&id); err != nil {
			return fmt.Errorf("provider model: %w", err)
		}
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("provider model: id is required")
		}
		m.ID = id
		return nil
	case yaml.MappingNode:
		type rawAlias RawProviderModel
		var alias rawAlias
		if err := node.Decode(&alias); err != nil {
			return fmt.Errorf("provider model: %w", err)
		}
		*m = RawProviderModel(alias)
		m.ID = strings.TrimSpace(m.ID)
		if m.ID == "" {
			return fmt.Errorf("provider model: id is required")
		}
		return nil
	default:
		return fmt.Errorf("provider model: expected scalar or mapping, got kind %d", node.Kind)
	}
}

// ProviderModelIDs returns the ID of each model entry, preserving order and
// dropping entries with empty IDs.
func ProviderModelIDs(models []RawProviderModel) []string {
	if len(models) == 0 {
		return nil
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// ProviderModelMetadataOverrides returns id -> metadata for entries with
// non-nil Metadata. Returns nil if no entries declare metadata.
func ProviderModelMetadataOverrides(models []RawProviderModel) map[string]*core.ModelMetadata {
	var out map[string]*core.ModelMetadata
	for _, m := range models {
		if m.ID == "" || m.Metadata == nil {
			continue
		}
		if out == nil {
			out = make(map[string]*core.ModelMetadata)
		}
		out[m.ID] = m.Metadata
	}
	return out
}
