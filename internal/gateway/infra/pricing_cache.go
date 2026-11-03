package infra

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// PricingGetter allows looking up model pricing by name.
type PricingGetter interface {
	Get(modelName string) (ModelPricing, bool)
}

// pricingResponse is the JSON shape returned by control-plane /internal/v1/pricing.
type pricingResponse struct {
	Models map[string]ModelPricing `json:"models"`
}

// MemoryPricingCache periodically fetches model pricing from the control-plane
// and serves lookups from an in-memory cache. It implements PricingGetter.
type MemoryPricingCache struct {
	cache           sync.Map
	controlPlaneURL string
	internalToken   string
	client          *http.Client
	mu              sync.RWMutex
	stopFn          context.CancelFunc
}

// NewMemoryPricingCache creates a new MemoryPricingCache.
func NewMemoryPricingCache(controlPlaneURL, internalToken string) *MemoryPricingCache {
	return &MemoryPricingCache{
		controlPlaneURL: controlPlaneURL,
		internalToken:   internalToken,
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

// Start begins the periodic refresh loop. It does an immediate fetch on startup,
// then refreshes every 5 minutes. The loop runs until ctx is cancelled.
func (c *MemoryPricingCache) Start(ctx context.Context) {
	ctx, c.stopFn = context.WithCancel(ctx)

	c.refresh()

	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.refresh()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop cancels the background refresh loop.
func (c *MemoryPricingCache) Stop() {
	if c.stopFn != nil {
		c.stopFn()
	}
}

// Get looks up pricing for the given model name. Returns the pricing and true
// if found, or zero value and false if not.
func (c *MemoryPricingCache) Get(modelName string) (ModelPricing, bool) {
	v, ok := c.cache.Load(modelName)
	if !ok {
		return ModelPricing{}, false
	}
	return v.(ModelPricing), true
}

// refresh fetches the latest pricing from the control-plane and updates the
// cache. On error, it logs the failure, increments the error metric, and
// leaves the existing cache intact.
func (c *MemoryPricingCache) refresh() {
	req, err := http.NewRequest(http.MethodGet, c.controlPlaneURL+"/internal/v1/pricing", nil)
	if err != nil {
		slog.Error("pricing cache: failed to create request", "error", err)
		UsageMetricsInstance.PricingCacheRefreshErrs.Inc()
		return
	}
	req.Header.Set("X-Internal-Token", c.internalToken)

	resp, err := c.client.Do(req)
	if err != nil {
		slog.Error("pricing cache: HTTP request failed", "error", err)
		UsageMetricsInstance.PricingCacheRefreshErrs.Inc()
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("pricing cache: failed to read response body", "error", err)
		UsageMetricsInstance.PricingCacheRefreshErrs.Inc()
		return
	}

	var pr pricingResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		slog.Error("pricing cache: failed to parse JSON response", "error", err)
		UsageMetricsInstance.PricingCacheRefreshErrs.Inc()
		return
	}

	for modelName, pricing := range pr.Models {
		c.cache.Store(modelName, pricing)
	}
}
