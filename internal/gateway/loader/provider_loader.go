// Package loader bridges the Control Plane provider configuration with
// GoModel's ProviderFactory and Router, providing atomic hot-reload on
// Redis config:change events.
package loader

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/example/llm-gateway/internal/gateway/infra"
	gatewaymodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/llmclientconfig"
	"github.com/example/llm-gateway/internal/providers"
)

// ProviderLoader bridges our Control Plane config with GoModel's ProviderFactory.
// It loads provider configs from the Redis snapshot → builds a providers.Router,
// and listens for config:change events → atomically replaces the active router.
type ProviderLoader struct {
	factory  *providers.ProviderFactory
	current  atomic.Value // stores *providers.Router
	onReload func(router *providers.Router)
}

// NewProviderLoader creates a ProviderLoader backed by the given factory.
// onReload is called (in the same goroutine as the event) whenever the router
// is atomically replaced due to a config change. May be nil.
func NewProviderLoader(factory *providers.ProviderFactory, onReload func(router *providers.Router)) *ProviderLoader {
	return &ProviderLoader{
		factory:  factory,
		onReload: onReload,
	}
}

// Router returns the currently active Router, or nil if one has not been built yet.
func (l *ProviderLoader) Router() *providers.Router {
	v := l.current.Load()
	if v == nil {
		return nil
	}
	return v.(*providers.Router)
}

// Build constructs a new Router from a slice of ProviderEntry values and atomically
// stores it as the current router.
//
// We use ConfiguredProviderModelsModeAllowlist so the registry skips calling
// the upstream /models endpoints and instead trusts the model list declared in
// the control-plane configuration. This matches the previous gateway behaviour
// where models were explicitly configured per provider entry.
func (l *ProviderLoader) Build(entries []gatewaymodel.ProviderEntry) (*providers.Router, error) {
	registry := providers.NewModelRegistry()
	// Use allowlist mode: model inventory comes entirely from config, not from
	// upstream discovery calls. This avoids requiring every provider to expose
	// a /models endpoint and keeps startup deterministic.
	registry.SetConfiguredProviderModelsMode(providers.ConfiguredProviderModelsModeAllowlist)

	builtCount := 0
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		if len(entry.Models) == 0 {
			slog.Warn("loader: provider has no models configured, skipping",
				"provider_name", entry.Name,
				"provider_type", entry.Type,
			)
			continue
		}

		cfg := providers.ProviderConfig{
			Type:    entry.Type,
			APIKey:  entry.Credentials,
			BaseURL: entry.Endpoint,
			Models:  entry.Models,
			// Phase 1: 启用 HTTP 重试（3 次指数退避）和熔断器
			Resilience: providers.ResilienceConfig{
				Retry:          llmclientconfig.DefaultRetryConfig(),
				CircuitBreaker: llmclientconfig.DefaultCircuitBreakerConfig(),
			},
		}

		p, err := l.factory.Create(cfg)
		if err != nil {
			slog.Warn("loader: failed to create provider, skipping",
				"provider_name", entry.Name,
				"provider_type", entry.Type,
				"error", err,
			)
			continue
		}

		registry.RegisterProviderWithNameAndType(p, entry.Name, entry.Type)
		registry.SetProviderConfiguredModels(entry.Name, entry.Models)
		builtCount++
	}

	if builtCount == 0 {
		slog.Warn("loader: no providers built from entries", "entry_count", len(entries))
	}

	// Initialize the registry. In allowlist mode this will not make any
	// network calls – it will simply apply the configured model lists.
	if builtCount > 0 {
		if err := registry.Initialize(context.Background()); err != nil {
			slog.Warn("loader: registry initialization returned error (may be non-fatal)",
				"error", err,
				"provider_count", builtCount,
			)
		}
	}

	router, err := providers.NewRouter(registry)
	if err != nil {
		return nil, fmt.Errorf("loader: build router: %w", err)
	}

	l.current.Store(router)
	slog.Info("loader: router built",
		"provider_count", registry.ProviderCount(),
		"model_count", registry.ModelCount(),
	)
	return router, nil
}

// HandleChangeEvent returns an infra.ChangeCallback that can be passed directly
// to infra.NewSubscriber.  On each event it calls fetchEntries to get the latest
// configuration snapshot and rebuilds the router.
func (l *ProviderLoader) HandleChangeEvent(fetchEntries func() ([]gatewaymodel.ProviderEntry, error)) infra.ChangeCallback {
	return func(ev infra.ChangeEvent) {
		slog.Info("loader: config change event received, rebuilding router",
			"event_type", ev.Type,
			"resource_type", ev.ResourceType,
			"snapshot_version", ev.SnapshotVersion,
		)

		entries, err := fetchEntries()
		if err != nil {
			slog.Error("loader: failed to fetch entries on config change", "error", err)
			return
		}

		router, err := l.Build(entries)
		if err != nil {
			slog.Error("loader: failed to rebuild router on config change", "error", err)
			return
		}

		if l.onReload != nil {
			l.onReload(router)
		}
	}
}

// Initialize builds the initial router from the provided entries.
// It is a thin wrapper around Build kept for API symmetry.
func (l *ProviderLoader) Initialize(_ context.Context, entries []gatewaymodel.ProviderEntry) (*providers.Router, error) {
	return l.Build(entries)
}
