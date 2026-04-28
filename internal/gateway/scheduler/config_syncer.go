package scheduler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	configSyncTimeout     = 5 * time.Second
	configSyncEndpoint    = "/api/v1/internal/groups"
)

// ConfigSyncer periodically fetches configuration from the control plane
// and updates the local RouteTable and HealthStore.
type ConfigSyncer struct {
	httpClient    *http.Client
	cpBaseURL     string
	routeTable    *RouteTable
	healthStore   *HealthStore
	interval      time.Duration
	stopCh        chan struct{}
	lastSyncAt    time.Time
	syncMu        sync.RWMutex
	internalToken string
}

// NewConfigSyncer creates a ConfigSyncer with the given parameters.
// If interval <= 0, the default sync interval of 5 seconds is used.
func NewConfigSyncer(cpBaseURL, internalToken string, routeTable *RouteTable, healthStore *HealthStore, interval time.Duration) *ConfigSyncer {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	cpBaseURL = strings.TrimRight(cpBaseURL, "/")

	return &ConfigSyncer{
		httpClient: &http.Client{
			Timeout: configSyncTimeout,
		},
		cpBaseURL:     cpBaseURL,
		routeTable:    routeTable,
		healthStore:   healthStore,
		interval:      interval,
		stopCh:        make(chan struct{}),
		internalToken: internalToken,
	}
}

// Run starts the config sync loop. It blocks until Stop is called.
func (cs *ConfigSyncer) Run() {
	ticker := time.NewTicker(cs.interval)
	defer ticker.Stop()

	for {
		select {
		case <-cs.stopCh:
			return
		case <-ticker.C:
			if err := cs.doSync(); err != nil {
				slog.Warn("scheduler config sync failed", "error", err)
			}
		}
	}
}

// doSync fetches the latest config from the control plane and updates
// the route table and health store. Errors are logged via fmt but not returned
// to avoid blocking the ticker loop.
func (cs *ConfigSyncer) doSync() error {
	config, err := cs.fetchConfig()
	if err != nil {
		return fmt.Errorf("config sync failed: %w", err)
	}

	cs.routeTable.ReplaceAll(config)

	activeIDs := collectActiveModelIDs(config)
	cs.healthStore.CleanOrphaned(activeIDs)

	cs.syncMu.Lock()
	cs.lastSyncAt = time.Now()
	cs.syncMu.Unlock()

	slog.Info("scheduler config synced", "groups", len(config.Groups), "models", len(config.Models))
	return nil
}

// Stop stops the config sync loop.
func (cs *ConfigSyncer) Stop() {
	close(cs.stopCh)
}

// LastSyncAt returns the last successful sync time.
func (cs *ConfigSyncer) LastSyncAt() time.Time {
	cs.syncMu.RLock()
	defer cs.syncMu.RUnlock()
	return cs.lastSyncAt
}

// fetchConfig performs an HTTP GET to the control plane and parses the
// response into a ControlPlaneConfig.
func (cs *ConfigSyncer) fetchConfig() (*ControlPlaneConfig, error) {
	url := cs.cpBaseURL + configSyncEndpoint

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Auth-Token", cs.internalToken)

	resp, err := cs.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var config ControlPlaneConfig
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}

	// Convert from control-plane response format if Data is populated
	if len(config.Data) > 0 {
		config.Groups = make([]GroupConfig, 0, len(config.Data))
		config.Models = make([]GroupModel, 0)
		modelIdx := uint(1)
				for _, g := range config.Data {
			config.Groups = append(config.Groups, GroupConfig{
				ID:               g.ID,
				Name:             g.Name,
				Strategy:         g.Strategy,
				Enabled:          g.Enabled,
				Description:      g.Description,
				TimeoutSeconds:   g.TimeoutSeconds,
				StickyEnabled:    g.StickyEnabled,
				StickyTTLSeconds: g.StickyTTLSeconds,
			})
			for _, m := range g.Models {
				config.Models = append(config.Models, GroupModel{
					ID:             modelIdx,
					GroupID:        g.ID,
					ModelID:        modelIdx,
					ModelName:      m.Name,
					ModelType:      m.ModelType,
					ChannelType:    m.ProviderType,
					ProviderName:   m.ProviderName,
					Endpoint:       m.Endpoint,
					Priority:       m.Priority,
					Weight:         m.Weight,
					ComplexityTier: m.ComplexityTier,
				})
				modelIdx++
			}
		}
	}

	return &config, nil
}

// collectActiveModelIDs extracts all model IDs from a ControlPlaneConfig
// as strings for use with HealthStore.
func collectActiveModelIDs(config *ControlPlaneConfig) []string {
	if config == nil {
		return nil
	}
	ids := make([]string, 0, len(config.Models))
	for _, m := range config.Models {
		ids = append(ids, fmt.Sprintf("%d", m.ModelID))
	}
	return ids
}
